package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyOneOwnerAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rpc.lock")
	first := New(path)
	second := New(path)
	if !first.Acquire() {
		t.Fatalf("first lock failed: %s", first.Reason)
	}
	if second.Acquire() || second.Reason != "held" {
		t.Fatalf("second lock result: acquired=%v reason=%q", second.Acquired(), second.Reason)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %o", info.Mode().Perm())
	}
	first.Release()
	if !second.Acquire() {
		t.Fatalf("lock after release failed: %s", second.Reason)
	}
	second.Release()
}

func TestSymlinkLockIsRejected(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "rpc.lock")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	instance := New(path)
	if instance.Acquire() {
		instance.Release()
		t.Fatal("symlink lock was accepted")
	}
}
