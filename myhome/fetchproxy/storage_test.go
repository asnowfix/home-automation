package fetchproxy

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
)

// newTestDB opens an in-memory SQLite database, matching the precedent in
// myhome/temperature/testutil_test.go and myhome/events (events.NewStorage
// takes a path, but temperature.NewStorage — the signature #465 mandates —
// takes a shared *sqlx.DB, so the in-memory instance is opened here).
func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("newTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := NewStorage(logr.Discard(), newTestDB(t))
	if err != nil {
		t.Fatalf("newTestStorage: %v", err)
	}
	return s
}

func TestStorageUpsertGetListDelete(t *testing.T) {
	s := newTestStorage(t)
	sub := sampleSubscription()

	// First write: no row yet, so it must count as a modification.
	modified, err := s.Upsert(sub)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !modified {
		t.Fatal("expected the first Upsert of a new subscription to report modified=true")
	}

	got, err := s.Get(sub.DeviceID, sub.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.URL != sub.URL || got.Topic != sub.Topic || got.Transform != sub.Transform {
		t.Fatalf("Get returned mismatched subscription: %+v", got)
	}
	if len(got.Headers) != len(sub.Headers) {
		t.Fatalf("Get returned mismatched headers: %+v vs %+v", got.Headers, sub.Headers)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 subscription in List, got %d", len(list))
	}

	found, err := s.Delete(sub.DeviceID, sub.Name)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !found {
		t.Fatal("expected Delete to report found=true for an existing subscription")
	}

	if _, err := s.Get(sub.DeviceID, sub.Name); err == nil {
		t.Fatal("expected Get to fail after Delete")
	}

	found, err = s.Delete(sub.DeviceID, sub.Name)
	if err != nil {
		t.Fatalf("Delete (already gone): %v", err)
	}
	if found {
		t.Fatal("expected Delete to report found=false for an already-deleted subscription")
	}
}

// TestStorageUpsertSkipsWriteWhenHashUnchanged is the direct test of the
// SD-card-wear optimisation in #465: re-upserting an identical subscription
// must report modified=false, meaning no SQL write happened.
func TestStorageUpsertSkipsWriteWhenHashUnchanged(t *testing.T) {
	s := newTestStorage(t)
	sub := sampleSubscription()

	if _, err := s.Upsert(sub); err != nil {
		t.Fatalf("Upsert (first): %v", err)
	}

	modified, err := s.Upsert(sub)
	if err != nil {
		t.Fatalf("Upsert (repeat): %v", err)
	}
	if modified {
		t.Fatal("expected re-upserting an unchanged subscription to report modified=false")
	}

	// Changing one field (e.g. interval) must be detected and written.
	changed := sub
	changed.IntervalSeconds = sub.IntervalSeconds + 60
	modified, err = s.Upsert(changed)
	if err != nil {
		t.Fatalf("Upsert (changed): %v", err)
	}
	if !modified {
		t.Fatal("expected upserting a subscription with a changed interval to report modified=true")
	}

	got, err := s.Get(sub.DeviceID, sub.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IntervalSeconds != changed.IntervalSeconds {
		t.Fatalf("stored interval not updated: got %d, want %d", got.IntervalSeconds, changed.IntervalSeconds)
	}
}

func TestStorageKeyedByDeviceAndName(t *testing.T) {
	s := newTestStorage(t)
	a := sampleSubscription()
	b := sampleSubscription()
	b.DeviceID = "another-device"

	if _, err := s.Upsert(a); err != nil {
		t.Fatalf("Upsert(a): %v", err)
	}
	if _, err := s.Upsert(b); err != nil {
		t.Fatalf("Upsert(b): %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 distinct subscriptions keyed by (device_id, name), got %d", len(list))
	}
}
