package events

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
)

func TestStorage_RecordID_DuplicateReturnsZero(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	e := Event{Ts: 1700000000, DeviceID: "d", Component: "c", Event: "ev", Severity: "info"}

	id, err := s.RecordID(ctx, e)
	if err != nil || id == 0 {
		t.Fatalf("first insert: id=%d err=%v, want non-zero id", id, err)
	}
	id2, err := s.RecordID(ctx, e)
	if err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}
	if id2 != 0 {
		t.Errorf("duplicate insert returned id %d, want 0 (ignored)", id2)
	}
}

func TestStorage_UpdateData(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	first := `{"a":1}`
	id, err := s.RecordID(ctx, Event{Ts: 1, DeviceID: "d", Component: "c", Event: "ev", Severity: "info", Data: &first})
	if err != nil || id == 0 {
		t.Fatalf("RecordID: id=%d err=%v", id, err)
	}
	if err := s.UpdateData(ctx, id, `{"a":1,"b":2}`); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}
	got, err := s.Query(ctx, Query{DeviceID: "d"})
	if err != nil || len(got) != 1 {
		t.Fatalf("Query: %v len=%d", err, len(got))
	}
	if got[0].Data == nil || *got[0].Data != `{"a":1,"b":2}` {
		t.Errorf("data = %v, want updated JSON", got[0].Data)
	}
	// Updating a missing row is not an error.
	if err := s.UpdateData(ctx, 9999, "x"); err != nil {
		t.Errorf("UpdateData on missing id: %v", err)
	}
}

func TestService_RecordIDAndUpdateData(t *testing.T) {
	store := newTestStorage(t)
	var broadcasts int
	svc := NewService(logr.Discard(), store, nil, func(Event) { broadcasts++ }, 0)
	ctx := context.Background()
	e := Event{DeviceID: "d", Component: "c", Event: "ev", Severity: "info", Ts: 5}

	id, err := svc.RecordID(ctx, e)
	if err != nil || id == 0 {
		t.Fatalf("RecordID: id=%d err=%v", id, err)
	}
	if err := svc.UpdateData(ctx, id, "merged"); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}
	if broadcasts != 1 {
		t.Errorf("broadcasts = %d, want 1 (amendments are not broadcast)", broadcasts)
	}
	got, _ := store.Query(ctx, Query{DeviceID: "d"})
	if len(got) != 1 || got[0].Data == nil || *got[0].Data != "merged" {
		t.Errorf("unexpected rows: %+v", got)
	}
}
