from __future__ import annotations

from pathlib import Path
import os
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from codex_discord_rpc.service import install_service, uninstall_service  # noqa: E402


class ServiceTests(unittest.TestCase):
    def test_install_and_uninstall_service_are_atomic_and_local(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            old_directory = os.environ.get("CODEX_RPC_SYSTEMD_DIR")
            os.environ["CODEX_RPC_SYSTEMD_DIR"] = directory
            try:
                completed = subprocess.CompletedProcess([], 0, "", "")
                with patch("codex_discord_rpc.service._systemctl", return_value=completed) as systemctl:
                    path = install_service()
                    self.assertTrue(path.is_file())
                    self.assertIn("ExecStart=", path.read_text())
                    self.assertEqual([call.args[0] for call in systemctl.call_args_list], [
                        "daemon-reload",
                        "enable",
                        "restart",
                    ])
                    uninstall_service()
                    self.assertFalse(path.exists())
            finally:
                if old_directory is None:
                    os.environ.pop("CODEX_RPC_SYSTEMD_DIR", None)
                else:
                    os.environ["CODEX_RPC_SYSTEMD_DIR"] = old_directory

    def test_install_drops_the_legacy_default_target_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            old_directory = os.environ.get("CODEX_RPC_SYSTEMD_DIR")
            os.environ["CODEX_RPC_SYSTEMD_DIR"] = directory
            try:
                legacy = Path(directory) / "default.target.wants" / "codex-discord-rpc.service"
                legacy.parent.mkdir(parents=True)
                legacy.symlink_to(Path(directory) / "codex-discord-rpc.service")
                completed = subprocess.CompletedProcess([], 0, "", "")
                with patch("codex_discord_rpc.service._systemctl", return_value=completed):
                    unit = install_service().read_text()
                # The unit must attach to the graphical session, never create it.
                self.assertFalse(legacy.is_symlink())
                self.assertIn("WantedBy=graphical-session.target", unit)
                self.assertNotIn("Wants=graphical-session.target", unit)
            finally:
                if old_directory is None:
                    os.environ.pop("CODEX_RPC_SYSTEMD_DIR", None)
                else:
                    os.environ["CODEX_RPC_SYSTEMD_DIR"] = old_directory


if __name__ == "__main__":
    unittest.main()
