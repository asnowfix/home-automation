package script

import (
	"maps"
	"reflect"

	"github.com/dop251/goja"
)

// VirtualComponentState is the persisted backing data for one Virtual
// component (Number, Text, Boolean, Enum, Button, or Group). Real devices
// only expose handles for components configured ahead of time through the
// device UI/config — Virtual.getHandle never creates one — so entries are
// populated by tests or a device snapshot, like DeviceState.ComponentStatus.
type VirtualComponentState struct {
	Kind   string         `json:"kind"` // number, text, boolean, enum, button, group
	Value  any            `json:"value,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}

// virtualListener is one Virtual instance on()/off() registration.
type virtualListener struct {
	id       int
	event    string
	callback goja.Callable
}

// installVirtual adds the Virtual global (Virtual.getHandle):
// https://shelly-api-docs.shelly.cloud/gen2/Scripts/APIs/Virtual
//
// It returns a trigger function that fires a named event (e.g. "single_push"
// for a Button) on listeners registered through a handle's on(); used by
// tests to simulate events that have no JS-side setter (buttons have no
// setValue). setValue's own "change" event firing doesn't need it.
func installVirtual(vm *goja.Runtime, deviceState *DeviceState) (trigger func(key, event string, data map[string]any)) {
	listeners := make(map[string][]*virtualListener)
	nextListenerID := 1

	virtualObj := vm.NewObject()
	virtualObj.Set("getHandle", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		comp, ok := deviceState.Virtual[key]
		if !ok {
			return goja.Null()
		}
		return buildVirtualInstance(vm, deviceState, key, comp, listeners, &nextListenerID)
	})
	vm.Set("Virtual", virtualObj)

	return func(key, event string, data map[string]any) {
		fireVirtualEvent(vm, listeners[key], event, data)
	}
}

func buildVirtualInstance(vm *goja.Runtime, deviceState *DeviceState, key string, comp *VirtualComponentState, listeners map[string][]*virtualListener, nextListenerID *int) *goja.Object {
	obj := vm.NewObject()

	obj.Set("getValue", func(call goja.FunctionCall) goja.Value {
		if comp.Value == nil {
			return goja.Undefined()
		}
		return vm.ToValue(comp.Value)
	})

	obj.Set("setValue", func(call goja.FunctionCall) goja.Value {
		newValue := call.Argument(0).Export()
		changed := !reflect.DeepEqual(comp.Value, newValue)
		comp.Value = newValue
		if deviceState.OnModified != nil {
			deviceState.OnModified()
		}
		// Buttons are momentary (single_push, etc.); they have no "change"
		// event and setValue isn't a real operation on them.
		if changed && comp.Kind != "button" {
			fireVirtualEvent(vm, listeners[key], "change", map[string]any{"value": newValue})
		}
		return goja.Undefined()
	})

	obj.Set("getStatus", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(map[string]any{"id": key, "value": comp.Value})
	})

	obj.Set("getConfig", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(comp.Config)
	})

	obj.Set("setConfig", func(call goja.FunctionCall) goja.Value {
		if cfg, ok := call.Argument(0).Export().(map[string]any); ok {
			if comp.Config == nil {
				comp.Config = make(map[string]any)
			}
			maps.Copy(comp.Config, cfg)
		}
		if deviceState.OnModified != nil {
			deviceState.OnModified()
		}
		return goja.Undefined()
	})

	obj.Set("on", func(call goja.FunctionCall) goja.Value {
		event := call.Argument(0).String()
		callback, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.ToValue("Virtual instance on(): second argument must be a function"))
		}
		id := *nextListenerID
		*nextListenerID++
		listeners[key] = append(listeners[key], &virtualListener{id: id, event: event, callback: callback})
		return vm.ToValue(id)
	})

	obj.Set("off", func(call goja.FunctionCall) goja.Value {
		id := int(call.Argument(0).ToInteger())
		ls := listeners[key]
		for i, l := range ls {
			if l.id == id {
				listeners[key] = append(ls[:i], ls[i+1:]...)
				break
			}
		}
		return goja.Undefined()
	})

	return obj
}

func fireVirtualEvent(vm *goja.Runtime, ls []*virtualListener, event string, data map[string]any) {
	for _, l := range ls {
		if l.event != event {
			continue
		}
		l.callback(goja.Undefined(), vm.ToValue(data))
	}
}
