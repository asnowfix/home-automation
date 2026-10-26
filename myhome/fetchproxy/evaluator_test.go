package fetchproxy

import (
	"strings"
	"testing"
	"time"
)

func testLimits() EvalLimits {
	return EvalLimits{
		Timeout:          50 * time.Millisecond,
		MaxInputBytes:    1 << 16,
		MaxOutputBytes:   300,
		MaxCallStackSize: 64,
	}
}

func TestEvaluateHappyPath(t *testing.T) {
	transform := `function(body) { var d = JSON.parse(body); return {sunrise: d.sunrise}; }`
	out, err := Evaluate(transform, []byte(`{"sunrise":"06:12"}`), testLimits())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := string(out); got != `{"sunrise":"06:12"}` {
		t.Fatalf("unexpected output: %s", got)
	}
}

// TestEvaluateThrows covers "a transform that throws" from #465's test list.
// The retained value must be left alone by the caller — Evaluate's job is
// just to surface the error.
func TestEvaluateThrows(t *testing.T) {
	transform := `function(body) { throw new Error("boom"); }`
	_, err := Evaluate(transform, []byte(`{}`), testLimits())
	if err == nil {
		t.Fatal("expected an error from a throwing transform, got nil")
	}
	if !strings.Contains(err.Error(), "threw") {
		t.Fatalf("expected error to mention the throw, got: %v", err)
	}
}

// TestEvaluateInfiniteLoopIsInterrupted covers "one that loops forever (must
// be interrupted)". The test itself is time-bounded well above the
// configured Timeout so a regression that removes the Interrupt call hangs
// the test suite loudly instead of passing silently.
func TestEvaluateInfiniteLoopIsInterrupted(t *testing.T) {
	transform := `function(body) { while (true) { } }`

	done := make(chan error, 1)
	go func() {
		_, err := Evaluate(transform, []byte(`{}`), testLimits())
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from an interrupted infinite loop, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Evaluate did not return within 2s of a 50ms timeout — Interrupt is not bounding the loop")
	}
}

// TestEvaluateOversizedResultIsRejected covers "one returning an oversized
// result" — the cap is applied to the serialised output, not to some proxy
// for it.
func TestEvaluateOversizedResultIsRejected(t *testing.T) {
	transform := `function(body) {
		var s = "";
		for (var i = 0; i < 1000; i++) { s += "x"; }
		return {big: s};
	}`
	_, err := Evaluate(transform, []byte(`{}`), testLimits())
	if err == nil {
		t.Fatal("expected an error for an oversized transform result, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds cap") {
		t.Fatalf("expected error to mention the output cap, got: %v", err)
	}
}

// TestEvaluateNonObjectResultIsRejected covers #465 decision 6: the
// transform must return an object; the daemon owns serialisation.
func TestEvaluateNonObjectResultIsRejected(t *testing.T) {
	cases := map[string]string{
		"string": `function(body) { return "just a string"; }`,
		"number": `function(body) { return 42; }`,
		"array":  `function(body) { return [1, 2, 3]; }`,
		"null":   `function(body) { return null; }`,
	}
	for name, transform := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Evaluate(transform, []byte(`{}`), testLimits())
			if err == nil {
				t.Fatalf("expected an error for a %s result, got nil", name)
			}
		})
	}
}

func TestEvaluateDoesNotCompile(t *testing.T) {
	_, err := Evaluate("not valid javascript {{{", []byte(`{}`), testLimits())
	if err == nil {
		t.Fatal("expected a compile error, got nil")
	}
}

// TestEvaluateHasNoHostObjects proves the transform cannot reach outside
// itself: no require, no filesystem, no network global is exposed.
func TestEvaluateHasNoHostObjects(t *testing.T) {
	transform := `function(body) {
		if (typeof require !== "undefined") { return {leak: "require"}; }
		if (typeof fetch !== "undefined") { return {leak: "fetch"}; }
		if (typeof process !== "undefined") { return {leak: "process"}; }
		return {ok: true};
	}`
	out, err := Evaluate(transform, []byte(`{}`), testLimits())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := string(out); got != `{"ok":true}` {
		t.Fatalf("transform observed a host object it should not have access to: %s", got)
	}
}

func TestEvaluateInputSizeCap(t *testing.T) {
	limits := testLimits()
	limits.MaxInputBytes = 10
	_, err := Evaluate(`function(body){return {ok:true};}`, []byte("this body is definitely longer than ten bytes"), limits)
	if err == nil {
		t.Fatal("expected an error for an oversized input body, got nil")
	}
}
