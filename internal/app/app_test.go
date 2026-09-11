package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/v0dol4zik/Codex-CLI-RPC/internal/config"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/version"
)

func TestRPCVersionDoesNotRequireConfiguration(t *testing.T) {
	t.Setenv("CODEX_RPC_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("CODEX_BINARY", filepath.Join(t.TempDir(), "missing-codex"))
	var stdout, stderr bytes.Buffer
	code := (App{Stdout: &stdout, Stderr: &stderr}).Run([]string{"--rpc-version"})
	if code != 0 || strings.TrimSpace(stdout.String()) != version.Current || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunCodexPreservesArgumentsAndExitCode(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "arguments")
	binary := filepath.Join(directory, "codex")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CODEX_TEST_OUTPUT\"\nexit 7\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_TEST_OUTPUT", output)
	var stdout, stderr bytes.Buffer
	settings := config.Settings{CodexBinary: binary, LockFile: filepath.Join(directory, "rpc.lock")}
	code := (App{Stdout: &stdout, Stderr: &stderr}).runCodex(settings, []string{"--one", "два слова"})
	if code != 7 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "--one\nдва слова\n" {
		t.Fatalf("arguments = %q", data)
	}
}

func TestInvalidServiceActionDoesNotInvokeSystemctl(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := (App{Stdout: &stdout, Stderr: &stderr}).Run([]string{"service", "invalid"})
	if code != 2 || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestTerminalInterruptReachesCodexExactlyOnce(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join("..", "..")
	wrapper := filepath.Join(directory, "codex-rpc")
	build := exec.Command("go", "build", "-o", wrapper, "./cmd/codex-rpc")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build wrapper: %v\n%s", err, output)
	}

	helperSource := `package main
import (
	"os"
	"os/signal"
	"syscall"
	"time"
)
func main() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)
	if err := os.WriteFile(os.Getenv("READY_FILE"), []byte("ready"), 0o600); err != nil { os.Exit(20) }
	<-signals
	value := []byte("I")
	select {
	case <-signals:
		value = append(value, 'I')
	case <-time.After(300 * time.Millisecond):
	}
	if err := os.WriteFile(os.Getenv("SIGNAL_FILE"), value, 0o600); err != nil { os.Exit(21) }
	syscall.Exit(9)
}`
	helperSourcePath := filepath.Join(directory, "helper.go")
	if err := os.WriteFile(helperSourcePath, []byte(helperSource), 0o600); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(directory, "fake-codex")
	build = exec.Command("go", "build", "-o", helper, helperSourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}

	ready := filepath.Join(directory, "ready")
	received := filepath.Join(directory, "signals")
	command := exec.Command(wrapper)
	command.Env = append(os.Environ(),
		"CODEX_BINARY="+helper,
		"CODEX_RPC_CONFIG="+filepath.Join(directory, "missing.toml"),
		"CODEX_RPC_LOCK_FILE="+filepath.Join(directory, "rpc.lock"),
		"CODEX_DISCORD_CLIENT_ID=",
		"DISCORD_CLIENT_ID=",
		"READY_FILE="+ready,
		"SIGNAL_FILE="+received,
	)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			_, _ = command.Process.Wait()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatal("fake Codex did not become ready")
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case err := <-wait:
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 9 {
			t.Fatalf("wrapper result = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wrapper did not exit after interrupt")
	}
	data, err := os.ReadFile(received)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "I" {
		t.Fatalf("Codex received interrupt %d times", len(data))
	}
}
