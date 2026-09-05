package pool

import (
	"errors"
	"strings"
	"testing"
)

// TestResolveInheritedSpeed is the #589 part-3 regression test: a read that
// fails while determining the speed to inherit from an already-configured
// pool device must not silently produce a default -- it must be reported
// as an error so the caller can refuse rather than write a wrong value.
//
// This is the exact production incident: an unrelated device's Script.List
// timeout during getPoolDevices() discovery caused `ctl pool add
// filtration-hiver` to silently rewrite the running pump's speed from
// "max" to the hardcoded "eco" default, with no visible sign beyond a
// cheerful "(speed: eco)" success line.
func TestResolveInheritedSpeed(t *testing.T) {
	t.Run("genuinely new device: no peer found, nothing unresolved -> default", func(t *testing.T) {
		got, err := resolveInheritedSpeed(false, "", nil, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != defaultPreferredSpeed {
			t.Errorf("got %q, want default %q", got, defaultPreferredSpeed)
		}
	})

	t.Run("peer found: inherits its speed", func(t *testing.T) {
		got, err := resolveInheritedSpeed(true, "max", nil, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "max" {
			t.Errorf("got %q, want inherited %q", got, "max")
		}
	})

	t.Run("peer found but its speed could not be read -> refuses, does not default", func(t *testing.T) {
		readErr := errors.New("timeout waiting for response from shellypro3-a0dd6ca1c588")
		got, err := resolveInheritedSpeed(true, "", readErr, 0)
		if err == nil {
			t.Fatalf("expected an error, got speed %q", got)
		}
		if got != "" {
			t.Errorf("expected empty speed on failure, got %q -- a failed read must not silently produce a default", got)
		}
		if got == defaultPreferredSpeed {
			t.Fatalf("must never return the hardcoded default on a failed read")
		}
		if !errors.Is(err, readErr) && !strings.Contains(err.Error(), readErr.Error()) {
			t.Errorf("expected error to wrap/mention the underlying read error, got %v", err)
		}
	})

	t.Run("no peer found, but some devices were unresolved -> refuses, does not assume fresh install", func(t *testing.T) {
		got, err := resolveInheritedSpeed(false, "", nil, 1)
		if err == nil {
			t.Fatalf("expected an error, got speed %q", got)
		}
		if got != "" {
			t.Errorf("expected empty speed when discovery was incomplete, got %q", got)
		}
	})
}
