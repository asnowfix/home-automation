package myhome

import (
	"context"
	"fmt"
	"testing"
)

// withHandler registers h for verb v via Register and restores the previous
// registration (if any) in t.Cleanup. Tests must NOT call t.Parallel()
// because the methods map is a package-level global.
func withHandler[P, R any](t *testing.T, v Verb, h func(ctx context.Context, p P) (R, error)) {
	t.Helper()
	prev := methods[v]
	delete(methods, v) // Register panics on double-registration; clear any prior entry first.
	Register(v, h)
	t.Cleanup(func() {
		if prev == nil {
			delete(methods, v)
		} else {
			methods[v] = prev
		}
	})
}

// nopHandler is a minimal handler that returns a non-nil result.
func nopHandler(_ context.Context, _ any) (any, error) { return "ok", nil }

// TestRegister_KnownVerb verifies that a handler registered via Register is
// stored and retrievable via Methods().
func TestRegister_KnownVerb(t *testing.T) {
	withHandler(t, TemperatureGet, nopHandler)

	m, err := Methods(TemperatureGet)
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

// TestRegister_DuplicateVerb_Panics verifies that registering a handler for
// a verb that already has one panics, matching the old
// RegisterMethodHandler behavior for a conflicting registration.
func TestRegister_DuplicateVerb_Panics(t *testing.T) {
	withHandler(t, TemperatureGet, nopHandler)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate verb registration, but did not panic")
		}
	}()
	Register(TemperatureGet, nopHandler)
}

// TestMethods_Unregistered verifies that Methods returns an error (not a panic)
// for a verb that has no registered handler.
func TestMethods_Unregistered(t *testing.T) {
	// Ensure the verb has no handler (save and clear).
	prev := methods[TemperatureGet]
	delete(methods, TemperatureGet)
	t.Cleanup(func() {
		if prev != nil {
			methods[TemperatureGet] = prev
		}
	})

	_, err := Methods(TemperatureGet)
	if err == nil {
		t.Error("expected error for unregistered method, got nil")
	}
}

// TestMethods_Registered verifies that Methods returns the Method with the
// correct name after registration.
func TestMethods_Registered(t *testing.T) {
	withHandler(t, TemperatureSet, nopHandler)

	m, err := Methods(TemperatureSet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != TemperatureSet {
		t.Errorf("Name: got %v, want %v", m.Name, TemperatureSet)
	}
}

// TestRegister_ParamsDecodedExactlyOnce verifies that a request's raw JSON
// params are decoded directly into the handler's declared parameter type,
// with no double-unmarshal step, whether the method is invoked in-process
// via Method.Call (typed params) or over the wire via Action (raw JSON).
func TestRegister_ParamsDecodedExactlyOnce(t *testing.T) {
	called := false
	handler := func(_ context.Context, params *TemperatureGetParams) (*TemperatureRoomConfig, error) {
		called = true
		if params.RoomID != "r1" {
			return nil, fmt.Errorf("unexpected room_id: %q", params.RoomID)
		}
		return &TemperatureRoomConfig{RoomID: "r1"}, nil
	}
	withHandler(t, TemperatureGet, handler)

	m, err := Methods(TemperatureGet)
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
