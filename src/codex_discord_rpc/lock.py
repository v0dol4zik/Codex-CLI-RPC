"""Single-instance lock shared by the monitor and the legacy wrapper."""

from __future__ import annotations

import fcntl
import os
from pathlib import Path
import tempfile


def default_lock_path() -> Path:
    """Return a per-user runtime lock path."""

    standard_runtime_dir = Path(f"/run/user/{os.getuid()}")
    if standard_runtime_dir.is_dir():
        return standard_runtime_dir / "codex-discord-rpc.lock"
    runtime_dir = os.environ.get("XDG_RUNTIME_DIR")
    if runtime_dir:
        return Path(runtime_dir) / "codex-discord-rpc.lock"
    return Path(tempfile.gettempdir()) / f"codex-discord-rpc-{os.getuid()}.lock"


class InstanceLock:
    """An advisory, non-blocking lock held for the process lifetime."""

    def __init__(self, path: str | os.PathLike[str] | None = None) -> None:
        self.path = Path(path).expanduser() if path else default_lock_path()
        self._file = None
        self.reason: str | None = None

    @property
    def acquired(self) -> bool:
        return self._file is not None

    def acquire(self) -> bool:
        if self._file is not None:
            return True
        self.reason = None
        try:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            flags = os.O_RDWR | os.O_CREAT | os.O_CLOEXEC
            if hasattr(os, "O_NOFOLLOW"):
                flags |= os.O_NOFOLLOW
            descriptor = os.open(self.path, flags, 0o600)
            os.fchmod(descriptor, 0o600)
            file_handle = os.fdopen(descriptor, "a+")
            try:
                fcntl.flock(file_handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                file_handle.close()
                self.reason = "held"
                return False
        except OSError as error:
            self.reason = str(error)
            return False
        self._file = file_handle
        return True

    def release(self) -> None:
        file_handle = self._file
        self._file = None
        if file_handle is None:
            return
        try:
            fcntl.flock(file_handle.fileno(), fcntl.LOCK_UN)
        except OSError:
            pass
        finally:
            file_handle.close()

    def __enter__(self) -> "InstanceLock":
        if not self.acquire():
            raise RuntimeError(f"Не удалось получить lock: {self.path}")
        return self

    def __exit__(self, _exc_type: object, _exc: object, _traceback: object) -> None:
        self.release()
