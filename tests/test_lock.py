from __future__ import annotations

from pathlib import Path
import stat
import tempfile
import unittest

import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from codex_discord_rpc.lock import InstanceLock  # noqa: E402


class LockTests(unittest.TestCase):
    def test_lock_allows_one_owner_and_recovers_after_release(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "rpc.lock"
            first = InstanceLock(path)
            second = InstanceLock(path)
            self.assertTrue(first.acquire())
            self.assertFalse(second.acquire())
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            first.release()
            self.assertTrue(second.acquire())
            second.release()


if __name__ == "__main__":
    unittest.main()
