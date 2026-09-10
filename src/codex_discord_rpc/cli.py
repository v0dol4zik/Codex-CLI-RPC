"""CLI wrapper that runs Codex while maintaining Discord Rich Presence."""

from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import threading
import time
import tomllib
from typing import Callable, Sequence

from . import __version__
from .discord_ipc import DiscordIpcClient, DiscordIpcError, find_ipc_socket
from .lock import InstanceLock
from .processes import CodexProcess, find_codex_processes, newest_codex_process
from .service import ServiceError, service_action


DEFAULT_DETAILS = "Работаю в Codex"
DEFAULT_STATE = "Codex CLI"
CONFIG_FILENAME = "config.toml"


@dataclass(frozen=True)
class Settings:
    client_id: str | None
    codex_binary: str
    details: str
    state: str
    large_image: str | None
    large_text: str | None
    retry_seconds: float
    refresh_seconds: float
    runtime_dir: str | None
    process_poll_seconds: float = 1.0
    lock_file: str | None = None


def _configured_path() -> Path:
    configured = os.environ.get("CODEX_RPC_CONFIG")
    if configured:
        return Path(configured).expanduser()
    config_home = os.environ.get("XDG_CONFIG_HOME")
    root = Path(config_home).expanduser() if config_home else Path.home() / ".config"
    return root / "codex-discord-rpc" / CONFIG_FILENAME


def load_config() -> dict[str, object]:
    path = _configured_path()
    try:
        with path.open("rb") as config_file:
            data = tomllib.load(config_file)
    except FileNotFoundError:
        return {}
    except (OSError, tomllib.TOMLDecodeError) as error:
        raise ValueError(f"Не удалось прочитать {path}: {error}") from error
    if not isinstance(data, dict):
        raise ValueError(f"Конфигурация {path} должна быть TOML-таблицей")
    return data


def _text_setting(config: dict[str, object], key: str, env_name: str, default: str | None = None) -> str | None:
    env_value = os.environ.get(env_name)
    value = env_value if env_value is not None else config.get(key, default)
    if value is None:
        return None
    if not isinstance(value, str):
        raise ValueError(f"Параметр {key} должен быть строкой")
    value = value.strip()
    return value or None


def _float_setting(
    config: dict[str, object],
    key: str,
    env_name: str,
    default: float,
    minimum: float,
) -> float:
    value: object = os.environ.get(env_name, config.get(key, default))
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        raise ValueError(f"Параметр {key} должен быть числом") from None
    return max(parsed, minimum)


def resolve_codex_binary(configured: str | None = None) -> str:
    """Resolve the real Codex executable, avoiding this wrapper itself."""

    configured = os.environ.get("CODEX_BINARY", configured)
    if configured:
        path = Path(configured).expanduser()
        if not path.is_absolute():
            resolved = shutil.which(configured)
            if resolved:
                path = Path(resolved)
        if not path.is_file() or not os.access(path, os.X_OK):
            raise FileNotFoundError(f"Codex не найден или не исполняем: {path}")
        return str(path.resolve())

    wrapper_paths = {Path(__file__).resolve()}
    try:
        wrapper_paths.add(Path(sys.argv[0]).resolve())
    except OSError:
        pass
    candidates: list[str] = []
    for entry in os.environ.get("PATH", "").split(os.pathsep):
        if not entry:
            continue
        candidate = Path(entry) / "codex"
        if candidate.exists() and candidate.is_file():
            try:
                resolved_candidate = candidate.resolve()
                if resolved_candidate not in wrapper_paths and os.access(resolved_candidate, os.X_OK):
                    candidates.append(str(resolved_candidate))
            except OSError:
                continue
    if candidates:
        return candidates[0]
    raise FileNotFoundError("Не найден бинарник Codex. Укажите путь через CODEX_BINARY.")


def load_settings() -> Settings:
    config = load_config()
    legacy_client_id = os.environ.get("DISCORD_CLIENT_ID")
    client_id = _text_setting(config, "client_id", "CODEX_DISCORD_CLIENT_ID") or legacy_client_id
    if client_id:
        client_id = client_id.strip()
    if client_id and (not client_id.isascii() or not client_id.isdecimal()):
        raise ValueError("Discord Application ID должен состоять только из цифр")
    codex_binary = _text_setting(config, "codex_binary", "CODEX_BINARY")
    return Settings(
        client_id=client_id.strip() if client_id and client_id.strip() else None,
        codex_binary=resolve_codex_binary(codex_binary),
        details=_text_setting(config, "details", "CODEX_RPC_DETAILS", DEFAULT_DETAILS) or DEFAULT_DETAILS,
        state=_text_setting(config, "state", "CODEX_RPC_STATE", DEFAULT_STATE) or DEFAULT_STATE,
        large_image=_text_setting(config, "large_image", "CODEX_RPC_LARGE_IMAGE"),
        large_text=_text_setting(config, "large_text", "CODEX_RPC_LARGE_TEXT"),
        retry_seconds=_float_setting(config, "retry_seconds", "CODEX_RPC_RETRY_SECONDS", 2.0, 0.25),
        refresh_seconds=_float_setting(config, "refresh_seconds", "CODEX_RPC_REFRESH_SECONDS", 15.0, 5.0),
        runtime_dir=_text_setting(config, "runtime_dir", "CODEX_RPC_RUNTIME_DIR"),
        process_poll_seconds=_float_setting(
            config,
            "process_poll_seconds",
            "CODEX_RPC_PROCESS_POLL_SECONDS",
            1.0,
            0.25,
        ),
        lock_file=_text_setting(config, "lock_file", "CODEX_RPC_LOCK_FILE"),
    )


class PresenceWorker:
    """Keep one activity alive and reconnect after Discord restarts."""

    def __init__(self, settings: Settings, *, started_at: int | None = None) -> None:
        self.settings = settings
        self.started_at = int(time.time()) if started_at is None else int(started_at)
        self.stop_event = threading.Event()
        self.client = (
            DiscordIpcClient(
                settings.client_id or "",
                runtime_dir=settings.runtime_dir,
                timeout=1.0,
            )
            if settings.client_id
            else None
        )
        self.thread = threading.Thread(target=self._run, name="codex-discord-rpc", daemon=True)
        self.last_error: str | None = None

    def _activity(self) -> dict[str, object]:
        activity: dict[str, object] = {
            "details": self.settings.details,
            "state": self.settings.state,
            "timestamps": {"start": self.started_at},
        }
        assets: dict[str, str] = {}
        if self.settings.large_image:
            assets["large_image"] = self.settings.large_image
            if self.settings.large_text:
                assets["large_text"] = self.settings.large_text
        if assets:
            activity["assets"] = assets
        return activity

    def start(self) -> None:
        if self.client:
            self.thread.start()

    def _run(self) -> None:
        assert self.client is not None
        next_refresh = 0.0
        while not self.stop_event.is_set():
            now = time.monotonic()
            try:
                if not self.client.connected:
                    self.client.connect()
                    next_refresh = 0.0
                if now >= next_refresh:
                    self.client.set_activity(self._activity())
                    next_refresh = now + self.settings.refresh_seconds
                    self.last_error = None
            except DiscordIpcError as error:
                self.last_error = str(error)
                self.client.close()
                next_refresh = 0.0
            wait_for = (
                self.settings.retry_seconds
                if not self.client.connected
                else min(
                    self.settings.retry_seconds,
                    max(0.25, next_refresh - time.monotonic()),
                )
            )
            self.stop_event.wait(wait_for)

    def stop(self) -> None:
        if not self.client:
            return
        self.stop_event.set()
        self.thread.join(timeout=max(2.0, self.settings.retry_seconds + 1.0))
        try:
            if self.client.connected:
                self.client.set_activity(None)
        except DiscordIpcError:
            pass
        finally:
            self.client.close()


class CodexMonitor:
    """Watch the process table and attach Presence to an existing Codex."""

    def __init__(
        self,
        settings: Settings,
        *,
        process_finder: Callable[[str], list[CodexProcess]] = find_codex_processes,
        worker_factory: Callable[..., PresenceWorker] = PresenceWorker,
        logger: Callable[[str], None] | None = None,
    ) -> None:
        self.settings = settings
        self.process_finder = process_finder
        self.worker_factory = worker_factory
        self.logger = logger or (lambda message: print(message, flush=True))

    def run(self, stop_event: threading.Event | None = None) -> int:
        """Run until ``stop_event`` is set, returning a shell exit code."""

        event = stop_event or threading.Event()
        active_identity: tuple[int, int] | None = None
        worker: PresenceWorker | None = None
        last_error: str | None = None
        try:
            while not event.is_set():
                scan_failed = False
                try:
                    processes = self.process_finder(self.settings.codex_binary)
                    last_error = None
                except (OSError, RuntimeError, ValueError) as error:
                    processes = []
                    scan_failed = True
                    message = str(error)
                    if message != last_error:
                        self.logger(f"codex-rpc: ошибка поиска Codex: {message}")
                        last_error = message

                if scan_failed:
                    event.wait(self.settings.process_poll_seconds)
                    continue
                selected = newest_codex_process(processes)
                identity = selected.identity if selected is not None else None
                if identity != active_identity:
                    if worker is not None:
                        worker.stop()
                        worker = None
                    if selected is None:
                        if active_identity is not None:
                            self.logger("codex-rpc: Codex завершён, Presence очищена")
                    elif self.settings.client_id:
                        worker = self.worker_factory(self.settings, started_at=selected.start_time)
                        worker.start()
                        self.logger(
                            f"codex-rpc: найден Codex (PID {selected.pid}), Presence включена"
                        )
                    else:
                        self.logger(
                            f"codex-rpc: найден Codex (PID {selected.pid}), но Application ID не задан"
                        )
                    active_identity = identity
                event.wait(self.settings.process_poll_seconds)
        finally:
            if worker is not None:
                worker.stop()
        return 0


def monitor_codex(settings: Settings) -> int:
    """Run the process monitor with graceful signal handling."""

    lock = InstanceLock(settings.lock_file)
    if not lock.acquire():
        if lock.reason == "held":
            message = f"monitor уже запущен (lock: {lock.path})"
        else:
            message = f"не удалось получить lock {lock.path}: {lock.reason or 'неизвестная ошибка'}"
        print(f"codex-rpc: {message}", file=sys.stderr)
        return 0
    stop_event = threading.Event()
    previous_handlers: dict[int, object] = {}

    def stop(_signum: int, _frame: object) -> None:
        stop_event.set()

    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        try:
            previous_handlers[signum] = signal.signal(signum, stop)
        except (OSError, ValueError):
            pass
    try:
        return CodexMonitor(settings).run(stop_event)
    finally:
        for signum, handler in previous_handlers.items():
            signal.signal(signum, handler)
        lock.release()


def _check(settings: Settings) -> int:
    print(f"codex-discord-rpc {__version__}")
    print(f"Codex: {settings.codex_binary}")
    processes = find_codex_processes(settings.codex_binary)
    print(f"Codex processes: {len(processes)}")
    socket_path = find_ipc_socket(settings.runtime_dir)
    print(f"Discord IPC: {socket_path or 'не найден'}")
    if not settings.client_id:
        print("Discord client ID: не задан (статус будет отключён)")
        return 1
    print("Discord client ID: задан")
    if socket_path is None:
        return 1
    client = DiscordIpcClient(settings.client_id, runtime_dir=settings.runtime_dir)
    try:
        client.connect()
    except DiscordIpcError as error:
        print(f"Discord handshake: ошибка — {error}")
        return 1
    finally:
        client.close()
    print("Discord handshake: успешно")
    return 0


def run_codex(settings: Settings, argv: Sequence[str]) -> int:
    process = subprocess.Popen([settings.codex_binary, *argv])
    forwarded_signals = (signal.SIGTERM, signal.SIGHUP)
    terminal_signals = (signal.SIGINT, signal.SIGQUIT)
    previous_handlers: dict[int, object] = {}

    def forward(signum: int, _frame: object) -> None:
        if process.poll() is None:
            try:
                process.send_signal(signum)
            except OSError:
                pass

    for signum in forwarded_signals:
        try:
            previous_handlers[signum] = signal.signal(signum, forward)
        except (OSError, ValueError):
            pass
    # A terminal delivers these signals to the whole foreground process group,
    # including Codex. Forwarding them again would make Codex receive Ctrl+C
    # twice and could turn "interrupt current turn" into "exit".
    for signum in terminal_signals:
        try:
            previous_handlers[signum] = signal.signal(signum, lambda *_args: None)
        except (OSError, ValueError):
            pass
    try:
        return_code = process.wait()
        return return_code if return_code >= 0 else 128 - return_code
    finally:
        for signum, handler in previous_handlers.items():
            signal.signal(signum, handler)


def service_command(arguments: Sequence[str]) -> int:
    """Handle ``codex-rpc service <action>`` without loading Codex settings."""

    if len(arguments) != 1 or arguments[0] not in {"install", "uninstall", "status", "start", "stop", "restart"}:
        print(
            "Использование: codex-rpc service {install|uninstall|status|start|stop|restart}",
            file=sys.stderr,
        )
        return 2
    action = arguments[0]
    try:
        result = service_action(action)
    except ServiceError as error:
        print(f"codex-rpc service: {error}", file=sys.stderr)
        return 1
    if isinstance(result, Path):
        if action == "uninstall":
            print(f"User service удалён: {result}")
        else:
            print(f"User service установлен и запущен: {result}")
        return 0
    output = (result.stdout or result.stderr or "").strip()
    if output:
        print(output)
    return result.returncode


def main(argv: Sequence[str] | None = None) -> int:
    arguments = list(sys.argv[1:] if argv is None else argv)
    if arguments == ["--rpc-version"]:
        print(__version__)
        return 0
    if arguments and arguments[0] == "service":
        return service_command(arguments[1:])
    try:
        settings = load_settings()
    except FileNotFoundError as error:
        print(f"codex-rpc: {error}", file=sys.stderr)
        return 127
    except ValueError as error:
        print(f"codex-rpc: {error}", file=sys.stderr)
        return 2

    if arguments in (["--check"], ["--rpc-check"]):
        return _check(settings)
    if arguments in (["--monitor"], ["--watch"], ["--rpc-monitor"]):
        return monitor_codex(settings)
    lock = InstanceLock(settings.lock_file)
    worker: PresenceWorker | None = None
    owns_lock = lock.acquire()
    if owns_lock:
        worker = PresenceWorker(settings)
        worker.start()
    elif settings.client_id:
        if lock.reason == "held":
            print(
                "codex-rpc: monitor уже запущен; Codex будет запущен без второго Presence-клиента",
                file=sys.stderr,
            )
        else:
            print(
                f"codex-rpc: lock недоступен ({lock.path}: {lock.reason or 'неизвестная ошибка'}); Presence отключена",
                file=sys.stderr,
            )
    try:
        try:
            return run_codex(settings, arguments)
        except OSError as error:
            print(f"codex-rpc: не удалось запустить Codex: {error}", file=sys.stderr)
            return 127
    finally:
        if worker is not None:
            worker.stop()
        if owns_lock:
            lock.release()


if __name__ == "__main__":
    raise SystemExit(main())
