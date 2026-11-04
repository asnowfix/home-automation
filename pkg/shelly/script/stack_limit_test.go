package script

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

const recursionPrelude = `function rec(n) { return n === 0 ? 0 : 1 + rec(n - 1); }`

func runStackScript(t *testing.T, limit int, code string) (int64, error) {
	t.Helper()
	ResetStackOverflows()
	ctx, cancel := context.WithTimeout(logr.NewContext(context.Background(), testr.New(t)), 500*time.Millisecond)
	defer cancel()
	ctx = mqtt.NewContextWithClient(ctx, mqtt.NewMockClient())
	ds := &DeviceState{
		KVS:              map[string]interface{}{},
		Storage:          map[string]interface{}{},
		MaxCallStackSize: limit,
	}
	err := RunWithDeviceState(ctx, "t.js", []byte(recursionPrelude+code), false, ds)
	return StackOverflows(), err
}

// Off by default: 0 means unlimited, deep recursion is fine.
func TestMaxCallStackSizeOffByDefault(t *testing.T) {
	n, err := runStackScript(t, 0, `rec(500);`)
	if (err != nil && err != context.DeadlineExceeded) || n != 0 {
		t.Fatalf("limit 0 must not trip: err=%v overflows=%d", err, n)
	}
}

// The limit trips on recursion, and a JS try/catch does NOT intercept it:
// the script run aborts with a goja.StackOverflowError (fact (a) of #496).
func TestMaxCallStackSizeTripsAndIsNotCatchable(t *testing.T) {
	n, err := runStackScript(t, 50, `
var caught = false;
try { rec(500); } catch (e) { caught = true; }
Shelly.call("KVS.Set", {key: "after-catch", value: "reached"});`)
	if err == nil || n != 1 {
		t.Fatalf("limit 50 must abort the script: err=%v overflows=%d", err, n)
	}
}

// An overflow inside a Go-invoked callback is swallowed by the emulator (the
// callable's error is discarded), so only the StackOverflows counter reveals
// it (fact (b) of #496).
func TestMaxCallStackSizeOverflowInCallbackIsRecorded(t *testing.T) {
	n, err := runStackScript(t, 50, `
Shelly.call("KVS.Get", {key: "x"}, function () { rec(500); });`)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("callback overflow should not fail the run (it is swallowed): %v", err)
	}
	if n != 1 {
		t.Fatalf("callback overflow not recorded: overflows=%d", n)
	}
}
