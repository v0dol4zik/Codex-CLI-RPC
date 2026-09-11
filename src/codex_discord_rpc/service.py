"""User-systemd service management for the Codex RPC monitor."""

from __future__ import annotations

import os
from pathlib import Path
import subprocess
import tempfile


SERVICE_NAME = "codex-discord-rpc.service"
LEGACY_WANTS_DIRECTORY = "default.target.wants"
SERVICE_UNIT = """[Unit]
Description=Discord Rich Presence monitor for Codex CLI
After=graphical-session.target
PartOf=graphical-session.target

[Service]
Type=simple
ExecStart=%h/.local/bin/codex-rpc --monitor
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Restart=always
RestartSec=2
TimeoutStopSec=5

[Install]
WantedBy=graphical-session.target
"""


class ServiceError(RuntimeError):
    """Raised when user-systemd management cannot be completed."""


def service_directory() -> Path:
    configured = os.environ.get("CODEX_RPC_SYSTEMD_DIR")
    if configured:
        return Path(configured).expanduser()
    return Path.home() / ".config" / "systemd" / "user"


def service_path() -> Path:
    return service_directory() / SERVICE_NAME


def _remove_legacy_install_symlink() -> None:
    # Releases up to 0.1.0 shipped WantedBy=default.target alongside
    # Wants=graphical-session.target, so the unit pulled that target up on any
    # login - a plain tty included - and the next real desktop session aborted
    # with "A graphical session is already running!". `systemctl enable` only
    # adds the new symlink, so the stale one has to be dropped explicitly or an
    # upgraded install keeps starting outside a graphical session.
    legacy = service_directory() / LEGACY_WANTS_DIRECTORY / SERVICE_NAME
    try:
        legacy.unlink()
    except FileNotFoundError:
        pass


def _systemctl(*arguments: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    try:
        result = subprocess.run(
            ["systemctl", "--user", *arguments],
            check=False,
            capture_output=True,
            text=True,
        )
    except OSError as error:
        raise ServiceError(f"Не удалось запустить systemctl: {error}") from error
    if check and result.returncode != 0:
        details = (result.stderr or result.stdout).strip()
        raise ServiceError(details or f"systemctl завершился с кодом {result.returncode}")
    return result


def _unit_source() -> str:
    # The installer keeps the source unit beside the installed package. The
    # embedded copy also makes the command usable from a source checkout that
    # has not been installed yet.
    candidate = Path(__file__).resolve().parents[2] / "systemd" / SERVICE_NAME
    try:
        return candidate.read_text(encoding="utf-8")
    except OSError:
        return SERVICE_UNIT


def install_service() -> Path:
    path = service_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary_name = tempfile.mkstemp(prefix=f".{SERVICE_NAME}.", dir=path.parent, text=True)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as temporary:
            temporary.write(_unit_source())
            temporary.flush()
            os.fsync(temporary.fileno())
        os.chmod(temporary_name, 0o644)
        os.replace(temporary_name, path)
    finally:
        try:
            os.unlink(temporary_name)
        except FileNotFoundError:
            pass
    _remove_legacy_install_symlink()
    _systemctl("daemon-reload")
    _systemctl("enable", SERVICE_NAME)
    _systemctl("restart", SERVICE_NAME)
    return path


def uninstall_service() -> Path:
    path = service_path()
    # A missing or inactive unit is not an uninstall failure.
    _systemctl("disable", "--now", SERVICE_NAME, check=False)
    if path.exists():
        path.unlink()
    _systemctl("daemon-reload")
    return path


def service_status() -> subprocess.CompletedProcess[str]:
    return _systemctl("status", "--no-pager", "--full", SERVICE_NAME, check=False)


def service_action(action: str) -> subprocess.CompletedProcess[str] | Path:
    if action == "install":
        return install_service()
    if action == "uninstall":
        return uninstall_service()
    if action == "status":
        return service_status()
    if action in {"start", "stop", "restart"}:
        return _systemctl(action, SERVICE_NAME)
    raise ServiceError(f"Неизвестное действие service: {action}")
