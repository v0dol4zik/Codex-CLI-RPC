package discord

import (
	"errors"
	"net"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestClientHandshakeAndActivity(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "discord-ipc-0")
	listener, err := net.Listen("unix", path)
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("Unix sockets are unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()
		opcode, handshake, readErr := ReadFrame(connection)
		if readErr != nil || opcode != OpHandshake || handshake["client_id"] != "123456" {
			done <- errors.New("invalid handshake")
			return
		}
		pingPayload := map[string]any{"message": "ping"}
		ping, _ := EncodeFrame(OpPing, pingPayload)
		if _, writeErr := connection.Write(ping); writeErr != nil {
			done <- writeErr
			return
		}
		opcode, pong, readErr := ReadFrame(connection)
		if readErr != nil || opcode != OpPong || pong["message"] != "ping" {
			done <- errors.New("invalid pong")
			return
		}
		ready, _ := EncodeFrame(OpFrame, map[string]any{"evt": "READY"})
		if _, writeErr := connection.Write(ready); writeErr != nil {
			done <- writeErr
			return
		}
		opcode, activity, readErr := ReadFrame(connection)
		args, _ := activity["args"].(map[string]any)
		body, _ := args["activity"].(map[string]any)
		if readErr != nil || opcode != OpFrame || activity["cmd"] != "SET_ACTIVITY" ||
			args["pid"] != float64(42) || body["details"] != "test" {
			done <- errors.New("invalid activity")
			return
		}
		nonce, _ := activity["nonce"].(string)
		unrelated, _ := EncodeFrame(OpFrame, map[string]any{"nonce": "unrelated"})
		if _, writeErr := connection.Write(unrelated); writeErr != nil {
			done <- writeErr
			return
		}
		response, _ := EncodeFrame(OpFrame, map[string]any{"nonce": nonce, "cmd": "SET_ACTIVITY"})
		if _, writeErr := connection.Write(response); writeErr != nil {
			done <- writeErr
			return
		}
		opcode, clear, readErr := ReadFrame(connection)
		clearArgs, _ := clear["args"].(map[string]any)
		if readErr != nil || opcode != OpFrame || clear["cmd"] != "SET_ACTIVITY" || clearArgs["activity"] != nil {
			done <- errors.New("invalid activity clear")
			return
		}
		clearNonce, _ := clear["nonce"].(string)
		clearResponse, _ := EncodeFrame(OpFrame, map[string]any{"nonce": clearNonce, "cmd": "SET_ACTIVITY"})
		_, writeErr := connection.Write(clearResponse)
		done <- writeErr
	}()

	client := NewClient("123456", directory, time.Second)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := client.SetActivity(map[string]any{"details": "test"}, 42); err != nil {
		t.Fatal(err)
	}
	if err := client.ClearActivity(42); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
}

func TestClientRejectsDiscordError(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "discord-ipc-0")
	listener, err := net.Listen("unix", path)
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("Unix sockets are unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _, _ = ReadFrame(connection)
		ready, _ := EncodeFrame(OpFrame, map[string]any{"evt": "READY"})
		_, _ = connection.Write(ready)
		_, request, _ := ReadFrame(connection)
		nonce, _ := request["nonce"].(string)
		response, _ := EncodeFrame(OpFrame, map[string]any{
			"evt": "ERROR", "nonce": nonce, "data": map[string]any{"message": "rejected"},
		})
		_, _ = connection.Write(response)
	}()

	client := NewClient("123456", directory, time.Second)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := client.SetActivity(map[string]any{}, 42); err == nil || err.Error() != "rejected" {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Connected() {
		t.Fatal("client remained connected after rejection")
	}
}

func TestClientSkipsStaleSocket(t *testing.T) {
	directory := t.TempDir()
	stalePath := filepath.Join(directory, "discord-ipc-0")
	stale, err := net.Listen("unix", stalePath)
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("Unix sockets are unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "discord-ipc-1")
	active, err := net.Listen("unix", activePath)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	go func() {
		connection, acceptErr := active.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _, _ = ReadFrame(connection)
		ready, _ := EncodeFrame(OpFrame, map[string]any{"evt": "READY"})
		_, _ = connection.Write(ready)
	}()
	client := NewClient("123456", directory, time.Second)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.SocketPath() != activePath {
		t.Fatalf("connected socket = %q, want %q", client.SocketPath(), activePath)
	}
}
