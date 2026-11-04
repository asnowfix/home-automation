package jstarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// mustFilter runs Filter and fails the test on error or on a Device that
// does not parse as valid JavaScript -- every fixture in this file is
// expected to produce runnable JS, so a parse failure here is always a bug
// in Filter, never an expected outcome.
func mustFilter(t *testing.T, src []byte) FilterResult {
	t.Helper()
	res, err := Filter(src)
	if err != nil {
		t.Fatalf("Filter: unexpected error: %v", err)
	}
	if _, err := Regions(res.Device); err != nil {
		t.Fatalf("Filter produced a Device output that does not parse as JavaScript: %v\n--- Device ---\n%s", err, res.Device)
	}
	return res
}

// TestFilter_NoAnnotations_PoolPumpRoundTripsByteIdentical is the single
// most valuable test in #569: internal/shelly/scripts/pool-pump.js is
// ~3000 lines of hand-tuned production source carrying zero annotations,
// and the filter must be provably inert on it -- Device must be exactly
// src, and Daemon must be empty.
func TestFilter_NoAnnotations_PoolPumpRoundTripsByteIdentical(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "pool-pump.js"))
	if err != nil {
		t.Fatalf("reading pool-pump.js: %v", err)
	}
	res, err := Filter(src)
	if err != nil {
		t.Fatalf("Filter(pool-pump.js): unexpected error: %v", err)
	}
	if !bytes.Equal(res.Device, src) {
		t.Fatalf("Filter(pool-pump.js): Device is not byte-identical to src (len src=%d, len Device=%d)", len(src), len(res.Device))
	}
	if len(res.Daemon) != 0 {
		t.Fatalf("Filter(pool-pump.js): Daemon = %d bytes, want empty: %q", len(res.Daemon), res.Daemon)
	}
	if res.Hash != "" {
		t.Fatalf("Filter(pool-pump.js): Hash = %q, want empty (nothing was extracted)", res.Hash)
	}
}

// TestFilter_NoAnnotations_EmptyDaemon repeats the same guarantee against a
// second, much smaller unannotated fixture, so the pool-pump.js case above
// isn't the only evidence for "no annotations -> Device == src, Daemon
// empty".
func TestFilter_NoAnnotations_EmptyDaemon(t *testing.T) {
	src := readTestdata(t, "no_annotations.js")
	res := mustFilter(t, src)
	if !bytes.Equal(res.Device, src) {
		t.Fatalf("Device = %q, want byte-identical to src %q", res.Device, src)
	}
	if len(res.Daemon) != 0 {
		t.Fatalf("Daemon = %q, want empty", res.Daemon)
	}
}

// TestFilter_MiddleObjectProperty is the first of #569's three named
// "awkward shapes": an annotated property in the middle of an object
// literal. Removing it without removing its trailing comma would leave
// "{ a: 1, , c: 3 }" -- a dangling comma and a syntax error.
func TestFilter_MiddleObjectProperty(t *testing.T) {
	src := readTestdata(t, "filter_middle_property.js")
	res := mustFilter(t, src)

	if bytes.Contains(res.Device, []byte("b:")) {
		t.Errorf("Device still contains the removed property: %s", res.Device)
	}
	if bytes.Contains(res.Device, []byte(",,")) || bytes.Contains(res.Device, []byte(", ,")) {
		t.Errorf("Device has a dangling comma: %s", res.Device)
	}
	// trimLeadingIndent removes the two spaces of indentation before the
	// annotation comment along with the rest of the cut, so no
	// whitespace-only line is left behind. The hash header is appended, not
	// prepended, so every surviving line's number matches src exactly --
	// see FilterResult.Device.
	wantDevice := "var config = {\n  a: 1,\n\n\n  c: 3\n};\n" +
		"// @target-hash sha256:" + res.Hash + "\n"
	if string(res.Device) != wantDevice {
		t.Errorf("Device = %q, want %q", res.Device, wantDevice)
	}
	wantDaemon := "// @target-hash sha256:" + res.Hash + "\n\nb: 2\n"
	if string(res.Daemon) != wantDaemon {
		t.Errorf("Daemon = %q, want %q", res.Daemon, wantDaemon)
	}
}

// TestFilter_LastObjectProperty is the mirror case: the annotated property
// is last in the list, so there is no trailing comma to absorb -- Filter
// must instead absorb the leading comma from the property before it.
func TestFilter_LastObjectProperty(t *testing.T) {
	src := readTestdata(t, "filter_last_property.js")
	res := mustFilter(t, src)

	// Unlike the middle-property case, the backward comma search here walks
	// through the newline and indentation between "a: 1," and the
	// annotation comment before it finds the comma -- so that whitespace is
	// consumed along with it, and there is no leftover indentation line.
	wantDevice := "var config = {\n  a: 1\n\n\n};\n" +
		"// @target-hash sha256:" + res.Hash + "\n"
	if string(res.Device) != wantDevice {
		t.Errorf("Device = %q, want %q", res.Device, wantDevice)
	}
}

// TestFilter_AdjacentDaemonProperties exercises mergeCuts's reason for
// existing: two consecutive properties are both "@target daemon", so the
// comma between them is claimed by both the first property's trailing-comma
// extension and (absent merging) the second property's leading-comma
// extension. Filter must still produce valid, comma-clean JS.
func TestFilter_AdjacentDaemonProperties(t *testing.T) {
	src := readTestdata(t, "filter_adjacent_properties.js")
	res := mustFilter(t, src)

	wantDevice := "var config = {\n  a: 1,\n\n\n\n\n  d: 4\n};\n" +
		"// @target-hash sha256:" + res.Hash + "\n"
	if string(res.Device) != wantDevice {
		t.Errorf("Device = %q, want %q", res.Device, wantDevice)
	}
	if bytes.Contains(res.Device, []byte(",,")) || bytes.Contains(res.Device, []byte(", ,")) {
		t.Errorf("Device has a dangling comma: %s", res.Device)
	}
	if !bytes.Contains(res.Daemon, []byte("b: 2")) || !bytes.Contains(res.Daemon, []byte("c: 3")) {
		t.Errorf("Daemon is missing an extracted property: %s", res.Daemon)
	}
}

// TestFilter_MultiDeclaratorVar is #569's second named awkward shape: an
// annotated multi-declarator "var a = 1, b = 2;". Per #568, the whole
// statement is one Region -- there is no way to remove one declarator
// without the other -- so this test's job is to prove the *statement's*
// removal leaves its neighbours syntactically intact (no half-declared
// var survives, nothing before or after it breaks).
func TestFilter_MultiDeclaratorVar(t *testing.T) {
	src := readTestdata(t, "filter_multi_declarator.js")
	res := mustFilter(t, src)

	if bytes.Contains(res.Device, []byte("var a")) {
		t.Errorf("Device still contains the removed declaration: %s", res.Device)
	}
	if !bytes.Contains(res.Device, []byte("var x = 0;")) || !bytes.Contains(res.Device, []byte("var y = 1;")) {
		t.Errorf("Device lost a neighbouring statement: %s", res.Device)
	}
	if !bytes.Contains(res.Daemon, []byte("var a = 1,")) || !bytes.Contains(res.Daemon, []byte("b = 2;")) {
		t.Errorf("Daemon is missing the extracted declaration: %s", res.Daemon)
	}
}

// TestFilter_FunctionBetweenTwoOthers is #569's third named awkward shape:
// an annotated function declaration sandwiched between two others. Both
// neighbours must remain independently callable in Device.
func TestFilter_FunctionBetweenTwoOthers(t *testing.T) {
	src := readTestdata(t, "filter_function_between.js")
	res := mustFilter(t, src)

	if bytes.Contains(res.Device, []byte("function middle")) {
		t.Errorf("Device still contains the removed function: %s", res.Device)
	}
	if !bytes.Contains(res.Device, []byte("function before()")) || !bytes.Contains(res.Device, []byte("function after()")) {
		t.Errorf("Device lost a neighbouring function: %s", res.Device)
	}
	if !bytes.Contains(res.Daemon, []byte("function middle(body)")) {
		t.Errorf("Daemon is missing the extracted function: %s", res.Daemon)
	}
}

// TestFilter_LineNumbersPreserved states and tests #569's line-number
// decision explicitly: every line that survives into Device keeps the same
// line number it had in src. The trailing hash-header line is the only
// line Filter adds, and it is appended after everything else so it cannot
// shift anything above it.
func TestFilter_LineNumbersPreserved(t *testing.T) {
	src := readTestdata(t, "filter_function_between.js")
	res := mustFilter(t, src)

	srcLines := strings.Split(string(src), "\n")
	deviceLines := strings.Split(string(res.Device), "\n")

	afterLineNo := -1
	for i, l := range srcLines {
		if strings.Contains(l, "function after()") {
			afterLineNo = i
		}
	}
	if afterLineNo < 0 {
		t.Fatalf("fixture is missing \"function after()\" -- test is broken, not the code under test")
	}
	if deviceLines[afterLineNo] != srcLines[afterLineNo] {
		t.Errorf("line %d shifted: src=%q device=%q", afterLineNo+1, srcLines[afterLineNo], deviceLines[afterLineNo])
	}

	// Device must have exactly one more line than src: the appended hash
	// header. Every other line count is preserved by construction (each
	// removed span is replaced with the same number of newlines it held).
	if len(deviceLines) != len(srcLines)+1 {
		t.Errorf("Device has %d lines, want %d (src's %d plus one hash-header line)", len(deviceLines), len(srcLines)+1, len(srcLines))
	}
}

// TestFilter_HashAppearsInBothOutputs is #569 property 4: both artifacts of
// one Filter call carry the same content hash of the extracted body, so a
// device/daemon deployment mismatch is detectable rather than inferred from
// misbehaviour.
func TestFilter_HashAppearsInBothOutputs(t *testing.T) {
	src := readTestdata(t, "filter_function_between.js")
	res := mustFilter(t, src)

	if res.Hash == "" {
		t.Fatalf("Hash is empty despite a @target daemon region being present")
	}

	// daemonBody appends one trailing newline after the last (here, only)
	// region's own bytes -- see daemonBody's doc comment.
	sum := sha256.Sum256([]byte("function middle(body) {\n  return body;\n}\n"))
	want := hex.EncodeToString(sum[:])
	if res.Hash != want {
		t.Errorf("Hash = %q, want sha256 of the extracted body = %q", res.Hash, want)
	}

	deviceHash, ok := findHashHeader(res.Device)
	if !ok {
		t.Fatalf("Device carries no hash header: %s", res.Device)
	}
	daemonHash, ok := findHashHeader(res.Daemon)
	if !ok {
		t.Fatalf("Daemon carries no hash header: %s", res.Daemon)
	}
	if deviceHash != res.Hash || daemonHash != res.Hash {
		t.Errorf("hash mismatch: Device=%q Daemon=%q FilterResult.Hash=%q", deviceHash, daemonHash, res.Hash)
	}
}

// TestFilter_HashMismatchIsDetectable proves the skew this hash exists to
// catch is mechanically detectable: filtering two different sources that
// each carry one @target daemon region yields two different hashes, and
// ParseHashHeader recovers each one from its own output.
func TestFilter_HashMismatchIsDetectable(t *testing.T) {
	a := mustFilter(t, readTestdata(t, "filter_function_between.js"))
	b := mustFilter(t, readTestdata(t, "filter_middle_property.js"))

	if a.Hash == b.Hash {
		t.Fatalf("two different extracted bodies hashed identically: %q", a.Hash)
	}

	aHash, ok := findHashHeader(a.Device)
	if !ok || aHash != a.Hash {
		t.Fatalf("a's Device header = %q (ok=%v), want %q", aHash, ok, a.Hash)
	}
	bHash, ok := findHashHeader(b.Daemon)
	if !ok || bHash != b.Hash {
		t.Fatalf("b's Daemon header = %q (ok=%v), want %q", bHash, ok, b.Hash)
	}
	// Simulate deployment skew: a's device output paired with b's daemon
	// hash must be flagged as a mismatch by ordinary equality.
	if aHash == bHash {
		t.Fatalf("a's device hash equals b's daemon hash -- skew would go undetected")
	}
}

// findHashHeader scans out for the "// @target-hash sha256:<hex>" line
// Filter embeds and returns the hash it carries.
func findHashHeader(out []byte) (string, bool) {
	for _, line := range strings.Split(string(out), "\n") {
		if hash, ok := ParseHashHeader(line); ok {
			return hash, true
		}
	}
	return "", false
}

// TestFilter_DeviceOutputSatisfiesEspruinoConstraints exercises the filtered
// fixtures through the real script emulator (pkg/shelly/script), the same
// engine internal/shelly/scripts' own pool-pump.js tests use, proving Device
// is not merely parseable but actually runnable -- and, since none of these
// fixtures' surviving code touches the removed daemon-only function, that it
// behaves identically to running the unfiltered source.
func TestFilter_DeviceOutputSatisfiesEspruinoConstraints(t *testing.T) {
	fixtures := []string{
		"filter_middle_property.js",
		"filter_last_property.js",
		"filter_adjacent_properties.js",
		"filter_multi_declarator.js",
		"filter_function_between.js",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			src := readTestdata(t, name)
			res := mustFilter(t, src)
			runInEmulator(t, name+" (unfiltered)", src)
			runInEmulator(t, name+" (Device output)", res.Device)
		})
	}
}

// runInEmulator runs buf through the same goja-hosted Shelly runtime
// internal/shelly/scripts' pool-pump.js tests use, and fails the test if the
// top-level script evaluation errors. It proves the code is not merely
// syntactically parseable (mustFilter's Regions() check) but actually
// runnable under the same constraints a real Shelly device enforces on the
// JS engine.
//
// script.RunWithDeviceState never returns on its own once the script has
// registered a handler (an MQTT topic subscription, in these fixtures' case
// -- see createShellyRuntime): it runs an event loop until ctx is
// cancelled, exactly like a Shelly device's own JS runtime never "finishes"
// while the script is running. pool_pump_test.go's own tests follow the
// same pattern: run in a goroutine, cancel once satisfied, then read the
// (expected-cancellation) error off the done channel. These fixtures do no
// asynchronous work worth polling for, so a short fixed wait for the
// synchronous top-level evaluation to complete, followed by cancel, is
// enough.
func runInEmulator(t *testing.T, name string, buf []byte) {
	t.Helper()
	ctx, cancel := context.WithCancel(
		mqtt.NewContextWithClient(logr.NewContext(context.Background(), testr.New(t)), mqtt.NewMockClient()),
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, name, buf, false, &script.DeviceState{
			KVS:     make(map[string]interface{}),
			Storage: make(map[string]interface{}),
		})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("script.RunWithDeviceState(%s): %v", name, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("script.RunWithDeviceState(%s): did not exit after cancel", name)
	}
}
