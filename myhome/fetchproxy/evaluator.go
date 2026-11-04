package fetchproxy

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// Evaluation bounds, per #465 "Bound every evaluation": a wall-clock
// Interrupt, a cap on input body size, a cap on serialised output, and
// SetMaxCallStackSize. goja has no memory-limit API — verified in the issue
// discussion against the module version this package pins — so a
// pathological allocation inside a transform is not caught here; only
// infinite loops (via Interrupt) and unbounded recursion (via
// SetMaxCallStackSize) are.
const (
	// DefaultEvalTimeout bounds the wall-clock time a transform may run.
	// Reductions here are "pick a couple of fields out of a small JSON
	// object" — a few hundred milliseconds is generous, not tight.
	DefaultEvalTimeout = 250 * time.Millisecond

	// DefaultMaxCallStackSize bounds recursion depth.
	DefaultMaxCallStackSize = 64

	// DefaultMaxInputBytes bounds the response body handed to a transform.
	// Open-Meteo forecast responses run a few KB; this is a generous cap
	// against a misconfigured or hostile upstream, not a tight one.
	DefaultMaxInputBytes = 1 << 20 // 1 MiB

	// DefaultMaxOutputBytes bounds the transform's *own* serialised result —
	// the "few hundred bytes" hard cap from #465. This is a Go constant, not
	// a runtime config option: CLAUDE.md prefers a constant over a new
	// option that would otherwise require touching options.go, run.go,
	// docs/configuration.md and myhome-example.yaml for a value nobody needs
	// to tune per-install.
	DefaultMaxOutputBytes = 300
)

// EvalLimits bounds a single transform evaluation.
type EvalLimits struct {
	Timeout          time.Duration
	MaxInputBytes    int
	MaxOutputBytes   int
	MaxCallStackSize int
}

// DefaultLimits returns the bounds used in production. Tests use tighter
// values (e.g. a shorter Timeout) so an infinite-loop test does not have to
// wait 250ms to prove it was interrupted.
func DefaultLimits() EvalLimits {
	return EvalLimits{
		Timeout:          DefaultEvalTimeout,
		MaxInputBytes:    DefaultMaxInputBytes,
		MaxOutputBytes:   DefaultMaxOutputBytes,
		MaxCallStackSize: DefaultMaxCallStackSize,
	}
}

// Evaluate runs transformSrc — JS source for a function of one argument (the
// response body, as a string) — against body, and returns the transform's
// result re-marshalled to compact JSON.
//
// Nothing is exposed to the script beyond that one argument: Evaluate builds
// a bare goja.Runtime with no globals set on it at all, so there is no
// filesystem, no network, no require, no host object of any kind reachable
// from the transform — only what ECMAScript itself provides (including the
// standard JSON object, which is how the example transforms in #465 parse
// the body).
//
// Never returns a value on error: on a compile error, a thrown exception, an
// interrupted (timed-out) evaluation, a non-object result, or an oversized
// result, the caller must treat this exactly like a fetch failure — leave
// the last good retained value untouched (#465 "Never publish a bad
// payload").
func Evaluate(transformSrc string, body []byte, limits EvalLimits) (json.RawMessage, error) {
	if limits.MaxInputBytes > 0 && len(body) > limits.MaxInputBytes {
		return nil, fmt.Errorf("input body %d bytes exceeds cap %d", len(body), limits.MaxInputBytes)
	}

	vm := goja.New()
	vm.SetMaxCallStackSize(limits.MaxCallStackSize)

	if limits.Timeout > 0 {
		timer := time.AfterFunc(limits.Timeout, func() {
			vm.Interrupt("transform exceeded its time budget")
		})
		defer timer.Stop()
	}

	// Wrapping in parentheses turns the transform source into a function
	// expression evaluated to a value, rather than a statement — "function
	// f(x){...}" at statement position is a declaration, and an anonymous
	// "function(x){...}" at statement position is a syntax error.
	fnValue, err := vm.RunString("(" + transformSrc + ")")
	if err != nil {
		return nil, fmt.Errorf("transform did not compile: %w", err)
	}
	callable, ok := goja.AssertFunction(fnValue)
	if !ok {
		return nil, fmt.Errorf("transform is not a function")
	}

	result, err := callable(goja.Undefined(), vm.ToValue(string(body)))
	if err != nil {
		return nil, fmt.Errorf("transform threw or was interrupted: %w", err)
	}

	exported := result.Export()
	if _, ok := exported.(map[string]interface{}); !ok {
		return nil, fmt.Errorf("transform must return an object, got %T", exported)
	}

	out, err := json.Marshal(exported)
	if err != nil {
		return nil, fmt.Errorf("transform result is not serialisable: %w", err)
	}
	if limits.MaxOutputBytes > 0 && len(out) > limits.MaxOutputBytes {
		return nil, fmt.Errorf("transform output %d bytes exceeds cap %d", len(out), limits.MaxOutputBytes)
	}
	return out, nil
}
