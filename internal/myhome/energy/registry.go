// Package energy is a minimal, static identity registry of things that may
// claim solar energy — today, only the pool pump. It is deliberately *not* a
// live-arbitration engine: no priority ordering, no partial allocation, no
// per-consumer wattage reporting. It exists so a future "solar router" issue
// (multi-consumer arbitration, see #401's follow-up) has an existing
// substrate to build on rather than starting from nothing.
//
// Structurally this mirrors internal/myhome/accounts (a sync-guarded map
// with Register/Snapshot methods), used here only as a concurrency-pattern
// template — the two packages track conceptually unrelated things.
package energy

import "sync"

// Claimer is a static identity record for something that may consume solar
// energy. This registry does not track live wattage or arbitrate between
// claimers — see the follow-up "solar router" issue for that.
type Claimer struct {
	Name     string `json:"name"`
	DeviceID string `json:"device_id,omitempty"`
}

// Registry holds the set of known claimers, keyed by name. Safe for
// concurrent use: Register is expected to be called once per claimer at
// daemon startup, Snapshot from RPC handlers.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Claimer
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Claimer)}
}

// Register records (or replaces) the identity of a claimer. Calling it again
// with the same name overwrites the previous entry.
func (r *Registry) Register(name, deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName[name] = Claimer{Name: name, DeviceID: deviceID}
}

// Snapshot returns a stable, name-sorted copy of every registered claimer.
// Never returns the internal map, so callers can't mutate registry state
// through the result.
func (r *Registry) Snapshot() []Claimer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Claimer, 0, len(r.byName))
	for _, c := range r.byName {
		out = append(out, c)
	}
	sortByName(out)
	return out
}

// sortByName is a tiny insertion sort — the claimer count is a handful, so
// avoiding a sort.Slice import keeps this file dependency-free (mirrors
// internal/myhome/accounts.sortByName).
func sortByName(c []Claimer) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].Name < c[j-1].Name; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}
