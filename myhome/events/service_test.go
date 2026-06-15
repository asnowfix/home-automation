package events

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
)

func TestService_RecordDefaultsTsFromReceivedAt(t *testing.T) {
	store := newTestStorage(t)
	svc := NewService(logr.Discard(), store, nil, nil, 0)
	ctx := context.Background()

	if err := svc.Record(ctx, Event{
		DeviceID:  "dev-1",
		Component: "motion:0",
		Event:     "motion.detected",
		Severity:  "info",
		// Ts deliberately zero
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	evts, err := store.Query(ctx, Query{DeviceID: "dev-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Ts == 0 {
		t.Error("Ts must not be zero after Record; expected it to be defaulted to ReceivedAt")
	}
	if evts[0].Ts != evts[0].ReceivedAt {
		t.Errorf("Ts (%v) should equal ReceivedAt (%v) when Ts was not set by caller", evts[0].Ts, evts[0].ReceivedAt)
	}
}
