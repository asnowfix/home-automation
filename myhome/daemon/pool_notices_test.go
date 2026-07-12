package daemon

import (
	"context"
	"testing"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

func TestRoundTo(t *testing.T) {
	cases := []struct {
		v      float64
		places int
		want   float64
	}{
		{3.14159, 2, 3.14},
		{3.145, 2, 3.15},
		{5, 2, 5},
		{-1.006, 2, -1.01},
		{0, 2, 0},
	}
	for _, c := range cases {
		if got := roundTo(c.v, c.places); got != c.want {
			t.Errorf("roundTo(%v, %d) = %v, want %v", c.v, c.places, got, c.want)
		}
	}
}

// TestPoolNoticesOnEventNilReceiver verifies that daemon.go can call
// poolNotices.OnEvent(...) unconditionally even when NewPoolNotices returned
// nil (pool device unreachable, or tracker/events disabled) — the broadcast
// hook has no nil check, so this must never panic.
func TestPoolNoticesOnEventNilReceiver(t *testing.T) {
	var p *PoolNotices
	p.OnEvent(context.Background(), events.Event{Event: "pool.pump_stop"})
}

// TestPoolNoticesOnEventIgnoresUnrelatedEvents verifies the event-name filter
// short-circuits before touching the (here nil) device handle, so unrelated
// events — including the device's own pool.pump_start — never attempt a KVS
// read.
func TestPoolNoticesOnEventIgnoresUnrelatedEvents(t *testing.T) {
	s := newTestEventsStorage(t)
	tracker := NewPoolRuntimeTracker(logr.Discard(), s, "pool-device")
	p := &PoolNotices{
		log:      logr.Discard(),
		events:   events.NewService(logr.Discard(), s, nil, nil, 0),
		tracker:  tracker,
		device:   nil,
		deviceID: "pool-device",
	}

	for _, evName := range []string{"pool.pump_start", "pool.run_window", "switch.on", "pool.water_supply_protected"} {
		p.OnEvent(context.Background(), events.Event{Event: evName})
	}
}
