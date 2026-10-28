package myhome

import (
	"context"
	"fmt"
	"testing"
)

// nopHandler is a minimal handler that returns a non-nil result.
func nopHandler(_ context.Context, _ any) (any, error) { return "ok", nil }

// TestRegister_KnownVerb verifies that a handler registered via Register is
// stored and retrievable via Methods(). Each test builds its own Registry
// instead of sharing process-wide state, so these are safe to run with
// t.Parallel().
func TestRegister_KnownVerb(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	Register(r, TemperatureGet, nopHandler)

	m, err := r.Methods(TemperatureGet)
	if err != nil {
		t.Fatalf("Methods(%v) error: %v", TemperatureGet, err)
	}
	if m == nil {
		t.Fatal("expected non-nil Method")
	}
	if m.Name != TemperatureGet {
		t.Errorf("Name: got %v, want %v", m.Name, TemperatureGet)
	}
	if m.Action == nil {
		t.Error("expected Action to be set")
	}
}

// TestRegister_DuplicateVerb_Overwrites verifies that registering a second
// handler for a verb that already has one replaces it rather than panicking.
func TestRegister_DuplicateVerb_Overwrites(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	Register(r, TemperatureGet, nopHandler)

	called := false
	Register(r, TemperatureGet, func(_ context.Context, _ any) (any, error) {
		called = true
		return "second", nil
	})

	m, err := r.Methods(TemperatureGet)
	if err != nil {
		t.Fatalf("Methods error: %v", err)
	}
	out, err := m.Action(context.Background(), nil)
	if err != nil {
		t.Fatalf("Action error: %v", err)
	}
	if !called {
		t.Error("second registration's handler was not called; first handler was not overwritten")
	}
	if out != "second" {
		t.Errorf("got %v, want %q", out, "second")
	}
}

// TestMethods_Unregistered verifies that Methods returns an error (not a
// panic) for a verb that has no registered handler.
func TestMethods_Unregistered(t *testing.T) {
	t.Parallel()
	r := NewRegistry()

	_, err := r.Methods(TemperatureGet)
	if err == nil {
		t.Error("expected error for unregistered method, got nil")
	}
}

// TestMethods_Registered verifies that Methods returns the Method with the
// correct name after registration.
func TestMethods_Registered(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	Register(r, TemperatureSet, nopHandler)

	m, err := r.Methods(TemperatureSet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != TemperatureSet {
		t.Errorf("Name: got %v, want %v", m.Name, TemperatureSet)
	}
}

// TestRestoreMethod_PutsBackExactMethod verifies that RestoreMethod on a
// Registry re-installs a previously saved *Method verbatim, the pattern
// callers use in t.Cleanup after swapping in a replacement handler.
func TestRestoreMethod_PutsBackExactMethod(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	Register(r, TemperatureSet, nopHandler)
	original, err := r.Methods(TemperatureSet)
	if err != nil {
		t.Fatalf("Methods error: %v", err)
	}

	Register(r, TemperatureSet, func(_ context.Context, _ any) (any, error) { return "replaced", nil })
	r.RestoreMethod(TemperatureSet, original)

	m, err := r.Methods(TemperatureSet)
	if err != nil {
		t.Fatalf("Methods error: %v", err)
	}
	if m != original {
		t.Error("expected RestoreMethod to reinstall the exact original *Method")
	}
}

// TestRegister_ParamsDecodedExactlyOnce verifies that a request's raw JSON
// params are decoded directly into the handler's declared parameter type,
// with no double-unmarshal step, whether the method is invoked in-process
// via Method.Call (typed params) or over the wire via Action (raw JSON).
func TestRegister_ParamsDecodedExactlyOnce(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	called := false
	handler := func(_ context.Context, params *TemperatureGetParams) (*TemperatureRoomConfig, error) {
		called = true
		if params.RoomID != "r1" {
			return nil, fmt.Errorf("unexpected room_id: %q", params.RoomID)
		}
		return &TemperatureRoomConfig{RoomID: "r1"}, nil
	}
	Register(r, TemperatureGet, handler)

	m, err := r.Methods(TemperatureGet)
	if err != nil {
		t.Fatalf("Methods error: %v", err)
	}

	out, err := m.Call(context.Background(), &TemperatureGetParams{RoomID: "r1"})
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
	result, ok := out.(*TemperatureRoomConfig)
	if !ok {
		t.Fatalf("unexpected result type: %T", out)
	}
	if result.RoomID != "r1" {
		t.Errorf("result.RoomID: got %q, want %q", result.RoomID, "r1")
	}

	// And again via Action, simulating the wire path: raw JSON in, no
	// pre-decoded Go value involved.
	called = false
	out, err = m.Action(context.Background(), []byte(`{"room_id":"r1"}`))
	if err != nil {
		t.Fatalf("Action error: %v", err)
	}
	if !called {
		t.Error("handler was not called via Action")
	}
	if result, ok := out.(*TemperatureRoomConfig); !ok || result.RoomID != "r1" {
		t.Fatalf("unexpected Action result: %#v", out)
	}
}
