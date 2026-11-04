package energy

import "testing"

func TestRegistryRegisterSnapshotRoundTrip(t *testing.T) {
	r := NewRegistry()
	r.Register("pool-pump", "abc123")

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].Name != "pool-pump" || snap[0].DeviceID != "abc123" {
		t.Fatalf("unexpected claimer: %+v", snap[0])
	}
}

func TestRegistryRegisterOverwritesExistingName(t *testing.T) {
	r := NewRegistry()
	r.Register("pool-pump", "old-id")
	r.Register("pool-pump", "new-id")

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry after re-register, got %d", len(snap))
	}
	if snap[0].DeviceID != "new-id" {
		t.Fatalf("expected overwritten device_id 'new-id', got %q", snap[0].DeviceID)
	}
}

func TestRegistrySnapshotSortedByName(t *testing.T) {
	r := NewRegistry()
	r.Register("water-heater", "wh1")
	r.Register("pool-pump", "pp1")
	r.Register("ev-charger", "ev1")

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap))
	}
	if snap[0].Name != "ev-charger" || snap[1].Name != "pool-pump" || snap[2].Name != "water-heater" {
		t.Fatalf("expected sorted order, got %v", snap)
	}
}

func TestRegistrySnapshotIsACopy(t *testing.T) {
	r := NewRegistry()
	r.Register("pool-pump", "abc123")

	snap := r.Snapshot()
	snap[0].DeviceID = "mutated"

	snap2 := r.Snapshot()
	if snap2[0].DeviceID != "abc123" {
		t.Fatalf("expected registry to be unaffected by mutation of snapshot, got %q", snap2[0].DeviceID)
	}
}

func TestRegistrySnapshotEmpty(t *testing.T) {
	r := NewRegistry()
	snap := r.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("expected empty snapshot, got %v", snap)
	}
}
