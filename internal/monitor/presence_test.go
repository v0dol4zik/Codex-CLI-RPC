package monitor

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/v0dol4zik/Codex-CLI-RPC/internal/config"
)

type fakeRPC struct {
	mutex        sync.Mutex
	connected    bool
	connects     int
	activities   int
	clears       int
	firstFailure bool
	success      chan struct{}
}

func (client *fakeRPC) Connect() error {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.connects++
	client.connected = true
	return nil
}

func (client *fakeRPC) Connected() bool {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.connected
}

func (client *fakeRPC) SetActivity(_ map[string]any, _ int) error {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.activities++
	if client.firstFailure {
		client.firstFailure = false
		return errors.New("simulated restart")
	}
	select {
	case <-client.success:
	default:
		close(client.success)
	}
	return nil
}

func (client *fakeRPC) ClearActivity(_ int) error {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.clears++
	return nil
}

func (client *fakeRPC) Close() error {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.connected = false
	return nil
}

func TestPresenceReconnectsAndKeepsTimestamp(t *testing.T) {
	settings := config.Settings{
		ClientID: "123", Details: "details", State: "state",
		RetryInterval: 5 * time.Millisecond, RefreshInterval: time.Hour,
	}
	client := &fakeRPC{firstFailure: true, success: make(chan struct{})}
	worker := newPresenceWorker(settings, 1234)
	worker.newClient = func() rpcClient { return client }
	worker.Start()
	select {
	case <-client.success:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not reconnect")
	}
	worker.Stop()
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.connects < 2 || client.activities < 2 || client.clears != 1 {
		t.Fatalf("unexpected calls: connects=%d activities=%d clears=%d", client.connects, client.activities, client.clears)
	}
	if start := worker.Activity()["timestamps"].(map[string]any)["start"]; start != int64(1234) {
		t.Fatalf("timestamp changed: %v", start)
	}
}

func TestLargeTextRequiresImage(t *testing.T) {
	worker := newPresenceWorker(config.Settings{LargeText: "tooltip"}, 1)
	if _, exists := worker.Activity()["assets"]; exists {
		t.Fatal("assets unexpectedly added without an image")
	}
}
