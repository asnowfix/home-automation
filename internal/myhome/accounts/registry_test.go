package accounts

import (
	"errors"
	"testing"
)

func TestRegistrySetEnabledDisabledNeverReported(t *testing.T) {
	r := NewRegistry()
	r.SetEnabled("beem", false)

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].Enabled {
		t.Fatalf("expected beem to be disabled")
	}
	if !snap[0].LastAttempt.IsZero() {
		t.Fatalf("expected no attempt recorded yet")
	}
}

func TestRegistryReportSuccessThenFailure(t *testing.T) {
	r := NewRegistry()
	r.Report("smtp", nil)

	snap := r.Snapshot()
	if len(snap) != 1 || !snap[0].Enabled || !snap[0].LastOK || snap[0].LastError != "" {
		t.Fatalf("unexpected status after success: %+v", snap)
	}

	r.Report("smtp", errors.New("dial timeout"))
	snap = r.Snapshot()
	if snap[0].LastOK {
		t.Fatalf("expected LastOK=false after failed report")
	}
	if snap[0].LastError != "dial timeout" {
		t.Fatalf("expected error message preserved, got %q", snap[0].LastError)
	}
}

func TestRegistrySnapshotSortedByName(t *testing.T) {
	r := NewRegistry()
	r.SetEnabled("sfr", true)
	r.SetEnabled("beem", true)
	r.SetEnabled("mqtt", true)

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap))
	}
	if snap[0].Name != "beem" || snap[1].Name != "mqtt" || snap[2].Name != "sfr" {
		t.Fatalf("expected sorted order, got %v", snap)
	}
}
