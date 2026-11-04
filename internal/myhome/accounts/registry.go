// Package accounts tracks the connection status of the external accounts
// myhome integrates with (Beem Energy, SFR box, SMTP, MQTT broker). Each
// integration already gates on credential presence and logs failures inline;
// this package adds nothing to that behavior beyond recording the outcome of
// each attempt so the UI (and /health) can show "which accounts, and did the
// last connection succeed" without scraping logs.
package accounts

import (
	"sync"
	"time"
)

// Status is a snapshot of one account's configuration and last connection
// attempt. Enabled is false when credentials are absent (the integration
// never even tries to connect); LastAttempt is the zero time until the first
// attempt is reported.
type Status struct {
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	LastOK      bool      `json:"last_ok"`
	LastAttempt time.Time `json:"last_attempt"`
	LastError   string    `json:"last_error,omitempty"`
}

// Registry holds the current Status of every tracked account. Safe for
// concurrent use: Report/SetEnabled are called from each integration's own
// goroutine, Snapshot from the UI's request handler.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]*Status
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]*Status)}
}

// SetEnabled records whether an account is configured at all. Call this once
// at startup based on credential presence, before any Report calls, so a
// disabled integration reads as "not configured" rather than "failed".
func (r *Registry) SetEnabled(name string, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.entry(name)
	s.Enabled = on
}

// Report records the outcome of one connection/poll attempt for name. A nil
// err means success.
func (r *Registry) Report(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.entry(name)
	s.Enabled = true
	s.LastAttempt = time.Now()
	s.LastOK = err == nil
	if err != nil {
		s.LastError = err.Error()
	} else {
		s.LastError = ""
	}
}

// entry returns the Status for name, creating it if this is the first time
// name has been seen. Callers must hold r.mu.
func (r *Registry) entry(name string) *Status {
	s, ok := r.byName[name]
	if !ok {
		s = &Status{Name: name}
		r.byName[name] = s
	}
	return s
}

// Snapshot returns a stable, name-sorted copy of every tracked account's
// current Status.
func (r *Registry) Snapshot() []Status {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Status, 0, len(r.byName))
	for _, s := range r.byName {
		out = append(out, *s)
	}
	sortByName(out)
	return out
}

// sortByName is a tiny insertion sort — the account count is a handful
// (beem/sfr/smtp/mqtt), so avoiding a sort.Slice import keeps this file
// dependency-free.
func sortByName(s []Status) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Name < s[j-1].Name; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
