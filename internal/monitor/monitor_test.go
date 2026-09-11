package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/v0dol4zik/Codex-CLI-RPC/internal/config"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/processes"
)

type finderResult struct {
	processes []processes.CodexProcess
	err       error
}

type fakeFinder struct {
	results []finderResult
	cancel  context.CancelFunc
}

func (finder *fakeFinder) Find(string) ([]processes.CodexProcess, error) {
	if len(finder.results) == 0 {
		finder.cancel()
		return nil, nil
	}
	result := finder.results[0]
	finder.results = finder.results[1:]
	if len(finder.results) == 0 {
		defer finder.cancel()
	}
	return result.processes, result.err
}

type fakeWorker struct {
	startedAt int64
	started   bool
	stopped   bool
}

func (worker *fakeWorker) Start() { worker.started = true }
func (worker *fakeWorker) Stop()  { worker.stopped = true }

func TestRunnerAttachesToExistingProcessAndClearsOnExit(t *testing.T) {
	contextValue, cancel := context.WithCancel(context.Background())
	process := processes.CodexProcess{PID: 42, StartTime: 1234, StartTicks: 5678, Executable: filepath.Join(t.TempDir(), "codex")}
	finder := &fakeFinder{results: []finderResult{
		{processes: []processes.CodexProcess{process}},
		{err: errors.New("temporary scan error")},
		{processes: []processes.CodexProcess{process}},
		{},
	}, cancel: cancel}
	workers := make([]*fakeWorker, 0, 1)
	logs := make([]string, 0)
	runner := Runner{
		Settings: config.Settings{ClientID: "123", CodexBinary: process.Executable, PollInterval: time.Millisecond},
		Finder:   finder,
		WorkerFactory: func(_ config.Settings, startedAt int64) Worker {
			worker := &fakeWorker{startedAt: startedAt}
			workers = append(workers, worker)
			return worker
		},
		Logger: func(message string) { logs = append(logs, message) },
	}
	if code := runner.Run(contextValue); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if len(workers) != 1 || workers[0].startedAt != 1234 || !workers[0].started || !workers[0].stopped {
		t.Fatalf("unexpected workers: %+v", workers)
	}
	if len(logs) != 3 {
		t.Fatalf("unexpected logs: %#v", logs)
	}
}

func TestIdleDelayBacksOffAndCapsAtFiveSeconds(t *testing.T) {
	base := 250 * time.Millisecond
	wants := []time.Duration{base, 2 * base, 4 * base, 2 * time.Second, 4 * time.Second, 5 * time.Second}
	for index, want := range wants {
		if got := idleDelay(base, index+1); got != want {
			t.Fatalf("scan %d: delay=%s, want %s", index+1, got, want)
		}
	}
}
