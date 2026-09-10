from __future__ import annotations

from pathlib import Path
import socket
import tempfile
import threading
import unittest

import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from codex_discord_rpc.discord_ipc import (  # noqa: E402
    DiscordIpcClient,
    DiscordIpcError,
    OP_FRAME,
    decode_frame,
    encode_frame,
    find_ipc_socket,
    recv_frame,
)


class DiscordIpcTests(unittest.TestCase):
    def test_frame_round_trip(self) -> None:
        payload = {"evt": "READY", "data": {"ok": True}, "text": "Привет"}
        frame = encode_frame(OP_FRAME, payload)
        self.assertEqual(decode_frame(frame), (OP_FRAME, payload))

    def test_frame_rejects_size_mismatch(self) -> None:
        frame = encode_frame(OP_FRAME, {"ok": True}) + b"x"
        with self.assertRaises(DiscordIpcError):
            decode_frame(frame)

    def test_find_socket_only_returns_unix_sockets(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "discord-ipc-0"
            path.write_text("not a socket")
            self.assertIsNone(find_ipc_socket(directory))
            path.unlink()

            server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                try:
                    server.bind(str(path))
                except PermissionError as error:
                    self.skipTest(f"sandbox does not allow Unix socket bind: {error}")
                self.assertEqual(find_ipc_socket(directory), path)
            finally:
                server.close()
                path.unlink(missing_ok=True)

    def test_client_handshake_and_set_activity(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            socket_path = Path(directory) / "discord-ipc-0"
            server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                server.bind(str(socket_path))
            except PermissionError as error:
                server.close()
                self.skipTest(f"sandbox does not allow Unix socket bind: {error}")
            server.listen(1)
            messages: list[tuple[int, dict[str, object]]] = []
            ready = threading.Event()

            def serve() -> None:
                connection, _ = server.accept()
                with connection:
                    messages.append(recv_frame(connection))
                    connection.sendall(encode_frame(OP_FRAME, {"evt": "READY", "data": {}}))
                    messages.append(recv_frame(connection))
                    request = messages[-1][1]
                    connection.sendall(
                        encode_frame(
                            OP_FRAME,
                            {"evt": None, "cmd": "SET_ACTIVITY", "data": {}, "nonce": request["nonce"]},
                        )
                    )
                    ready.set()

            thread = threading.Thread(target=serve)
            thread.start()
            try:
                client = DiscordIpcClient("123456", runtime_dir=directory)
                client.connect()
                client.set_activity({"details": "test"}, pid=42, nonce="test-nonce")
                ready.wait(timeout=2)
                client.close()
            finally:
                server.close()
                thread.join(timeout=2)

            self.assertEqual(messages[0][0], 0)
            self.assertEqual(messages[0][1], {"v": 1, "client_id": "123456"})
            self.assertEqual(messages[1][0], OP_FRAME)
            self.assertEqual(messages[1][1]["cmd"], "SET_ACTIVITY")
            self.assertEqual(messages[1][1]["args"]["pid"], 42)


if __name__ == "__main__":
    unittest.main()
