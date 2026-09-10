"""Small, dependency-free client for Discord's local IPC protocol."""

from __future__ import annotations

import json
import os
from pathlib import Path
import socket
import stat
import struct
import tempfile
from typing import Any, Iterable


IPC_HEADER = struct.Struct("<II")
OP_HANDSHAKE = 0
OP_FRAME = 1
OP_CLOSE = 2
OP_PING = 3
OP_PONG = 4
MAX_FRAME_SIZE = 8 * 1024 * 1024


class DiscordIpcError(RuntimeError):
    """Raised when Discord IPC cannot be used."""


def encode_frame(opcode: int, payload: dict[str, Any]) -> bytes:
    """Encode one Discord IPC frame."""

    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    return IPC_HEADER.pack(opcode, len(body)) + body


def decode_frame(frame: bytes) -> tuple[int, dict[str, Any]]:
    """Decode one complete Discord IPC frame."""

    if len(frame) < IPC_HEADER.size:
        raise DiscordIpcError("Discord IPC frame is shorter than its header")
    opcode, size = IPC_HEADER.unpack_from(frame)
    body = frame[IPC_HEADER.size:]
    if size != len(body):
        raise DiscordIpcError(f"Discord IPC frame size mismatch: expected {size}, got {len(body)}")
    if size > MAX_FRAME_SIZE:
        raise DiscordIpcError("Discord IPC frame is too large")
    try:
        payload = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise DiscordIpcError("Discord IPC payload is not valid JSON") from error
    if not isinstance(payload, dict):
        raise DiscordIpcError("Discord IPC payload must be a JSON object")
    return opcode, payload


def _read_exact(sock: socket.socket, size: int) -> bytes:
    chunks: list[bytes] = []
    remaining = size
    while remaining:
        chunk = sock.recv(remaining)
        if not chunk:
            raise DiscordIpcError("Discord closed the IPC socket")
        chunks.append(chunk)
        remaining -= len(chunk)
    return b"".join(chunks)


def recv_frame(sock: socket.socket) -> tuple[int, dict[str, Any]]:
    """Read and decode one frame from a connected socket."""

    header = _read_exact(sock, IPC_HEADER.size)
    opcode, size = IPC_HEADER.unpack(header)
    if size > MAX_FRAME_SIZE:
        raise DiscordIpcError("Discord IPC frame is too large")
    body = _read_exact(sock, size)
    return decode_frame(header + body)


def _candidate_directories(runtime_dir: str | os.PathLike[str] | None = None) -> Iterable[Path]:
    # An explicitly supplied directory is authoritative.  Besides being less
    # surprising for users, this keeps diagnostics and tests from accidentally
    # discovering a socket belonging to another runtime directory.
    if runtime_dir:
        yield Path(runtime_dir)
        return
    directories: list[Path] = []
    xdg_runtime_dir = os.environ.get("XDG_RUNTIME_DIR")
    values = (
        xdg_runtime_dir,
        Path(xdg_runtime_dir) / "app/com.discordapp.Discord" if xdg_runtime_dir else None,
        Path(xdg_runtime_dir) / "app/com.discordapp.DiscordCanary" if xdg_runtime_dir else None,
        Path(xdg_runtime_dir) / "snap.discord" if xdg_runtime_dir else None,
        f"/run/user/{os.getuid()}",
        os.environ.get("TMPDIR"),
        os.environ.get("TMP"),
        os.environ.get("TEMP"),
        tempfile.gettempdir(),
    )
    for value in values:
        if value:
            path = Path(value)
            if path not in directories:
                directories.append(path)
    yield from directories


def find_ipc_socket(runtime_dir: str | os.PathLike[str] | None = None) -> Path | None:
    """Return the first existing Discord IPC socket, if any."""

    for directory in _candidate_directories(runtime_dir):
        for index in range(10):
            path = directory / f"discord-ipc-{index}"
            try:
                mode = path.stat().st_mode
            except OSError:
                continue
            if stat.S_ISSOCK(mode):
                return path
    return None


class DiscordIpcClient:
    """Synchronous Discord IPC connection used by the presence worker."""

    def __init__(self, client_id: str, *, runtime_dir: str | os.PathLike[str] | None = None, timeout: float = 1.0) -> None:
        if not client_id.strip():
            raise ValueError("Discord client ID cannot be empty")
        self.client_id = client_id.strip()
        self.runtime_dir = runtime_dir
        self.timeout = timeout
        self.socket: socket.socket | None = None
        self.socket_path: Path | None = None

    @property
    def connected(self) -> bool:
        return self.socket is not None

    def connect(self) -> None:
        self.close()
        path = find_ipc_socket(self.runtime_dir)
        if path is None:
            raise DiscordIpcError("Discord IPC socket was not found")
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(self.timeout)
        try:
            sock.connect(str(path))
            sock.sendall(encode_frame(OP_HANDSHAKE, {"v": 1, "client_id": self.client_id}))
            while True:
                opcode, payload = recv_frame(sock)
                if opcode == OP_PING:
                    sock.sendall(encode_frame(OP_PONG, payload))
                    continue
                if opcode == OP_CLOSE:
                    raise DiscordIpcError(f"Discord rejected the handshake: {payload}")
                if opcode != OP_FRAME or payload.get("evt") != "READY":
                    raise DiscordIpcError(f"Discord handshake failed: {payload}")
                break
        except (OSError, DiscordIpcError) as error:
            sock.close()
            if isinstance(error, DiscordIpcError):
                raise
            raise DiscordIpcError(f"Discord IPC connection failed: {error}") from error
        sock.settimeout(self.timeout)
        self.socket = sock
        self.socket_path = path

    def _send(self, opcode: int, payload: dict[str, Any]) -> None:
        if self.socket is None:
            raise DiscordIpcError("Discord IPC is not connected")
        try:
            self.socket.sendall(encode_frame(opcode, payload))
        except OSError as error:
            self.close()
            raise DiscordIpcError(f"Discord IPC write failed: {error}") from error

    def _receive_response(self, nonce: str) -> dict[str, Any]:
        if self.socket is None:
            raise DiscordIpcError("Discord IPC is not connected")
        try:
            while True:
                opcode, payload = recv_frame(self.socket)
                if opcode == OP_PING:
                    self._send(OP_PONG, payload)
                    continue
                if opcode == OP_CLOSE:
                    raise DiscordIpcError(f"Discord closed the IPC connection: {payload}")
                if opcode != OP_FRAME:
                    raise DiscordIpcError(f"Unexpected Discord IPC opcode: {opcode}")
                if payload.get("evt") == "ERROR":
                    data = payload.get("data")
                    message = data.get("message") if isinstance(data, dict) else None
                    raise DiscordIpcError(str(message or "Discord rejected SET_ACTIVITY"))
                if payload.get("nonce") == nonce:
                    return payload
        except OSError as error:
            raise DiscordIpcError(f"Discord IPC read failed: {error}") from error

    def set_activity(self, activity: dict[str, Any] | None, *, pid: int | None = None, nonce: str | None = None) -> None:
        """Set or clear the activity for this process."""

        import uuid

        request_nonce = nonce or str(uuid.uuid4())
        self._send(
            OP_FRAME,
            {
                "cmd": "SET_ACTIVITY",
                "args": {"pid": pid if pid is not None else os.getpid(), "activity": activity},
                "nonce": request_nonce,
            },
        )
        try:
            self._receive_response(request_nonce)
        except DiscordIpcError:
            self.close()
            raise

    def close(self) -> None:
        if self.socket is not None:
            try:
                self.socket.close()
            except OSError:
                pass
        self.socket = None
        self.socket_path = None
