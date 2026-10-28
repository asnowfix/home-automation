package occupancy

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/go-logr/logr"
)

// TestOccupancyGetStatus_Dispatch verifies that RegisterHandlers wires
// myhome.OccupancyGetStatus to handleGetStatus, and that dispatching through
// the registry's Methods/Action table (as the RPC server does) returns the
// occupancy service's current status. Each test constructs its own
// myhome.Registry, so there is no shared package-level state to guard and
// this is safe to run with t.Parallel().
func TestOccupancyGetStatus_Dispatch(t *testing.T) {
	t.Parallel()
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	registry := myhome.NewRegistry()
	handler := NewRPCHandler(logr.Discard(), svc, registry)
	handler.RegisterHandlers()

	dispatched, err := registry.Methods(myhome.OccupancyGetStatus)
	if err != nil {
		t.Fatalf("Methods(OccupancyGetStatus): %v", err)
	}

	out, err := dispatched.Action(context.Background(), nil)
	if err != nil {
		t.Fatalf("Action: %v", err)
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
	t.Parallel()
	svc, mc, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()
	_ = mc

	svc.lastEvent.Store(time.Now().UnixNano())

	registry := myhome.NewRegistry()
	handler := NewRPCHandler(logr.Discard(), svc, registry)
	handler.RegisterHandlers()

	dispatched, err := registry.Methods(myhome.OccupancyGetStatus)
	if err != nil {
		t.Fatalf("Methods(OccupancyGetStatus): %v", err)
	}

	out, err := dispatched.Action(context.Background(), nil)
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	result, ok := out.(*myhome.OccupancyStatusResult)
	if !ok {
		t.Fatalf("expected *OccupancyStatusResult, got %T", out)
	}
	if !result.Occupied {
		t.Error("expected Occupied=true after a recent input event")
	}
}
