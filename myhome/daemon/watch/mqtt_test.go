package watch

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"

	"github.com/go-logr/logr/testr"
)

// TestMqttWatcherStopsOnChannelClose is a regression test: a closed
// subscription channel used to make mqttWatcher spin forever on nil receives
// (logging "nil message" in a tight loop). The watcher must exit instead.
func TestMqttWatcherStopsOnChannelClose(t *testing.T) {
	ctx := context.Background()
	log := testr.New(t)

	ch := make(chan mqtt.Message)
	done := make(chan struct{})
	go func() {
		mqttWatcher(ctx, log, "+/events/rpc", nil, nil, ch)
		close(done)
	}()

	// A nil message on an open channel is skipped, not fatal
	ch <- nil
	select {
	case <-done:
		t.Fatal("watcher exited on a nil message; it should only skip it")
	case <-time.After(50 * time.Millisecond):
	}

	// Closing the channel must end the watcher (not busy-loop)
	close(ch)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop after the subscription channel closed")
	}
}

// TestMqttWatcherStopsOnContextCancel covers the pre-existing shutdown path.
func TestMqttWatcherStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	log := testr.New(t)

	ch := make(chan mqtt.Message)
	done := make(chan struct{})
	go func() {
		mqttWatcher(ctx, log, "+/events/rpc", nil, nil, ch)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop on context cancellation")
	}
}
