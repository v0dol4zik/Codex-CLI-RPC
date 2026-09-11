from __future__ import annotations

import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from codex_discord_rpc import __version__  # noqa: E402
from codex_discord_rpc.cli import (  # noqa: E402
    CodexMonitor,
    PresenceWorker,
    Settings,
    load_config,
    load_settings,
    resolve_codex_binary,
    run_codex,
)
from codex_discord_rpc.discord_ipc import DiscordIpcError  # noqa: E402
from codex_discord_rpc.processes import CodexProcess  # noqa: E402


class CliTests(unittest.TestCase):
    def test_resolve_codex_binary_honours_explicit_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            executable = Path(directory) / "example-codex"
            executable.write_text("#!/bin/sh\nexit 0\n")
            executable.chmod(executable.stat().st_mode | stat.S_IXUSR)
            previous = os.environ.get("CODEX_BINARY")
            os.environ["CODEX_BINARY"] = str(executable)
            try:
                self.assertEqual(resolve_codex_binary(), str(executable.resolve()))
            finally:
                if previous is None:
                    os.environ.pop("CODEX_BINARY", None)
                else:
                    os.environ["CODEX_BINARY"] = previous

    def test_load_settings_without_client_id(self) -> None:
        old_id = os.environ.pop("CODEX_DISCORD_CLIENT_ID", None)
        old_binary = os.environ.get("CODEX_BINARY")
        old_config = os.environ.get("CODEX_RPC_CONFIG")
        with tempfile.TemporaryDirectory() as directory:
            os.environ["CODEX_BINARY"] = sys.executable
            os.environ["CODEX_RPC_CONFIG"] = str(Path(directory) / "missing.toml")
            try:
                settings = load_settings()
                self.assertIsNone(settings.client_id)
                self.assertEqual(Path(settings.codex_binary).resolve(), Path(sys.executable).resolve())
            finally:
                if old_id is not None:
                    os.environ["CODEX_DISCORD_CLIENT_ID"] = old_id
                if old_binary is None:
                    os.environ.pop("CODEX_BINARY", None)
                else:
                    os.environ["CODEX_BINARY"] = old_binary
                if old_config is None:
                    os.environ.pop("CODEX_RPC_CONFIG", None)
                else:
                    os.environ["CODEX_RPC_CONFIG"] = old_config

    def test_run_codex_preserves_exit_code_and_arguments(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "args.txt"
            script = Path(directory) / "fake-codex.py"
            script.write_text(
                "import pathlib, sys\n"
                "pathlib.Path(sys.argv[1]).write_text(repr(sys.argv[2:]))\n"
                "raise SystemExit(7)\n"
            )
            settings = Settings(
                client_id=None,
                codex_binary=sys.executable,
                details="",
                state="",
                large_image=None,
                large_text=None,
                retry_seconds=0.25,
                refresh_seconds=5,
                runtime_dir=None,
            )
            code = run_codex(settings, [str(script), str(output), "--one", "два"])
            self.assertEqual(code, 7)
            self.assertEqual(output.read_text(), repr(["--one", "два"]))

    def test_presence_activity_keeps_original_timestamp(self) -> None:
        settings = Settings(None, sys.executable, "details", "state", None, None, 0.25, 5, None)
        worker = PresenceWorker(settings)
        first = worker.started_at
        self.assertEqual(worker._activity()["timestamps"], {"start": first})

    def test_presence_worker_accepts_process_start_timestamp(self) -> None:
        settings = Settings(None, sys.executable, "details", "state", None, None, 0.25, 5, None)
        worker = PresenceWorker(settings, started_at=1234)
        self.assertEqual(worker._activity()["timestamps"], {"start": 1234})

    def test_large_text_requires_large_image(self) -> None:
        settings = Settings(None, sys.executable, "details", "state", None, "tooltip", 0.25, 5, None)
        worker = PresenceWorker(settings)
        self.assertNotIn("assets", worker._activity())

    def test_config_file_and_environment_precedence(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.toml"
            config_path.write_text(
                'client_id = "123456789"\n'
                'codex_binary = "' + sys.executable + '"\n'
                'details = "from config"\n'
                'retry_seconds = 0.1\n'
            )
            previous = {
                name: os.environ.get(name)
                for name in ("CODEX_RPC_CONFIG", "CODEX_RPC_DETAILS", "CODEX_BINARY")
            }
            os.environ["CODEX_RPC_CONFIG"] = str(config_path)
            os.environ["CODEX_RPC_DETAILS"] = "from environment"
            os.environ.pop("CODEX_BINARY", None)
            try:
                settings = load_settings()
            finally:
                for name, value in previous.items():
                    if value is None:
                        os.environ.pop(name, None)
                    else:
                        os.environ[name] = value
            self.assertEqual(settings.client_id, "123456789")
            self.assertEqual(settings.details, "from environment")
            self.assertEqual(settings.retry_seconds, 0.25)

    def test_malformed_config_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.toml"
            config_path.write_text("invalid = [\n")
            previous = os.environ.get("CODEX_RPC_CONFIG")
            os.environ["CODEX_RPC_CONFIG"] = str(config_path)
            try:
                with self.assertRaisesRegex(ValueError, "Не удалось прочитать"):
                    load_config()
            finally:
                if previous is None:
                    os.environ.pop("CODEX_RPC_CONFIG", None)
                else:
                    os.environ["CODEX_RPC_CONFIG"] = previous

    def test_presence_worker_reconnects_without_resetting_timestamp(self) -> None:
        class FakeClient:
            def __init__(self) -> None:
                self.connected = False
                self.connect_count = 0
                self.activity_count = 0
                self.success = threading.Event()

            def connect(self) -> None:
                self.connected = True
                self.connect_count += 1

            def set_activity(self, activity: object) -> None:
                if activity is None:
                    return
                self.activity_count += 1
                if self.activity_count == 1:
                    raise DiscordIpcError("simulated restart")
                self.success.set()

            def close(self) -> None:
                self.connected = False

        settings = Settings("123", sys.executable, "details", "state", None, None, 0.01, 0.05, None)
        worker = PresenceWorker(settings)
        fake = FakeClient()
        worker.client = fake  # type: ignore[assignment]
        started_at = worker.started_at
        worker.start()
        self.assertTrue(fake.success.wait(timeout=2))
        worker.stop()
        self.assertGreaterEqual(fake.connect_count, 2)
        self.assertEqual(worker.started_at, started_at)

    def test_monitor_attaches_to_existing_process_and_clears_on_exit(self) -> None:
        settings = Settings("123", sys.executable, "details", "state", None, None, 0.25, 5, None, 0.01)
        process = CodexProcess(42, 1234, 5678, Path(sys.executable))
        snapshots = [[process], [process], []]
        stop_event = threading.Event()
        workers: list[object] = []

        class FakeWorker:
            def __init__(self, _settings: Settings, *, started_at: int) -> None:
                self.started_at = started_at
                self.started = False
                self.stopped = False
                workers.append(self)

            def start(self) -> None:
                self.started = True

            def stop(self) -> None:
                self.stopped = True

        def find(_binary: str) -> list[CodexProcess]:
            snapshot = snapshots.pop(0) if snapshots else []
            if not snapshots:
                stop_event.set()
            return snapshot

        monitor = CodexMonitor(settings, process_finder=find, worker_factory=FakeWorker, logger=lambda _message: None)
        self.assertEqual(monitor.run(stop_event), 0)
        self.assertEqual(len(workers), 1)
        self.assertEqual(workers[0].started_at, 1234)
        self.assertTrue(workers[0].started)
        self.assertTrue(workers[0].stopped)

    def test_terminal_sigint_reaches_child_once(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "signals.txt"
            child = Path(directory) / "child.py"
            child.write_text(
                "import pathlib, signal, sys, time\n"
                "marker = pathlib.Path(sys.argv[1])\n"
                "def stop(*_args):\n"
                "    previous = marker.read_text() if marker.exists() else ''\n"
                "    marker.write_text(previous + 'I')\n"
                "    raise SystemExit(9)\n"
                "signal.signal(signal.SIGINT, stop)\n"
                "marker.write_text('ready:')\n"
                "while True: time.sleep(0.05)\n"
            )
            runner = Path(__file__).resolve().parents[1] / "bin" / "codex-rpc"
            environment = os.environ.copy()
            environment["CODEX_BINARY"] = sys.executable
            environment["CODEX_RPC_CONFIG"] = str(Path(directory) / "missing.toml")
            process = subprocess.Popen(
                [sys.executable, str(runner), str(child), str(marker)],
                env=environment,
                start_new_session=True,
            )
            try:
                deadline = time.monotonic() + 3
                while time.monotonic() < deadline and not marker.exists():
                    time.sleep(0.02)
                self.assertTrue(marker.exists(), "child did not start")
                os.killpg(process.pid, signal.SIGINT)
                self.assertEqual(process.wait(timeout=3), 9)
                self.assertEqual(marker.read_text(), "ready:I")
            finally:
                if process.poll() is None:
                    process.terminate()
                    process.wait(timeout=3)

    def test_installer_creates_working_launcher(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "share"
            bin_dir = Path(directory) / "bin"
            environment = os.environ.copy()
            environment["CODEX_RPC_INSTALL_ROOT"] = str(root)
            environment["CODEX_RPC_BIN_DIR"] = str(bin_dir)
            environment["CODEX_RPC_ENABLE_SERVICE"] = "0"
            environment["CODEX_RPC_MANAGE_SERVICE"] = "0"
            environment["CODEX_RPC_SYSTEMD_DIR"] = str(Path(directory) / "systemd")
            subprocess.run(
                ["bash", str(Path(__file__).resolve().parents[1] / "scripts" / "install.sh")],
                env=environment,
                check=True,
                capture_output=True,
                text=True,
            )
            launcher = bin_dir / "codex-rpc"
            self.assertTrue(launcher.is_symlink())
            environment["CODEX_BINARY"] = sys.executable
            environment["CODEX_RPC_CONFIG"] = str(Path(directory) / "missing.toml")
            result = subprocess.run(
                [str(launcher), "--rpc-version"],
                env=environment,
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.stdout.strip(), __version__)
            self.assertTrue((Path(directory) / "systemd" / "codex-discord-rpc.service").is_file())
            self.assertTrue((root / "systemd" / "codex-discord-rpc.service").is_file())

            config_sentinel = Path(directory) / "config.toml"
            config_sentinel.write_text('client_id = "kept"\n')
            subprocess.run(
                ["bash", str(Path(__file__).resolve().parents[1] / "scripts" / "uninstall.sh")],
                env=environment,
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertFalse(root.exists())
            self.assertFalse(launcher.exists())
            self.assertFalse((Path(directory) / "systemd" / "codex-discord-rpc.service").exists())
            self.assertTrue(config_sentinel.is_file())


if __name__ == "__main__":
    unittest.main()
