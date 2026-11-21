package alert

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

const (
	DefaultHold  = 5 * time.Minute  // resolve hold: a cleared condition must stay clear this long
	DefaultGrace = 10 * time.Minute // after process start, new firings are held
)

// Supersedes reports whether a firing document opens a newer episode of the same fingerprint than
// open, which delivery then closes silently.
func (a *Alert) Supersedes(open *Alert) bool {
	return a.State == Firing && open != nil && a.Fingerprint == open.Fingerprint && a.Since > open.Since
}

// Rule is the per-rule policy; the condition itself is evaluated by the producer.
type Rule struct {
	Name     string
	Source   string
	Severity Severity
	For      time.Duration // the condition must hold this long before firing (0: immediately)
	Hold     time.Duration // resolve hold; 0 means DefaultHold
}

// Observation is a producer's reading of one key (one fingerprint of the rule) at this tick.
type Observation struct {
	Key     string
	Active  bool // the condition is true
	Frozen  bool // pause the key's state machine (e.g. a member of a grouped alert); wall-clock timers keep running
	Summary string
	Detail  string
	Labels  map[string]string
}

// Evaluator runs the inactive -> pending -> firing -> resolving -> inactive machine for every key and
// publishes only the transitions to firing and to resolved. Each transition is published first and
// persisted second: a crash in between can duplicate a firing, never lose a resolve.
type Evaluator struct {
	log   logr.Logger
	pub   Publisher
	store *Store
	now   func() time.Time
	grace time.Duration

	mu    sync.Mutex
	start time.Time // grace is relative to process start
	rows  map[string]*row
}

// Option configures an Evaluator.
type Option func(*Evaluator)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(e *Evaluator) { e.now = now } }

// WithGrace replaces DefaultGrace.
func WithGrace(d time.Duration) Option { return func(e *Evaluator) { e.grace = d } }

// NewEvaluator restores the persisted state. Create it after delivery has subscribed and Eval only after mc.Start().
func NewEvaluator(ctx context.Context, log logr.Logger, pub Publisher, store *Store, opts ...Option) (*Evaluator, error) {
	e := &Evaluator{log: log, pub: pub, store: store, now: time.Now, grace: DefaultGrace, rows: map[string]*row{}}
	for _, o := range opts {
		o(e)
	}
	e.start = e.now()
	rows, err := store.load(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		e.rows[rows[i].Fingerprint] = &rows[i]
	}
	return e, nil
}

// ErrContract marks an Eval error caused by what the producer passed in (rule, key or texts).
var ErrContract = errors.New("alert: contract violation")

func contractErr(err error) error { return fmt.Errorf("%w: %w", ErrContract, err) }

// Eval advances the keys of rule. A key with state but absent from obs counts as inactive, except
// during the startup grace, when it is frozen (its producer may not observe it yet).
//
// Error contract: only contract violations are returned (ErrContract: bad rule, key or texts); the
// caller fixes its producer. Invalid texts never block a transition: the last valid texts are kept.
// Transient publish and persist failures are logged once and not returned; the state stays put and
// the next Eval retries, so the caller never needs to retry. Not calling Eval freezes the whole rule.
func (e *Evaluator) Eval(ctx context.Context, rule Rule, obs []Observation) error {
	if err := rule.validate(); err != nil {
		return contractErr(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	seen := map[string]bool{}
	var errs []error
	for _, o := range obs {
		seen[o.Key] = true
		if !nameRE.MatchString(o.Key) {
			errs = append(errs, contractErr(fmt.Errorf("key %q must match %s", o.Key, nameRE)))
			continue
		}
		if o.Frozen { // timers keep running while frozen; only the state machine is paused
			continue
		}
		if err := sanitize(&o); err != nil {
			errs = append(errs, contractErr(err))
		}
		if e.rows[rule.Name+"/"+o.Key] == nil && o.Active && o.Summary == "" {
			errs = append(errs, contractErr(fmt.Errorf("key %q: a new alert needs a valid summary", o.Key)))
			continue
		}
		e.step(ctx, rule, o, now)
	}
	if now.Sub(e.start) >= e.grace {
		for _, r := range e.rows {
			if r.Rule == rule.Name && !seen[r.Key] {
				e.step(ctx, rule, Observation{Key: r.Key}, now)
			}
		}
	}
	return errors.Join(errs...)
}

func (r Rule) validate() error {
	switch {
	case !nameRE.MatchString(r.Name):
		return fmt.Errorf("rule name %q must match %s", r.Name, nameRE)
	case r.Severity != Critical && r.Severity != Warning:
		return fmt.Errorf("rule %s: bad severity %q", r.Name, r.Severity)
	case r.Source == "":
		return fmt.Errorf("rule %s: empty source", r.Name)
	}
	return nil
}

// sanitize drops the texts that would fail Validate, so they can never wedge a transition.
func sanitize(o *Observation) error {
	var errs []error
	if err := checkText("summary", o.Summary, MaxSummary); err != nil {
		o.Summary = ""
		errs = append(errs, err)
	}
	if err := checkText("detail", o.Detail, MaxDetail); err != nil {
		o.Detail = ""
		errs = append(errs, err)
	}
	if err := checkLabels(o.Labels); err != nil {
		o.Labels = nil
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// step advances one key. Failures are logged here and leave the state unchanged for the next Eval.
func (e *Evaluator) step(ctx context.Context, rule Rule, o Observation, now time.Time) {
	hold := rule.Hold
	if hold == 0 {
		hold = DefaultHold
	}
	fp := rule.Name + "/" + o.Key
	r := e.rows[fp]
	if r == nil {
		if !o.Active {
			return
		}
		r = e.open(ctx, rule, o, now)
	} else {
		updateTexts(r, o)
	}

	// The hold is over: close the episode. A condition that is active again starts a NEW one; it never
	// slips back into the closed episode (which delivery has already resolved).
	if r.State == "resolving" && now.Sub(time.Unix(r.HoldStart, 0)) >= hold {
		if !e.publish(ctx, r, Resolved, now) {
			return
		}
		e.forget(ctx, fp)
		if !o.Active {
			return
		}
		r = e.open(ctx, rule, o, now)
	}

	switch {
	case r.State == "pending" && !o.Active:
		e.forget(ctx, fp) // a pending condition that clears is dropped silently
	case r.State == "pending":
		if now.Sub(time.Unix(r.PendingSince, 0)) >= rule.For && now.Sub(e.start) >= e.grace && e.publish(ctx, r, Firing, now) {
			r.State = "firing"
			e.persist(ctx, r)
		}
	case r.State == "firing" && !o.Active:
		r.State, r.HoldStart = "resolving", now.Unix()
		e.persist(ctx, r)
	case r.State == "resolving" && o.Active: // hold not over (checked above)
		r.State, r.HoldStart = "firing", 0
		e.persist(ctx, r)
	}
}

// open starts a new pending episode.
func (e *Evaluator) open(ctx context.Context, rule Rule, o Observation, now time.Time) *row {
	r := &row{Fingerprint: rule.Name + "/" + o.Key, Rule: rule.Name, Key: o.Key, State: "pending", Labels: "{}",
		Severity: string(rule.Severity), Source: rule.Source, PendingSince: now.Unix(), Since: now.Unix()}
	e.rows[r.Fingerprint] = r
	updateTexts(r, o)
	e.persist(ctx, r)
	return r
}

// updateTexts keeps the last non-empty texts: an observation may omit what it has nothing new to say about.
func updateTexts(r *row, o Observation) {
	if o.Summary != "" {
		r.Summary = o.Summary
	}
	if o.Detail != "" {
		r.Detail = o.Detail
	}
	if len(o.Labels) > 0 {
		r.Labels = encodeLabels(o.Labels)
	}
}

// persist and forget run to completion even if ctx is cancelled by a shutdown right after a publish.
func (e *Evaluator) persist(ctx context.Context, r *row) {
	if err := e.store.save(context.WithoutCancel(ctx), r); err != nil {
		e.log.Error(err, "persisting alert_state failed; in-memory state continues", "fingerprint", r.Fingerprint)
	}
}

func (e *Evaluator) forget(ctx context.Context, fp string) {
	delete(e.rows, fp)
	if err := e.store.delete(context.WithoutCancel(ctx), fp); err != nil {
		e.log.Error(err, "deleting alert_state failed", "fingerprint", fp)
	}
}

func (e *Evaluator) publish(ctx context.Context, r *row, st State, now time.Time) bool {
	if err := e.pub.Publish(ctx, e.document(r, st, now)); err != nil {
		e.log.Error(err, "publishing alert failed; retried on the next evaluation", "fingerprint", r.Fingerprint, "state", st)
		return false
	}
	return true
}

func (e *Evaluator) document(r *row, st State, now time.Time) *Alert {
	a := &Alert{V: Version, Fingerprint: r.Fingerprint, Rule: r.Rule, Key: r.Key, Severity: Severity(r.Severity),
		State: st, Source: r.Source, Since: r.Since, Ts: now.Unix(), Summary: r.Summary, Detail: r.Detail,
		Labels: decodeLabels(r.Labels)}
	if st == Resolved {
		a.Ended = r.HoldStart
	}
	return a
}

// Snapshot returns the open (firing or resolving) alerts, sorted by fingerprint, for a producer's own panel.
func (e *Evaluator) Snapshot() []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Alert
	now := e.now()
	for _, r := range e.rows {
		if r.State != "pending" {
			out = append(out, *e.document(r, Firing, now))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out
}
