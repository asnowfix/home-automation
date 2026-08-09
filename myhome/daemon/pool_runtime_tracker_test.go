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

	base := localMidnightPlusHour(t)

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

	base := localMidnightPlusHour(t)
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

// localMidnightPlusHour returns 01:00 local time today, as a Unix timestamp.
//
// Not time.Now().Truncate(24*time.Hour): Truncate works on absolute time since
// the zero instant, so it aligns to UTC midnight, not local midnight. In a
// UTC+2 zone that is 02:00 local — which means that between local midnight and
// 02:00, "today" computed this way is still yesterday, the fixture events land
// on the previous local day, and every test that counts "today's" runtime sees
// zero. Deterministically broken for two hours every night rather than flaky.
func localMidnightPlusHour(t *testing.T) float64 {
	t.Helper()
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return float64(midnight.Add(time.Hour).Unix())
}
