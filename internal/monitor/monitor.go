package monitor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/v0dol4zik/Codex-CLI-RPC/internal/config"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/processes"
)

type ProcessFinder interface {
	Find(string) ([]processes.CodexProcess, error)
}

type Worker interface {
	Start()
	Stop()
}

type Runner struct {
	Settings      config.Settings
	Finder        ProcessFinder
	WorkerFactory func(config.Settings, int64) Worker
	Logger        func(string)
}

func (runner *Runner) Run(contextValue context.Context) int {
	if runner.Finder == nil {
		runner.Finder = &processes.Scanner{}
	}
	if runner.WorkerFactory == nil {
		runner.WorkerFactory = func(settings config.Settings, startedAt int64) Worker {
			return newPresenceWorker(settings, startedAt)
		}
	}
	if runner.Logger == nil {
		runner.Logger = func(message string) { fmt.Fprintln(os.Stderr, message) }
	}
	pollInterval := runner.Settings.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}

	var activeIdentity [2]uint64
	hasActive := false
	var worker Worker
	lastError := ""
	idleScans := 0
	defer func() {
		if worker != nil {
			worker.Stop()
		}
	}()

	for {
		found, err := runner.Finder.Find(runner.Settings.CodexBinary)
		if err != nil {
			message := err.Error()
			if message != lastError {
				runner.Logger("codex-rpc: cannot scan Codex processes: " + message)
				lastError = message
			}
		} else {
			lastError = ""
			selected, exists := processes.Newest(found)
			if exists {
				idleScans = 0
			} else {
				idleScans++
			}
			identity := selected.Identity()
			changed := exists != hasActive || (exists && identity != activeIdentity)
			if changed {
				if worker != nil {
					worker.Stop()
					worker = nil
				}
				if !exists {
					if hasActive {
						runner.Logger("codex-rpc: Codex exited; Presence cleared")
					}
				} else if runner.Settings.ClientID == "" {
					runner.Logger(fmt.Sprintf("codex-rpc: found Codex (PID %d), but Application ID is not configured", selected.PID))
				} else {
					worker = runner.WorkerFactory(runner.Settings, selected.StartTime)
					worker.Start()
					runner.Logger(fmt.Sprintf("codex-rpc: found Codex (PID %d); Presence enabled", selected.PID))
				}
				hasActive = exists
				activeIdentity = identity
			}
		}

		delay := pollInterval
		if err == nil && !hasActive {
			delay = idleDelay(pollInterval, idleScans)
		}
		if !wait(contextValue, delay) {
			return 0
		}
	}
}

func idleDelay(base time.Duration, scans int) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if scans <= 1 {
		return base
	}
	delay := base
	for index := 1; index < scans && delay < 5*time.Second; index++ {
		delay *= 2
	}
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}
