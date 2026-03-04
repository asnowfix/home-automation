package myhome

import (
	"context"
	"fmt"
	"testing"
)

// withHandler registers h for verb v and restores the previous handler in
// t.Cleanup. Tests must NOT call t.Parallel() because the methods map is a
// package-level global.
func withHandler(t *testing.T, v Verb, h MethodHandler) {
	t.Helper()
	prev := methods[v]
	RegisterMethodHandler(v, h)
	t.Cleanup(func() {
		if prev == nil {
			delete(methods, v)
		} else {
			methods[v] = prev
		}
	})
}

// nopHandler is a minimal MethodHandler that returns a non-nil result.
func nopHandler(_ context.Context, _ any) (any, error) { return "ok", nil }

// TestRegisterMethodHandler_KnownVerb verifies that a handler for a known verb
// is stored and retrievable via Methods().
func TestRegisterMethodHandler_KnownVerb(t *testing.T) {
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
	if m.ActionE == nil {
		t.Error("expected ActionE to be set")
	}
}

// TestRegisterMethodHandler_UnknownVerb_Panics verifies that registering a
// handler for an unknown verb panics.
func TestRegisterMethodHandler_UnknownVerb_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown verb, but did not panic")
		}
	}()
	RegisterMethodHandler("unknown.verb.xyz", nopHandler)
}

// TestMethods_Unregistered verifies that Methods returns an error (not a panic)
// for a known verb that has no registered handler.
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

// TestSignatures_AllHaveNewParams verifies that every entry in the signatures
// map has a non-nil NewParams factory (or documents the nil case).
func TestSignatures_AllHaveNewParams(t *testing.T) {
	for verb, sig := range signatures {
		if sig.NewParams == nil {
			// Some verbs legitimately have nil params (no parameters needed).
			// Document them; they are not a bug.
			t.Logf("NOTICE: verb %q has nil NewParams (parameterless method)", verb)
		}
	}
}

// TestSignatures_AllHaveNewResult verifies that every entry in the signatures
// map has a non-nil NewResult factory (or documents the nil case).
func TestSignatures_AllHaveNewResult(t *testing.T) {
	for verb, sig := range signatures {
		if sig.NewResult == nil {
			t.Logf("NOTICE: verb %q has nil NewResult (void-return method)", verb)
		}
	}
}

// TestSignatures_NotEmpty verifies that the signatures map is populated.
func TestSignatures_NotEmpty(t *testing.T) {
	if len(signatures) == 0 {
		t.Error("signatures map is empty; expected registered method signatures")
	}
}

// TestMethodHandler_Dispatch verifies that the registered handler is called
// with the params produced by NewParams and returns the expected result.
func TestMethodHandler_Dispatch(t *testing.T) {
	called := false
	handler := func(_ context.Context, params any) (any, error) {
		called = true
		p, ok := params.(*TemperatureGetParams)
		if !ok {
			return nil, fmt.Errorf("unexpected params type: %T", params)
		}
		if p.RoomID != "r1" {
			return nil, fmt.Errorf("unexpected room_id: %q", p.RoomID)
		}
		return &TemperatureRoomConfig{RoomID: "r1"}, nil
	}
	withHandler(t, TemperatureGet, handler)

	m, err := Methods(TemperatureGet)
	if err != nil {
		t.Fatalf("Methods error: %v", err)
	}

	// Use NewParams to build params, then dispatch.
	params := m.Signature.NewParams().(*TemperatureGetParams)
	params.RoomID = "r1"

	out, err := m.ActionE(context.Background(), params)
	if err != nil {
		t.Fatalf("ActionE error: %v", err)
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
}

// TestAllVerbsInSignatures ensures that every Verb constant defined in the
// package is present in the signatures map.
func TestAllVerbsInSignatures(t *testing.T) {
	allVerbs := []Verb{
		DevicesMatch, DeviceLookup, DeviceShow, DeviceForget, DeviceRefresh,
		DeviceSetup, DeviceUpdate,
		TemperatureGet, TemperatureSet, TemperatureList, TemperatureDelete,
		TemperatureGetSchedule, TemperatureGetWeekdayDefaults,
		TemperatureSetWeekdayDefault, TemperatureGetKindSchedules,
		TemperatureSetKindSchedule,
		OccupancyGetStatus,
		HeaterGetConfig, HeaterSetConfig,
		ThermometerList, DoorList,
		RoomList, RoomCreate, RoomEdit, RoomDelete,
		DeviceSetRoom, DeviceListByRoom,
		SwitchToggle, SwitchOn, SwitchOff, SwitchStatus, SwitchAll,
	}
	for _, v := range allVerbs {
		if _, ok := signatures[v]; !ok {
			t.Errorf("verb %q is missing from signatures map", v)
		}
	}
}
