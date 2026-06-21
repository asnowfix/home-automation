package script

import (
	"testing"

	"github.com/dop251/goja"
)

func newVirtualTestVM(t *testing.T, state *DeviceState) (*goja.Runtime, func(key, event string, data map[string]any)) {
	t.Helper()
	vm := goja.New()
	trigger := installVirtual(vm, state)
	return vm, trigger
}

func TestVirtualGetHandleMissingKeyReturnsNull(t *testing.T) {
	state := &DeviceState{Virtual: map[string]*VirtualComponentState{}}
	vm, _ := newVirtualTestVM(t, state)

	result, err := vm.RunString(`Virtual.getHandle("number:200") === null`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	if !result.ToBoolean() {
		t.Error("getHandle on a missing key did not return null")
	}
}

func TestVirtualNumberGetSetValue(t *testing.T) {
	state := &DeviceState{Virtual: map[string]*VirtualComponentState{
		"number:200": {Kind: "number", Value: 21.5, Config: map[string]any{"min": 0.0, "max": 100.0}},
	}}
	vm, _ := newVirtualTestVM(t, state)

	result, err := vm.RunString(`
		var h = Virtual.getHandle("number:200");
		var before = h.getValue();
		h.setValue(42);
		var after = h.getValue();
		var cfg = h.getConfig();
		JSON.stringify({before: before, after: after, max: cfg.max});
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	got := result.String()
	want := `{"before":21.5,"after":42,"max":100}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if state.Virtual["number:200"].Value != int64(42) {
		t.Errorf("backing state Value = %v, want 42", state.Virtual["number:200"].Value)
	}
}

func TestVirtualSetValueFiresChangeEvent(t *testing.T) {
	state := &DeviceState{Virtual: map[string]*VirtualComponentState{
		"boolean:201": {Kind: "boolean", Value: false},
	}}
	vm, _ := newVirtualTestVM(t, state)

	result, err := vm.RunString(`
		var seen = null;
		var h = Virtual.getHandle("boolean:201");
		h.on("change", function(data) { seen = data.value; });
		h.setValue(true);
		seen;
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	if !result.ToBoolean() {
		t.Errorf("change listener did not observe the new value: %v", result.Export())
	}
}

func TestVirtualSetValueNoChangeNoEvent(t *testing.T) {
	state := &DeviceState{Virtual: map[string]*VirtualComponentState{
		"boolean:202": {Kind: "boolean", Value: true},
	}}
	vm, _ := newVirtualTestVM(t, state)

	result, err := vm.RunString(`
		var fired = false;
		var h = Virtual.getHandle("boolean:202");
		h.on("change", function(data) { fired = true; });
		h.setValue(true); // same value: no change event
		fired;
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	if result.ToBoolean() {
		t.Error("change event fired even though the value didn't change")
	}
}

func TestVirtualOffRemovesListener(t *testing.T) {
	state := &DeviceState{Virtual: map[string]*VirtualComponentState{
		"text:203": {Kind: "text", Value: "a"},
	}}
	vm, _ := newVirtualTestVM(t, state)

	result, err := vm.RunString(`
		var count = 0;
		var h = Virtual.getHandle("text:203");
		var id = h.on("change", function() { count++; });
		h.setValue("b");
		h.off(id);
		h.setValue("c");
		count;
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	if got := result.ToInteger(); got != 1 {
		t.Errorf("listener fired %d times after off(), want 1", got)
	}
}

func TestVirtualButtonPushEventViaTrigger(t *testing.T) {
	state := &DeviceState{Virtual: map[string]*VirtualComponentState{
		"button:204": {Kind: "button"},
	}}
	vm, trigger := newVirtualTestVM(t, state)

	_, err := vm.RunString(`
		var pushes = 0;
		var h = Virtual.getHandle("button:204");
		h.on("single_push", function() { pushes++; });
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}

	// Buttons are momentary: there's no setValue path, so a real hardware
	// press is simulated through the Go-side trigger, same as the
	// device-event injector used elsewhere in this package.
	trigger("button:204", "single_push", nil)

	result, err := vm.RunString(`pushes`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	if got := result.ToInteger(); got != 1 {
		t.Errorf("pushes = %d, want 1", got)
	}
}
