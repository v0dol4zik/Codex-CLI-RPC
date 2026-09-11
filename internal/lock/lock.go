package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

type Instance struct {
	Path   string
	Reason string
	file   *os.File
}

func New(path string) *Instance {
	if path == "" {
		path = defaultPath()
	}
	return &Instance{Path: path}
}

func (instance *Instance) Acquired() bool { return instance.file != nil }

func (instance *Instance) Acquire() bool {
	if instance.file != nil {
		return true
	}
	instance.Reason = ""
	if err := os.MkdirAll(filepath.Dir(instance.Path), 0o700); err != nil {
		instance.Reason = err.Error()
		return false
	}
	descriptor, err := syscall.Open(instance.Path,
		syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		instance.Reason = err.Error()
		return false
	}
	if err := syscall.Fchmod(descriptor, 0o600); err != nil {
		_ = syscall.Close(descriptor)
		instance.Reason = err.Error()
		return false
	}
	if err := syscall.Flock(descriptor, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = syscall.Close(descriptor)
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			instance.Reason = "held"
		} else {
			instance.Reason = err.Error()
		}
		return false
	}
	instance.file = os.NewFile(uintptr(descriptor), instance.Path)
	if instance.file == nil {
		_ = syscall.Flock(descriptor, syscall.LOCK_UN)
		_ = syscall.Close(descriptor)
		instance.Reason = "could not create lock file handle"
		return false
	}
	return true
}

func (instance *Instance) Release() {
	file := instance.file
	instance.file = nil
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func defaultPath() string {
	runtime := "/run/user/" + strconv.Itoa(os.Getuid())
	if info, err := os.Stat(runtime); err == nil && info.IsDir() {
		return filepath.Join(runtime, "codex-discord-rpc.lock")
	}
	if runtime = os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		return filepath.Join(runtime, "codex-discord-rpc.lock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("codex-discord-rpc-%d.lock", os.Getuid()))
}
