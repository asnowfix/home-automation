package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

func newTestEventsStorage(t *testing.T) *events.Storage {
	t.Helper()
	s, err := events.NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("events.NewStorage: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func insertSwitchEvent(t *testing.T, s *events.Storage, deviceID, event string, ts float64) {
	t.Helper()
	if err := s.Record(context.Background(), events.Event{
		Ts:        ts,
		DeviceID:  deviceID,
		Component: "switch:0",
		Event:     event,
		Severity:  "info",
	}); err != nil {
		t.Fatalf("Record %s: %v", event, err)
	}
}

func TestPoolTracker_DailyRuntimeSec(t *testing.T) {
	s := newTestEventsStorage(t)
	tracker := NewPoolRuntimeTracker(logr.Discard(), s, "pool-device")

	base := float64(time.Now().Truncate(24*time.Hour).Unix()) + 3600

	insertSwitchEvent(t, s, "pool-device", "switch.on", base)
	insertSwitchEvent(t, s, "pool-device", "switch.off", base+300) // 5 min
	insertSwitchEvent(t, s, "pool-device", "switch.on", base+600)
	insertSwitchEvent(t, s, "pool-device", "switch.off", base+900) // 5 min

	got, err := tracker.DailyRuntimeSec(context.Background())
	if err != nil {
		t.Fatalf("DailyRuntimeSec: %v", err)
	}
	if got != 600 {
		t.Errorf("want 600, got %d", got)
	}
}

func TestPoolTracker_RemainingRuntimeSec(t *testing.T) {
	s := newTestEventsStorage(t)
	tracker := NewPoolRuntimeTracker(logr.Discard(), s, "pool-device")

	base := float64(time.Now().Truncate(24*time.Hour).Unix()) + 3600
	insertSwitchEvent(t, s, "pool-device", "switch.on", base)
	insertSwitchEvent(t, s, "pool-device", "switch.off", base+3600) // 1 h

	remaining, err := tracker.RemainingRuntimeSec(context.Background(), 7200)
	if err != nil {
		t.Fatalf("RemainingRuntimeSec: %v", err)
	}
	if remaining != 3600 {
		t.Errorf("want 3600, got %d", remaining)
	}

	// Target already met.
	done, err := tracker.RemainingRuntimeSec(context.Background(), 1800)
	if err != nil {
		t.Fatalf("RemainingRuntimeSec (met): %v", err)
	}
	if done != 0 {
		t.Errorf("want 0 when target met, got %d", done)
	}
}
