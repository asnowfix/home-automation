package events

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestRecordAndQuery(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	e1 := Event{
		Ts:       1700000000.0,
		DeviceID: "dev-1",
		Component: "switch:0",
		Event:    "switch.on",
		Severity: "info",
	}
	e2 := Event{
		Ts:       1700000001.0,
		DeviceID: "dev-2",
		Component: "switch:0",
		Event:    "switch.off",
		Severity: "info",
	}

	if err := s.Record(ctx, e1); err != nil {
		t.Fatalf("Record e1: %v", err)
	}
	if err := s.Record(ctx, e2); err != nil {
		t.Fatalf("Record e2: %v", err)
	}
	// Duplicate — should be silently ignored
	if err := s.Record(ctx, e1); err != nil {
		t.Fatalf("Record duplicate: %v", err)
	}

	// Query by device
	events, err := s.Query(ctx, Query{DeviceID: "dev-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event for dev-1, got %d", len(events))
	}
	if events[0].Event != "switch.on" {
		t.Errorf("unexpected event: %s", events[0].Event)
	}

	// Query all
	all, err := s.Query(ctx, Query{})
	if err != nil {
		t.Fatalf("Query all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 total events, got %d", len(all))
	}
}

func TestOnDurationSec(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	today := time.Now().Format("2006-01-02")
	// Use a base timestamp that falls on today's date.
	base := float64(time.Now().Truncate(24*time.Hour).Unix()) + 3600 // 01:00 today

	record := func(deviceID, component, event string, ts float64) {
		t.Helper()
		if err := s.Record(ctx, Event{Ts: ts, DeviceID: deviceID, Component: component, Event: event, Severity: "info"}); err != nil {
			t.Fatalf("Record %s: %v", event, err)
		}
	}

	dur := func(deviceID, component string) int64 {
		t.Helper()
		d, err := s.OnDurationSec(ctx, deviceID, component, "switch.on", "switch.off", today)
		if err != nil {
			t.Fatalf("OnDurationSec: %v", err)
		}
		return d
	}

	// No events yet → 0.
	if got := dur("pump", "switch:0"); got != 0 {
		t.Errorf("empty: want 0, got %d", got)
	}

	// Single completed ON→OFF pair (100 s).
	record("pump", "switch:0", "switch.on", base)
	record("pump", "switch:0", "switch.off", base+100)
	if got := dur("pump", "switch:0"); got != 100 {
		t.Errorf("single pair: want 100, got %d", got)
	}

	// Second pair (200 s).
	record("pump", "switch:0", "switch.on", base+200)
	record("pump", "switch:0", "switch.off", base+400)
	if got := dur("pump", "switch:0"); got != 300 {
		t.Errorf("two pairs: want 300, got %d", got)
	}

	// Consecutive ON events (duplicate reconnect): only first counted.
	record("pump", "switch:0", "switch.on", base+500)
	record("pump", "switch:0", "switch.on", base+510) // duplicate
	record("pump", "switch:0", "switch.off", base+600)
	// 100 s run from base+500, duplicate ON at base+510 ignored.
	if got := dur("pump", "switch:0"); got != 400 {
		t.Errorf("consecutive ONs: want 400, got %d", got)
	}

	// Different device is not counted.
	record("heater", "switch:0", "switch.on", base)
	record("heater", "switch:0", "switch.off", base+9999)
	if got := dur("pump", "switch:0"); got != 400 {
		t.Errorf("cross-device: want 400, got %d", got)
	}

	// Pump currently ON (no OFF yet): open interval counted toward now.
	record("pump2", "switch:0", "switch.on", base)
	got := dur("pump2", "switch:0")
	elapsed := int64(time.Now().Unix()) - int64(base)
	if got < elapsed-2 || got > elapsed+2 {
		t.Errorf("open interval: want ~%d, got %d", elapsed, got)
	}
}

func TestOnDurationSec_WrongDay(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")
	baseYesterday := float64(time.Now().Add(-24*time.Hour).Truncate(24*time.Hour).Unix()) + 3600

	if err := s.Record(ctx, Event{Ts: baseYesterday, DeviceID: "pump", Component: "switch:0", Event: "switch.on", Severity: "info"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, Event{Ts: baseYesterday + 100, DeviceID: "pump", Component: "switch:0", Event: "switch.off", Severity: "info"}); err != nil {
		t.Fatal(err)
	}

	// Querying yesterday returns 100.
	if got, _ := s.OnDurationSec(ctx, "pump", "switch:0", "switch.on", "switch.off", yesterday); got != 100 {
		t.Errorf("yesterday: want 100, got %d", got)
	}
	// Querying today returns 0.
	if got, _ := s.OnDurationSec(ctx, "pump", "switch:0", "switch.on", "switch.off", today); got != 0 {
		t.Errorf("today (no events): want 0, got %d", got)
	}
}

func TestPurge(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	old := Event{
		Ts:        float64(time.Now().Add(-48 * time.Hour).Unix()),
		DeviceID:  "dev-old",
		Component: "switch:0",
		Event:     "switch.on",
		Severity:  "info",
	}
	fresh := Event{
		Ts:        float64(time.Now().Unix()),
		DeviceID:  "dev-fresh",
		Component: "switch:0",
		Event:     "switch.on",
		Severity:  "info",
	}

	if err := s.Record(ctx, old); err != nil {
		t.Fatalf("Record old: %v", err)
	}
	if err := s.Record(ctx, fresh); err != nil {
		t.Fatalf("Record fresh: %v", err)
	}

	n, err := s.Purge(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 purged row, got %d", n)
	}

	remaining, err := s.Query(ctx, Query{})
	if err != nil {
		t.Fatalf("Query after purge: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining event, got %d", len(remaining))
	}
	if remaining[0].DeviceID != "dev-fresh" {
		t.Errorf("wrong remaining event: %s", remaining[0].DeviceID)
	}
}
