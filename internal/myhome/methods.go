package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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

// Registry holds the set of RPC methods registered for one myhome instance.
// It replaces the old package-level `methods` map: the daemon (the
// composition root, see myhome/daemon.NewDaemon) constructs exactly one
// Registry per process and passes it explicitly to every service
// constructor that registers a handler (DeviceManager, temperature.Service,
// occupancy.RPCHandler, ...) and to every reader (the RPC server's handler,
// the UI's RPC/HTMX handlers). A test that needs to register a handler can
// construct its own throwaway Registry instead of reaching for shared
// process-wide state, which is what makes RPC handler tests safe to run
// with t.Parallel().
//
// The map itself was previously unsynchronized package-level state (no
// mutex at all); the RWMutex here is a genuine bug fix alongside the DI
// change, not just a refactor.
type Registry struct {
	mu      sync.RWMutex
	methods map[Verb]*Method
}

// NewRegistry returns an empty Registry ready for use.
func NewRegistry() *Registry {
	return &Registry{methods: make(map[Verb]*Method)}
}

// Methods looks up the Method registered for name on r.
func (r *Registry) Methods(name Verb) (*Method, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, exists := r.methods[name]
	if !exists {
		return nil, fmt.Errorf("unknown or unregistered method %s", name)
	}
	return m, nil
}

// RestoreMethod re-registers m verbatim under verb on r, bypassing the P/R
// decode wrapper Register builds. It exists for test cleanup: a test that
// swaps in a replacement handler via Register can save the previous *Method
// (from r.Methods) and put it back exactly via r.RestoreMethod in
// t.Cleanup, without needing to reconstruct a typed Register call for a
// handler whose P/R it may not know.
func (r *Registry) RestoreMethod(verb Verb, m *Method) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods[verb] = m
}

// Register registers h as the handler for verb on r. P is the method's
// parameter type; it is decoded from the request's raw JSON exactly once,
// directly into P — no intermediate "unmarshal once to see what we've got,
// then unmarshal the whole message again with the right type" step. R is
// the method's result type. Both are normally inferred from h, so a
// registration is a single, compile-time-checked expression:
//
//	myhome.Register(reg, myhome.DeviceShow, func(ctx context.Context, p *myhome.DeviceShowParams) (*myhome.Device, error) {
//	    ...
//	})
//
// Register stays a package-level generic function rather than a method on
// *Registry: Go does not allow a method to introduce type parameters beyond
// its receiver's, so r is passed explicitly as the first argument instead.
//
// Registering the same verb twice on the same r overwrites the previous
// registration; there is no Unregister — tests that need to swap a handler
// just call Register again and restore the previous *Method (via r.Methods)
// in t.Cleanup via r.RestoreMethod.
func Register[P, R any](r *Registry, verb Verb, h func(ctx context.Context, p P) (R, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods[verb] = &Method{
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
