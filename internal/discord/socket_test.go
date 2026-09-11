package discord

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFindSocketUsesExplicitDirectory(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "discord-ipc-0")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := FindSocket(directory)
	if err != nil || found != "" {
		t.Fatalf("regular-file result = %q, %v", found, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("Unix sockets are unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	found, err = FindSocket(directory)
	if err != nil || found != path {
		t.Fatalf("socket result = %q, %v", found, err)
	}
}
