from __future__ import annotations

import os
from pathlib import Path
import tempfile
import unittest

import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from codex_discord_rpc.processes import (  # noqa: E402
    CodexProcess,
    find_codex_processes,
    newest_codex_process,
)


class ProcessTests(unittest.TestCase):
    def test_find_processes_matches_executable_and_reads_start_time(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "releases" / "current" / "bin" / "codex"
            target.parent.mkdir(parents=True)
            target.write_text("binary")
            target.chmod(0o755)
            (root / "stat").write_text("btime 1000\n")

            process_dir = root / "4242"
            process_dir.mkdir()
            (process_dir / "exe").symlink_to(target)
            fields = ["S", *(["0"] * 18), "250"]
            (process_dir / "stat").write_text(f"4242 (codex) {' '.join(fields)}\n")

            other_dir = root / "4243"
            other_dir.mkdir()
            other = root / "other"
            other.write_text("other")
            (other_dir / "exe").symlink_to(other)
            (other_dir / "stat").write_text(f"4243 (other) {' '.join(fields)}\n")

            old_dir = root / "4244"
            old_dir.mkdir()
            old_binary = root / "releases" / "previous" / "bin" / "codex"
            old_binary.parent.mkdir(parents=True)
            old_binary.write_text("older binary")
            old_binary.chmod(0o755)
            (old_dir / "exe").symlink_to(old_binary)
            (old_dir / "stat").write_text(f"4244 (codex) {' '.join(fields[:-1] + ['350'])}\n")

            unrelated_dir = root / "4245"
            unrelated_dir.mkdir()
            unrelated_binary = root / "unrelated" / "bin" / "codex"
            unrelated_binary.parent.mkdir(parents=True)
            unrelated_binary.write_text("unrelated binary")
            unrelated_binary.chmod(0o755)
            (unrelated_dir / "exe").symlink_to(unrelated_binary)
            (unrelated_dir / "stat").write_text(f"4245 (codex) {' '.join(fields)}\n")

            worker_dir = root / "4246"
            worker_dir.mkdir()
            (worker_dir / "exe").symlink_to(target)
            (worker_dir / "stat").write_text(f"4246 (codex) {' '.join(fields)}\n")
            (worker_dir / "cmdline").write_bytes(b"codex-linux-sandbox\0--sandbox-policy\0")

            found = find_codex_processes(target, proc_root=root)

            self.assertEqual([process.pid for process in found], [4242, 4244])
            expected = int(1000 + 250 / int(os.sysconf("SC_CLK_TCK")))
            self.assertEqual(found[0].start_time, expected)
            self.assertEqual(found[0].identity, (4242, 250))

    def test_child_from_previous_standalone_release_is_excluded(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "releases" / "current" / "bin" / "codex"
            target.parent.mkdir(parents=True)
            target.write_text("binary")
            target.chmod(0o755)
            (root / "stat").write_text("btime 1000\n")

            parent_dir = root / "4242"
            parent_dir.mkdir()
            (parent_dir / "exe").symlink_to(target)
            parent_fields = ["S", "1", *(["0"] * 17), "250"]
            (parent_dir / "stat").write_text(f"4242 (codex) {' '.join(parent_fields)}\n")

            old_binary = root / "releases" / "previous" / "bin" / "codex"
            old_binary.parent.mkdir(parents=True)
            old_binary.write_text("older binary")
            old_binary.chmod(0o755)
            child_dir = root / "4243"
            child_dir.mkdir()
            (child_dir / "exe").symlink_to(old_binary)
            child_fields = ["S", "4242", *(["0"] * 17), "350"]
            (child_dir / "stat").write_text(f"4243 (codex) {' '.join(child_fields)}\n")

            found = find_codex_processes(target, proc_root=root)

            self.assertEqual([process.pid for process in found], [4242])

    def test_newest_process_is_selected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            processes = [
                CodexProcess(1, 100, 10, path / "codex"),
                CodexProcess(2, 200, 20, path / "codex"),
            ]
            self.assertEqual(newest_codex_process(processes).pid, 2)


if __name__ == "__main__":
    unittest.main()
