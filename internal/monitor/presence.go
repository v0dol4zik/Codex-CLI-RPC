package monitor

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/v0dol4zik/Codex-CLI-RPC/internal/config"
	"github.com/v0dol4zik/Codex-CLI-RPC/internal/discord"
)

type rpcClient interface {
	Connect() error
	Connected() bool
	SetActivity(map[string]any, int) error
	ClearActivity(int) error
	Close() error
}

type presenceWorker struct {
	settings  config.Settings
	startedAt int64
	newClient func() rpcClient
	cancel    context.CancelFunc
	done      chan struct{}
	mutex     sync.Mutex
}

func newPresenceWorker(settings config.Settings, startedAt int64) *presenceWorker {
	return &presenceWorker{
		settings:  settings,
		startedAt: startedAt,
		newClient: func() rpcClient {
			return discord.NewClient(settings.ClientID, settings.RuntimeDir, time.Second)
		},
	}
}

func NewPresenceWorker(settings config.Settings, startedAt int64) Worker {
	return newPresenceWorker(settings, startedAt)
}

func (worker *presenceWorker) Start() {
	worker.mutex.Lock()
	defer worker.mutex.Unlock()
	if worker.cancel != nil {
		return
	}
	contextValue, cancel := context.WithCancel(context.Background())
	worker.cancel = cancel
	worker.done = make(chan struct{})
	go worker.run(contextValue, worker.done)
}

func (worker *presenceWorker) Stop() {
	worker.mutex.Lock()
	cancel := worker.cancel
	done := worker.done
	worker.cancel = nil
	worker.done = nil
	worker.mutex.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (worker *presenceWorker) Activity() map[string]any {
	activity := map[string]any{
		"details":    worker.settings.Details,
		"state":      worker.settings.State,
		"timestamps": map[string]any{"start": worker.startedAt},
	}
	if worker.settings.LargeImage != "" {
		assets := map[string]any{"large_image": worker.settings.LargeImage}
		if worker.settings.LargeText != "" {
			assets["large_text"] = worker.settings.LargeText
		}
		activity["assets"] = assets
	}
	return activity
}

func (worker *presenceWorker) run(contextValue context.Context, done chan<- struct{}) {
	defer close(done)
	client := worker.newClient()
	defer client.Close()
	nextRefresh := time.Time{}
	for {
		if contextValue.Err() != nil {
			if client.Connected() {
				_ = client.ClearActivity(os.Getpid())
			}
			return
		}
		if !client.Connected() {
			if err := client.Connect(); err != nil {
				if !wait(contextValue, worker.settings.RetryInterval) {
					return
				}
				continue
			}
			nextRefresh = time.Time{}
		}
		if nextRefresh.IsZero() || !time.Now().Before(nextRefresh) {
			if err := client.SetActivity(worker.Activity(), os.Getpid()); err != nil {
				_ = client.Close()
				if !wait(contextValue, worker.settings.RetryInterval) {
					return
				}
				continue
			}
			nextRefresh = time.Now().Add(worker.settings.RefreshInterval)
		}
		delay := worker.settings.RetryInterval
		if untilRefresh := time.Until(nextRefresh); untilRefresh > 0 && untilRefresh < delay {
			delay = untilRefresh
		}
		if !wait(contextValue, delay) {
			if client.Connected() {
				_ = client.ClearActivity(os.Getpid())
			}
			return
		}
	}
}

func wait(contextValue context.Context, duration time.Duration) bool {
	if duration <= 0 {
		duration = 250 * time.Millisecond
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-contextValue.Done():
		return false
	case <-timer.C:
		return true
	}
}
