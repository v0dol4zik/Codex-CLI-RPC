"""Linux process discovery for the background Codex monitor."""

from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import time


@dataclass(frozen=True)
class CodexProcess:
    """A running Codex process and its stable identity."""

    pid: int
    start_time: int
    start_ticks: int
    executable: Path

    @property
    def identity(self) -> tuple[int, int]:
        """Return a PID-reuse-safe identity for this process."""

        return self.pid, self.start_ticks


def _boot_time(proc_root: Path) -> float:
    """Read the kernel boot timestamp from ``/proc/stat``."""

    try:
        for line in (proc_root / "stat").read_text(encoding="ascii").splitlines():
            if line.startswith("btime "):
                return float(line.split()[1])
    except (OSError, ValueError, IndexError):
        pass
    # A restricted /proc is unusual but should not prevent monitoring.  The
    # fallback still gives a useful timestamp for the process discovery event.
    return time.time()


def _clock_ticks() -> int:
    try:
        value = int(os.sysconf("SC_CLK_TCK"))
    except (OSError, ValueError):
        value = 100
    return max(value, 1)


def _process_stat(pid_dir: Path) -> tuple[int, int]:
    """Read the parent PID and start ticks from ``/proc/<pid>/stat``."""

    text = (pid_dir / "stat").read_text(encoding="ascii")
    # The command name is enclosed in parentheses and may contain spaces.
    _, fields = text.rsplit(") ", 1)
    values = fields.split()
    # ``values[0]`` is state (field 3), so PPID is index 1 and process
    # start time (field 22) is index 19 here.
    if len(values) <= 19:
        raise ValueError("/proc stat has no process start time")
    return int(values[1]), int(values[19])


def _standalone_release_root(executable: Path) -> Path | None:
    """Return the shared releases directory for a standalone Codex binary."""

    if executable.name != "codex" or executable.parent.name != "bin":
        return None
    try:
        release_root = executable.parents[2]
    except IndexError:
        return None
    return release_root if release_root.name == "releases" else None


def _matches_codex_executable(executable: Path, target: Path) -> bool:
    if executable == target:
        return True
    # A standalone update can move the ``codex`` symlink to a new release
    # while an older release process is still alive. Only accept the old binary
    # when both paths belong to the same standalone releases directory.
    target_root = _standalone_release_root(target)
    return target_root is not None and _standalone_release_root(executable) == target_root


def _is_internal_worker(pid_dir: Path) -> bool:
    """Identify Codex's internal sandbox mode, which shares the main binary."""

    try:
        argv0 = (pid_dir / "cmdline").read_bytes().split(b"\0", 1)[0]
    except OSError:
        return False
    return Path(os.fsdecode(argv0)).name == "codex-linux-sandbox"


def find_codex_processes(
    codex_binary: str | os.PathLike[str],
    *,
    proc_root: str | os.PathLike[str] = "/proc",
) -> list[CodexProcess]:
    """Find root sessions whose executable is the configured Codex binary.

    Comparing ``/proc/<pid>/exe`` with the resolved binary avoids false
    positives from unrelated commands that merely contain ``codex`` in their
    command line. Processes can disappear while the directory is scanned, so
    all individual read errors are intentionally ignored. Internal sandbox
    workers and child Codex processes are excluded so they cannot reset the
    Rich Presence timer.
    """

    root = Path(proc_root)
    try:
        target = Path(codex_binary).expanduser().resolve(strict=False)
    except (OSError, RuntimeError):
        return []

    boot_time = _boot_time(root)
    clock_ticks = _clock_ticks()
    matches: list[tuple[CodexProcess, int]] = []
    try:
        entries = list(root.iterdir())
    except OSError:
        return []

    for pid_dir in entries:
        if not pid_dir.name.isdecimal():
            continue
        try:
            pid = int(pid_dir.name)
            executable = (pid_dir / "exe").resolve(strict=True)
            if not _matches_codex_executable(executable, target) or _is_internal_worker(pid_dir):
                continue
            parent_pid, ticks = _process_stat(pid_dir)
        except (OSError, RuntimeError, ValueError):
            continue
        started = int(boot_time + (ticks / clock_ticks))
        matches.append((CodexProcess(pid, started, ticks, executable), parent_pid))

    matched_pids = {process.pid for process, _ in matches}
    found = [process for process, parent_pid in matches if parent_pid not in matched_pids]
    return sorted(found, key=lambda process: (process.start_time, process.start_ticks, process.pid))


def newest_codex_process(processes: list[CodexProcess]) -> CodexProcess | None:
    """Select the newest process, preserving the v0.1 single-activity model."""

    return max(processes, key=lambda process: (process.start_time, process.start_ticks, process.pid), default=None)
