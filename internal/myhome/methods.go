package myhome

import (
	"context"
	"encoding/json"
	"fmt"
)

// Action is the uniform, wire-facing entry point for a registered RPC
// method: it receives the request's params exactly as decoded off the wire
// (raw JSON, not yet interpreted) and returns the untyped result to be
// marshalled back onto the wire. Every Method built by Register shares this
// same shape regardless of its real parameter/result types.
type Action func(ctx context.Context, params json.RawMessage) (any, error)

type Method struct {
	Name   Verb
	Action Action
}

// Call is a convenience wrapper for in-process callers that already hold a
// typed params value instead of raw JSON — e.g. the UI's htmx/rpc handlers,
// or DeviceManager's local dispatch shortcut that bypasses MQTT entirely. It
// marshals params once and delegates to Action, so every call path (network
// or in-process) decodes params through the exact same Register-generated
// logic.
func (m *Method) Call(ctx context.Context, params any) (any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params for %s: %w", m.Name, err)
	}
	return m.Action(ctx, raw)
}

// Methods looks up the Method registered for name.
func Methods(name Verb) (*Method, error) {
	m, exists := methods[name]
	if !exists {
		return nil, fmt.Errorf("unknown or unregistered method %s", name)
	}
	return m, nil
}

// Register registers h as the handler for verb. P is the method's parameter
// type; it is decoded from the request's raw JSON exactly once, directly
// into P — no intermediate "unmarshal once to see what we've got, then
// unmarshal the whole message again with the right type" step. R is the
// method's result type. Both are normally inferred from h, so a
// registration is a single, compile-time-checked expression:
//
//	myhome.Register(myhome.DeviceShow, func(ctx context.Context, p *myhome.DeviceShowParams) (*myhome.Device, error) {
//	    ...
//	})
//
// This replaces the old two-step ritual (add an entry to the `signatures`
// map keyed by func() any constructors, then call RegisterMethodHandler) and
// the runtime reflect.TypeOf check it required on the client: P is now
// enforced by the compiler at every call site via the generic Call helper.
//
// Registering the same verb twice panics, matching the previous behavior of
// RegisterMethodHandler.
func Register[P, R any](verb Verb, h func(ctx context.Context, p P) (R, error)) {
	if _, exists := methods[verb]; exists {
		panic(fmt.Errorf("method %s already registered", verb))
	}
	methods[verb] = &Method{
		Name: verb,
		Action: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p P
			if len(raw) > 0 && string(raw) != "null" {
				if err := json.Unmarshal(raw, &p); err != nil {
					return nil, fmt.Errorf("unmarshal params for %s: %w", verb, err)
				}
			}
			return h(ctx, p)
		},
	}
}

var methods = make(map[Verb]*Method)
