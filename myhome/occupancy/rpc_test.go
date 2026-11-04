package occupancy

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/go-logr/logr"
)

// withRegisteredHandler registers h for verb on the shared myhome method
// registry and restores the previous registration (if any) in t.Cleanup.
// myhome exposes no Unregister API, so when there was no prior registration
// this intentionally leaves h registered afterward. Tests using this must
// not call t.Parallel(), since the registry is a package-level global.
func withRegisteredHandler(t *testing.T, verb myhome.Verb, h myhome.MethodHandler) {
	t.Helper()
	prev, err := myhome.Methods(verb)
	myhome.RegisterMethodHandler(verb, h)
	t.Cleanup(func() {
		if err == nil && prev != nil {
			myhome.RegisterMethodHandler(verb, prev.ActionE)
		}
	})
}

// TestOccupancyGetStatus_Dispatch verifies that RegisterHandlers wires
// myhome.OccupancyGetStatus to handleGetStatus, and that dispatching through
// the shared myhome.Methods/ActionE table (as the RPC server does) returns
// the occupancy service's current status.
func TestOccupancyGetStatus_Dispatch(t *testing.T) {
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	handler := NewRPCHandler(logr.Discard(), svc)

	// RegisterHandlers calls myhome.RegisterMethodHandler directly (a package
	// global), so route it through withRegisteredHandler for cleanup instead
	// of calling it directly.
	withRegisteredHandler(t, myhome.OccupancyGetStatus, handler.handleGetStatus)

	dispatched, err := myhome.Methods(myhome.OccupancyGetStatus)
	if err != nil {
		t.Fatalf("Methods(OccupancyGetStatus): %v", err)
	}

	out, err := dispatched.ActionE(context.Background(), nil)
	if err != nil {
		t.Fatalf("ActionE: %v", err)
	}
	result, ok := out.(*myhome.OccupancyStatusResult)
	if !ok {
		t.Fatalf("expected *OccupancyStatusResult, got %T", out)
	}
	if result.Occupied {
		t.Error("expected Occupied=false for a freshly created service with no events")
	}
}

// TestOccupancyGetStatus_Dispatch_Occupied verifies the handler reflects a
// recent input event through the same dispatch path.
func TestOccupancyGetStatus_Dispatch_Occupied(t *testing.T) {
	svc, mc, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()
	_ = mc

	svc.lastEvent.Store(time.Now().UnixNano())

	handler := NewRPCHandler(logr.Discard(), svc)
	withRegisteredHandler(t, myhome.OccupancyGetStatus, handler.handleGetStatus)

	dispatched, err := myhome.Methods(myhome.OccupancyGetStatus)
	if err != nil {
		t.Fatalf("Methods(OccupancyGetStatus): %v", err)
	}

	out, err := dispatched.ActionE(context.Background(), nil)
	if err != nil {
		t.Fatalf("ActionE: %v", err)
	}
	result, ok := out.(*myhome.OccupancyStatusResult)
	if !ok {
		t.Fatalf("expected *OccupancyStatusResult, got %T", out)
	}
	if !result.Occupied {
		t.Error("expected Occupied=true after a recent input event")
	}
}
