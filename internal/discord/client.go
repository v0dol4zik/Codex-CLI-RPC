package discord

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

type Client struct {
	ClientID   string
	RuntimeDir string
	Timeout    time.Duration
	connection net.Conn
	socketPath string
}

func NewClient(clientID, runtimeDir string, timeout time.Duration) *Client {
	return &Client{ClientID: clientID, RuntimeDir: runtimeDir, Timeout: timeout}
}

func (client *Client) Connected() bool    { return client.connection != nil }
func (client *Client) SocketPath() string { return client.socketPath }

func (client *Client) Connect() error {
	_ = client.Close()
	if client.ClientID == "" {
		return &Error{Message: "Discord client ID cannot be empty"}
	}
	paths, err := FindSockets(client.RuntimeDir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return &Error{Message: "Discord IPC socket was not found"}
	}
	var lastError error
	for _, path := range paths {
		if err := client.connectPath(path); err == nil {
			return nil
		} else {
			lastError = err
		}
	}
	return lastError
}

func (client *Client) connectPath(path string) error {
	_ = client.Close()
	connection, err := net.DialTimeout("unix", path, client.timeout())
	if err != nil {
		return &Error{Message: fmt.Sprintf("Discord IPC connection failed: %v", err)}
	}
	if err := connection.SetDeadline(time.Now().Add(client.timeout())); err != nil {
		_ = connection.Close()
		return &Error{Message: fmt.Sprintf("set Discord IPC deadline: %v", err)}
	}
	client.connection = connection
	client.socketPath = path
	if err := client.send(OpHandshake, map[string]any{"v": 1, "client_id": client.ClientID}); err != nil {
		_ = client.Close()
		return err
	}
	for {
		opcode, payload, err := ReadFrame(connection)
		if err != nil {
			_ = client.Close()
			return err
		}
		switch opcode {
		case OpPing:
			if err := client.send(OpPong, payload); err != nil {
				_ = client.Close()
				return err
			}
		case OpClose:
			_ = client.Close()
			return &Error{Message: fmt.Sprintf("Discord rejected the handshake: %v", payload)}
		case OpFrame:
			if event, _ := payload["evt"].(string); event == "READY" {
				_ = connection.SetDeadline(time.Time{})
				return nil
			}
			_ = client.Close()
			return &Error{Message: fmt.Sprintf("Discord handshake failed: %v", payload)}
		default:
			_ = client.Close()
			return &Error{Message: fmt.Sprintf("unexpected Discord IPC opcode: %d", opcode)}
		}
	}
}

func (client *Client) SetActivity(activity map[string]any, pid int) error {
	connection := client.connection
	if connection == nil {
		return &Error{Message: "Discord IPC is not connected"}
	}
	nonce, err := randomNonce()
	if err != nil {
		return err
	}
	if pid == 0 {
		pid = os.Getpid()
	}
	_ = connection.SetDeadline(time.Now().Add(client.timeout()))
	defer connection.SetDeadline(time.Time{})
	if err := client.send(OpFrame, map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  map[string]any{"pid": pid, "activity": activity},
		"nonce": nonce,
	}); err != nil {
		_ = client.Close()
		return err
	}
	for {
		opcode, payload, err := ReadFrame(connection)
		if err != nil {
			_ = client.Close()
			return err
		}
		switch opcode {
		case OpPing:
			if err := client.send(OpPong, payload); err != nil {
				_ = client.Close()
				return err
			}
		case OpClose:
			_ = client.Close()
			return &Error{Message: fmt.Sprintf("Discord closed the IPC connection: %v", payload)}
		case OpFrame:
			if event, _ := payload["evt"].(string); event == "ERROR" {
				message := "Discord rejected SET_ACTIVITY"
				if data, ok := payload["data"].(map[string]any); ok {
					if value, ok := data["message"].(string); ok && value != "" {
						message = value
					}
				}
				_ = client.Close()
				return &Error{Message: message}
			}
			if responseNonce, _ := payload["nonce"].(string); responseNonce == nonce {
				return nil
			}
		default:
			_ = client.Close()
			return &Error{Message: fmt.Sprintf("unexpected Discord IPC opcode: %d", opcode)}
		}
	}
}

func (client *Client) ClearActivity(pid int) error { return client.SetActivity(nil, pid) }

func (client *Client) send(opcode uint32, payload any) error {
	if client.connection == nil {
		return &Error{Message: "Discord IPC is not connected"}
	}
	frame, err := EncodeFrame(opcode, payload)
	if err != nil {
		return err
	}
	if _, err := io.Copy(client.connection, bytes.NewReader(frame)); err != nil {
		return &Error{Message: fmt.Sprintf("Discord IPC write failed: %v", err)}
	}
	return nil
}

func (client *Client) Close() error {
	if client.connection == nil {
		return nil
	}
	err := client.connection.Close()
	client.connection = nil
	client.socketPath = ""
	return err
}

func (client *Client) timeout() time.Duration {
	if client.Timeout <= 0 {
		return time.Second
	}
	return client.Timeout
}

func randomNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", &Error{Message: fmt.Sprintf("generate Discord nonce: %v", err)}
	}
	return hex.EncodeToString(value), nil
}
