package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/v0dol4zik/Codex-CLI-RPC/internal/config"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/discord"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/lock"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/monitor"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/processes"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/service"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/version"
)

const temporaryFailure = 75

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (app App) Run(arguments []string) int {
	if app.Stdout == nil {
		app.Stdout = os.Stdout
	}
	if app.Stderr == nil {
		app.Stderr = os.Stderr
	}
	if len(arguments) == 1 && arguments[0] == "--rpc-version" {
		fmt.Fprintln(app.Stdout, version.Current)
		return 0
	}
	if len(arguments) > 0 && arguments[0] == "service" {
		return app.serviceCommand(arguments[1:])
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(app.Stderr, "codex-rpc: %v\n", err)
		if errors.Is(err, config.ErrCodexExecutable) {
			return 127
		}
		return 2
	}
	if equalArguments(arguments, "--check") || equalArguments(arguments, "--rpc-check") {
		return app.check(settings)
	}
	if equalArguments(arguments, "--monitor") || equalArguments(arguments, "--watch") || equalArguments(arguments, "--rpc-monitor") {
		return app.runMonitor(settings)
	}
	return app.runCodex(settings, arguments)
}

func (app App) runMonitor(settings config.Settings) int {
	instance := lock.New(settings.LockFile)
	contextValue, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	reportedCollision := false
	for !instance.Acquire() {
		if instance.Reason != "held" {
			fmt.Fprintf(app.Stderr, "codex-rpc: cannot acquire lock %s: %s\n", instance.Path, instance.Reason)
			return temporaryFailure
		}
		if !reportedCollision {
			fmt.Fprintf(app.Stderr, "codex-rpc: another RPC client owns %s; waiting\n", instance.Path)
			reportedCollision = true
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-contextValue.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
	defer instance.Release()
	runner := monitor.Runner{Settings: settings, Logger: func(message string) { fmt.Fprintln(app.Stderr, message) }}
	return runner.Run(contextValue)
}

func (app App) runCodex(settings config.Settings, arguments []string) int {
	instance := lock.New(settings.LockFile)
	ownsLock := instance.Acquire()
	var worker monitor.Worker
	if ownsLock {
		defer instance.Release()
		if settings.ClientID != "" {
			worker = monitor.NewPresenceWorker(settings, time.Now().Unix())
			worker.Start()
			defer worker.Stop()
		}
	} else if settings.ClientID != "" {
		if instance.Reason == "held" {
			fmt.Fprintln(app.Stderr, "codex-rpc: monitor already owns Presence; starting Codex without a second RPC client")
		} else {
			fmt.Fprintf(app.Stderr, "codex-rpc: Presence disabled because lock %s is unavailable: %s\n", instance.Path, instance.Reason)
		}
	}

	command := exec.Command(settings.CodexBinary, arguments...)
	command.Stdin = os.Stdin
	command.Stdout = app.Stdout
	command.Stderr = app.Stderr
	if err := command.Start(); err != nil {
		fmt.Fprintf(app.Stderr, "codex-rpc: could not start Codex: %v\n", err)
		return 127
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for received := range signals {
			switch received {
			case syscall.SIGTERM, syscall.SIGHUP:
				_ = command.Process.Signal(received)
			case os.Interrupt, syscall.SIGQUIT:
				// A terminal sends these to the entire foreground process group,
				// including Codex. Forwarding would deliver Ctrl+C twice.
			}
		}
	}()
	err := command.Wait()
	signal.Stop(signals)
	close(signals)
	<-done
	return exitCode(err)
}

func (app App) check(settings config.Settings) int {
	fmt.Fprintf(app.Stdout, "codex-discord-rpc %s\n", version.Current)
	fmt.Fprintf(app.Stdout, "Codex: %s\n", settings.CodexBinary)
	found, err := (&processes.Scanner{}).Find(settings.CodexBinary)
	if err != nil {
		fmt.Fprintf(app.Stdout, "Codex process scan: failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(app.Stdout, "Codex processes: %d\n", len(found))
	path, err := discord.FindSocket(settings.RuntimeDir)
	if err != nil {
		fmt.Fprintf(app.Stdout, "Discord IPC: failed: %v\n", err)
		return 1
	}
	if path == "" {
		fmt.Fprintln(app.Stdout, "Discord IPC: not found")
	} else {
		fmt.Fprintf(app.Stdout, "Discord IPC: %s\n", path)
	}
	if settings.ClientID == "" {
		fmt.Fprintln(app.Stdout, "Discord client ID: not configured")
		return 1
	}
	fmt.Fprintln(app.Stdout, "Discord client ID: configured")
	if path == "" {
		return 1
	}
	client := discord.NewClient(settings.ClientID, settings.RuntimeDir, time.Second)
	defer client.Close()
	if err := client.Connect(); err != nil {
		fmt.Fprintf(app.Stdout, "Discord handshake: failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(app.Stdout, "Discord handshake: successful")
	return 0
}

func (app App) serviceCommand(arguments []string) int {
	if len(arguments) != 1 {
		app.serviceUsage()
		return 2
	}
	manager, err := service.DefaultManager()
	if err != nil {
		fmt.Fprintf(app.Stderr, "codex-rpc service: %v\n", err)
		return 1
	}
	action := arguments[0]
	switch action {
	case "install":
		path, err := manager.Install()
		if err != nil {
			fmt.Fprintf(app.Stderr, "codex-rpc service: %v\n", err)
			return 1
		}
		fmt.Fprintf(app.Stdout, "User service installed: %s\nIt was not enabled or started. Use explicit service enable and service start commands.\n", path)
		return 0
	case "uninstall":
		path, err := manager.Uninstall()
		if err != nil {
			fmt.Fprintf(app.Stderr, "codex-rpc service: %v\n", err)
			return 1
		}
		fmt.Fprintf(app.Stdout, "User service stopped and removed: %s\n", path)
		return 0
	case "enable", "disable", "start", "stop", "restart", "status":
		result, err := manager.Action(action)
		if result.Stdout != "" {
			fmt.Fprint(app.Stdout, result.Stdout)
		}
		if result.Stderr != "" {
			fmt.Fprint(app.Stderr, result.Stderr)
		}
		if err != nil {
			fmt.Fprintf(app.Stderr, "codex-rpc service: %v\n", err)
			return 1
		}
		return result.ExitCode
	default:
		app.serviceUsage()
		return 2
	}
}

func (app App) serviceUsage() {
	fmt.Fprintln(app.Stderr, "Usage: codex-rpc service {install|uninstall|enable|disable|status|start|stop|restart}")
}

func equalArguments(arguments []string, expected string) bool {
	return len(arguments) == 1 && arguments[0] == expected
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return 127
	}
	if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			return 128 + int(status.Signal())
		}
		return status.ExitStatus()
	}
	return exitError.ExitCode()
}
