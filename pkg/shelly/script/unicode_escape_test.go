package script

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// A note on how the fixtures below are built: every test input that needs
// to contain the literal source text of a unicode escape sequence
// (backslash, letter u, then hex digits) is assembled at runtime via
// string concatenation from the `backslash` constant, rather than typed
// as a contiguous literal anywhere in this file's source -- including in
// comments. This is deliberate: while authoring this package, typing that
// exact adjacent pattern directly was observed to occasionally get
// silently decoded into the actual Unicode character somewhere upstream
// of the Go compiler (in this tooling's own text handling), which would
// silently defeat the very tests meant to guard against that class of
// bug. Building fixtures from separate tokens sidesteps the issue
// entirely and keeps the tests trustworthy regardless of that quirk.

// backslash is a single literal backslash character.
const backslash = "\\"

// escapeSeq returns the literal source text of a unicode escape sequence
// for the given 4 hex digits: a real backslash byte, the letter u, then
// hex4. For hex4 "2713" this is exactly the 6-byte sequence a pre-fix
// esbuild ASCII charset emitted in place of pool-pump.js's checkmark
// character, and exactly what real Shelly firmware's tokenizer rejects
// (see uXXXXDeviceError in es5check.go).
func escapeSeq(hex4 string) string {
	return backslash + "u" + hex4
}

// doubledBackslashThenLiteralU returns two literal backslash bytes
// followed by the letter u and the given hex digits: an escaped backslash
// character followed by ordinary literal text, NOT a unicode escape
// sequence. This is the false-positive case FindUnicodeEscapes must not
// flag -- e.g. the JS string literal built from this for hex4 "2713" is a
// string containing one literal backslash character followed by the four
// literal characters u, 2, 7, 1, 3.
func doubledBackslashThenLiteralU(hex4 string) string {
	return backslash + backslash + "u" + hex4
}

// TestFindUnicodeEscapes_RawNonASCIIPasses is the regression guard against
// "fixing" FindUnicodeEscapes into a non-ASCII check. Raw, un-escaped
// UTF-8 bytes -- a checkmark character and an em dash here -- are exactly
// the kind of bytes pool-pump.js ships today via the default tdewolff
// minifier (~30 of them), and that exact script is running correctly on
// the production Pro1 and on a Plus1 right now. They must never be
// flagged.
func TestFindUnicodeEscapes_RawNonASCIIPasses(t *testing.T) {
	src := []byte(`print("✓ done — ok");`)
	if found := FindUnicodeEscapes(src); len(found) != 0 {
		t.Fatalf("raw non-ASCII bytes must not be flagged as device-rejected escapes, got %v", found)
	}
}

// TestFindUnicodeEscapes_LiteralEscapeFails is the actual bug this
// package closes: the literal source text of a unicode escape sequence
// must be flagged, because real Shelly firmware rejects it with
// "SyntaxError: \uXXXX literals are disallowed" (a real device crash,
// once, on a Plus1 running pool-pump.js -- see uXXXXDeviceError).
func TestFindUnicodeEscapes_LiteralEscapeFails(t *testing.T) {
	src := []byte(`print("` + escapeSeq("2713") + ` done");`)
	want := escapeSeq("2713")
	found := FindUnicodeEscapes(src)
	if len(found) != 1 || found[0] != want {
		t.Fatalf("FindUnicodeEscapes(%q) = %v, want [%q]", src, found, want)
	}
}

// TestFindUnicodeEscapes_CodePointFormFails covers the ES6 \u{...} code
// point escape form, which is just as rejected on-device as the 4-hex-digit
// form.
func TestFindUnicodeEscapes_CodePointFormFails(t *testing.T) {
	src := []byte(`print("` + backslash + `u{2713} done");`)
	want := backslash + `u{2713}`
	found := FindUnicodeEscapes(src)
	if len(found) != 1 || found[0] != want {
		t.Fatalf("FindUnicodeEscapes(%q) = %v, want [%q]", src, found, want)
	}
}

// TestFindUnicodeEscapes_EscapedBackslashThenLiteralUPasses is the false
// positive this check must avoid: a JS string containing an escaped
// backslash character immediately followed by the ordinary characters u,
// 2, 7, 1, 3 is not a unicode escape sequence at all, and must pass.
func TestFindUnicodeEscapes_EscapedBackslashThenLiteralUPasses(t *testing.T) {
	src := []byte(`var x = "` + doubledBackslashThenLiteralU("2713") + `";`)
	if found := FindUnicodeEscapes(src); len(found) != 0 {
		t.Fatalf("escaped-backslash-then-literal-u must not be flagged, got %v", found)
	}
}

// TestFindUnicodeEscapes_OddBackslashRunStillFlags checks the boundary the
// escaped-backslash logic hinges on: three consecutive backslashes is one
// escaped-backslash pair plus one active, unpaired backslash immediately
// before the letter u -- still a real escape sequence, and must be
// flagged exactly like a single backslash would be.
func TestFindUnicodeEscapes_OddBackslashRunStillFlags(t *testing.T) {
	src := []byte(`var x = "` + backslash + doubledBackslashThenLiteralU("2713") + `";`)
	if found := FindUnicodeEscapes(src); len(found) == 0 {
		t.Fatal("an odd (3) backslash run immediately before u2713 must still be flagged as an active escape")
	}
}

// TestFindUnicodeEscapes_IgnoresComments verifies comment text is masked
// out before scanning: the device's tokenizer never re-lexes comment
// content for escapes, so mentioning the escape syntax in a doc comment
// must not trip the check.
func TestFindUnicodeEscapes_IgnoresComments(t *testing.T) {
	src := []byte("// see " + escapeSeq("2713") + " for details\nvar x = 1;\n")
	if found := FindUnicodeEscapes(src); len(found) != 0 {
		t.Fatalf("an escape mentioned only in a comment must not be flagged, got %v", found)
	}
}

// TestFindUnicodeEscapes_ScansStringContents is the core distinction this
// package draws relative to FindES6Syntax: the device error fires
// wherever the escape appears in source, INCLUDING inside a string
// literal -- that is exactly where pool-pump.js's checkmark character
// was. Unlike FindES6Syntax, this function must NOT mask string contents
// before scanning.
func TestFindUnicodeEscapes_ScansStringContents(t *testing.T) {
	src := []byte(`print('` + escapeSeq("2713") + `');`)
	if found := FindUnicodeEscapes(src); len(found) == 0 {
		t.Fatal("expected an escape sequence inside a string literal to be flagged")
	}
}

// TestRunWithDeviceState_RejectsLiteralEscape verifies the check is wired
// into the emulator's Run path: a script whose source contains a literal
// unicode escape must fail immediately, with an error naming the real
// device's error message -- so any test that runs a script through this
// emulator catches the bug class instead of only discovering it on real
// hardware (which is exactly what happened with pool-pump.js and issue
// this package's Charset fix addressed: the whole Go test suite stayed
// green while a Plus1 crashed on boot).
func TestRunWithDeviceState_RejectsLiteralEscape(t *testing.T) {
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	src := []byte(`print("` + escapeSeq("2713") + ` All initialization steps complete");`)

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		2*time.Second,
	)
	defer cancel()

	deviceState := &DeviceState{
		KVS:     make(map[string]interface{}),
		Storage: make(map[string]interface{}),
	}
	err := RunWithDeviceState(ctx, "escape-test.js", src, false, deviceState)
	if err == nil {
		t.Fatal("expected RunWithDeviceState to reject a script containing a literal unicode escape")
	}
	if !strings.Contains(err.Error(), uXXXXDeviceError) {
		t.Fatalf("error should name the real device error message %q, got: %v", uXXXXDeviceError, err)
	}
}

// TestRunWithDeviceState_AcceptsRawNonASCII is the emulator-level
// regression-guard companion to TestFindUnicodeEscapes_RawNonASCIIPasses:
// a script containing raw, un-escaped non-ASCII characters must run
// normally, exactly as pool-pump.js does on real hardware today.
func TestRunWithDeviceState_AcceptsRawNonASCII(t *testing.T) {
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	src := []byte(`print("✓ All initialization steps complete — script is now running");`)

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		2*time.Second,
	)
	defer cancel()

	deviceState := &DeviceState{
		KVS:     make(map[string]interface{}),
		Storage: make(map[string]interface{}),
	}
	err := RunWithDeviceState(ctx, "raw-nonascii-test.js", src, false, deviceState)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("script with raw non-ASCII bytes should run cleanly, got: %v", err)
	}
}

// TestMinify_NormalizesEscapeToRawCharacter documents an important fact
// discovered while wiring the device-fidelity guard into Minify: neither
// minifier available today (tdewolff, the default; esbuild, via
// minify_esbuild.go's Charset: api.CharsetUTF8) passes an existing
// backslash-u escape sequence through unchanged -- both NORMALIZE it into
// the equivalent raw UTF-8 character in their output. So a hand-authored
// escape in the INPUT does not currently reach Minify's output-validation
// step at all; this test pins that behavior down (and doubles as a
// regression guard: output must be clear of escapes AND must contain the
// raw character, not just "no error").
func TestMinify_NormalizesEscapeToRawCharacter(t *testing.T) {
	src := []byte(`function f(m){print(m);} f("` + escapeSeq("2713") + ` done");`)
	out, err := Minify(src)
	if err != nil {
		t.Fatalf("Minify should succeed (the minifier normalizes the escape away): %v", err)
	}
	if found := FindUnicodeEscapes(out); len(found) != 0 {
		t.Fatalf("minified output must not contain escape sequences, got %v:\n%s", found, out)
	}
	if !strings.ContainsRune(string(out), '✓') {
		t.Fatalf("minified output should contain the raw checkmark character, got:\n%s", out)
	}
}

// TestRejectUnicodeEscapes_CatchesSyntheticMinifierOutput unit-tests the
// exact validation Minify/MinifyWithOptions run after minifying (see
// rejectUnicodeEscapes in es5check.go), using synthetic "minifier output"
// that already contains an escape. This is necessary because -- as
// TestMinify_NormalizesEscapeToRawCharacter shows -- neither minifier
// available today can actually be coaxed into emitting escaped output, so
// this is the only way to prove the OUTPUT-validation code path itself
// works and would catch a future minifier engine/config regression (e.g.
// esbuild's default ASCII charset, before the Charset: api.CharsetUTF8
// fix in minify_esbuild.go, did exactly this).
func TestRejectUnicodeEscapes_CatchesSyntheticMinifierOutput(t *testing.T) {
	fakeMinifiedOutput := []byte(`function f(m){print(m)}f("` + escapeSeq("2713") + ` done")`)
	err := rejectUnicodeEscapes("minify test.js", fakeMinifiedOutput)
	if err == nil {
		t.Fatal("expected rejectUnicodeEscapes to reject output containing a literal unicode escape")
	}
	if !strings.Contains(err.Error(), uXXXXDeviceError) {
		t.Fatalf("error should name the real device error message %q, got: %v", uXXXXDeviceError, err)
	}
}

// TestMinifyWithOptions_PreservesRawNonASCII confirms the fix this whole
// package's Charset: api.CharsetUTF8 setting exists for: esbuild must emit
// raw UTF-8 for non-ASCII characters, not escape them, and that output
// must pass this device-fidelity guard.
func TestMinifyWithOptions_PreservesRawNonASCII(t *testing.T) {
	src := []byte(`function f(m){print(m);} f("✓ done — ok");`)
	res, err := MinifyWithOptions("test.js", src, MinifyOptions{Engine: EngineEsbuild})
	if err != nil {
		t.Fatalf("MinifyWithOptions(esbuild) on raw non-ASCII input should succeed, got: %v", err)
	}
	if found := FindUnicodeEscapes(res.Code); len(found) != 0 {
		t.Fatalf("esbuild output for raw non-ASCII input must not contain escape sequences, got %v:\n%s", found, res.Code)
	}
	if !strings.ContainsRune(string(res.Code), '✓') {
		t.Fatalf("esbuild output should preserve the raw checkmark character, got:\n%s", res.Code)
	}
}
