package daemon

import (
	"context"
	"testing"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

func TestVacuumEventRecorder_RecordEvent(t *testing.T) {
	store, err := events.NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(store.Close)
	svc := events.NewService(logr.Discard(), store, nil, nil, 0)
	rec := vacuumEventRecorder{events: svc}
	ctx := context.Background()

	if err := rec.RecordEvent(ctx, "robot", "vacuum", "vacuum.clean_requested", "info", `{"attempt":1}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	got, err := store.Query(ctx, events.Query{DeviceID: "robot"})
	if err != nil || len(got) != 1 {
		t.Fatalf("Query: err=%v len=%d", err, len(got))
	}
	e := got[0]
	if e.Component != "vacuum" || e.Event != "vacuum.clean_requested" || e.Severity != "info" {
		t.Errorf("unexpected event %+v", e)
	}
	if e.Data == nil || *e.Data != `{"attempt":1}` {
		t.Errorf("data = %v", e.Data)
	}
	if e.Ts == 0 {
		t.Error("Ts must be set")
	}
}
