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
