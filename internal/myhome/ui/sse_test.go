package ui

import (
	"testing"

	"github.com/go-logr/logr"
)

func newTestBroadcaster(t *testing.T) *SSEBroadcaster {
	t.Helper()
	return NewSSEBroadcaster(logr.Discard())
}

// TestSSE_FastClientReceivesMessages verifies normal clients receive every broadcast.
func TestSSE_FastClientReceivesMessages(t *testing.T) {
	b := newTestBroadcaster(t)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.BroadcastSensorUpdate("dev1", "temperature", "22.5")

	select {
	case msg := <-ch:
		if msg == "" {
			t.Error("received empty message")
		}
	default:
		t.Error("expected message in channel, got none")
	}
}

// TestSSE_SlowClientEvictedAfterThreshold verifies that a client with a full
// channel is evicted after sseSlowClientMaxSkips consecutive skips.
func TestSSE_SlowClientEvictedAfterThreshold(t *testing.T) {
	b := newTestBroadcaster(t)

	// slow client: buffer=0 so every broadcast fills immediately
	slowCh := make(chan string, 0)
	b.mu.Lock()
	b.clients[slowCh] = &sseClient{ch: slowCh}
	b.mu.Unlock()

	// fast client that drains the channel
	fastCh := b.Subscribe()
	defer b.Unsubscribe(fastCh)

	// Broadcast sseSlowClientMaxSkips times — slow channel full every time
	for i := 0; i < sseSlowClientMaxSkips; i++ {
		b.BroadcastSensorUpdate("dev1", "temperature", "22.5")
		// drain fast client so it stays healthy
		<-fastCh
	}

	// After exactly sseSlowClientMaxSkips skips the slow client should be evicted
	b.mu.Lock()
	_, slowStillPresent := b.clients[slowCh]
	b.mu.Unlock()

	if slowStillPresent {
		t.Error("slow client should have been evicted but is still in the map")
	}

	// The closed channel should now return the zero value immediately
	select {
	case _, open := <-slowCh:
		if open {
			t.Error("evicted channel should be closed")
		}
	default:
		t.Error("evicted channel should be closed (readable), not blocking")
	}
}

// TestSSE_SlowClientDoesNotAffectFastClients verifies that evicting a slow client
// does not interrupt delivery to fast clients.
func TestSSE_SlowClientDoesNotAffectFastClients(t *testing.T) {
	b := newTestBroadcaster(t)

	fastCh := b.Subscribe()
	defer b.Unsubscribe(fastCh)

	// slow client
	slowCh := make(chan string, 0)
	b.mu.Lock()
	b.clients[slowCh] = &sseClient{ch: slowCh}
	b.mu.Unlock()

	const broadcasts = sseSlowClientMaxSkips + 5
	for i := 0; i < broadcasts; i++ {
		b.BroadcastSensorUpdate("dev1", "temperature", "22.5")
		// fast client drains promptly
		select {
		case msg := <-fastCh:
			if msg == "" {
				t.Errorf("broadcast %d: empty message", i)
			}
		default:
			t.Errorf("broadcast %d: fast client did not receive message", i)
		}
	}
}

// TestSSE_UnsubscribeAfterEvictionIsSafe verifies that calling Unsubscribe on a
// channel that was already closed by the broadcaster does not panic.
func TestSSE_UnsubscribeAfterEvictionIsSafe(t *testing.T) {
	b := newTestBroadcaster(t)

	slowCh := make(chan string, 0)
	b.mu.Lock()
	b.clients[slowCh] = &sseClient{ch: slowCh}
	b.mu.Unlock()

	for i := 0; i < sseSlowClientMaxSkips; i++ {
		b.BroadcastSensorUpdate("dev1", "temperature", "22.5")
	}

	// Must not panic
	b.Unsubscribe(slowCh)
}
