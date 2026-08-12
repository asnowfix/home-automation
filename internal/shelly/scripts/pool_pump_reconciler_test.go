package scripts

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
)

// === #476: the level-triggered reconciler ===
//
// pool-pump.js is now three layers: facts (one scalar per observed fact, one
// writer each), a pure desiredOutput() returning an output id / -1 off / -2
// "no opinion", and a single applyOutput()/applyDone() pair that is the only
// code allowed to call Switch.Set on the pump. Every entry point is "write
// the fact, call reconcile(), return".
//
// The tests below pin the properties that arrangement exists to guarantee.

// TestPoolPump_OnlyOneActuator asserts the structural invariant by grep, which
// is the only way to assert it: a second call site would be a latent #450, and
// no black-box test can see one that has not fired yet.
//
// Switch.Set may appear only inside the low-level helpers (setOutput,
// turnOffAllSwitchesNext), and those helpers may only be called from the
// actuator. Everything else must go through applyOutput().
func TestPoolPump_OnlyOneActuator(t *testing.T) {
	src, err := os.ReadFile(poolPumpScriptPath)
	if err != nil {
		t.Fatalf("read pool-pump.js: %v", err)
	}
	code := stripJSComments(string(src))

	// The three primitives that move a pump output.
	for _, prim := range []string{"setOutput(", "turnOffAllSwitches(", "turnOffOtherOutputs("} {
		for _, fn := range callersOf(t, code, prim) {
			switch fn {
			case "applyOutput", "applyOutputOn",
				// definitions and internal recursion of the helpers themselves
				"setOutput", "turnOffAllSwitches", "turnOffAllSwitchesNext",
				"turnOffOtherOutputs", "turnOffOtherOutputsNext":
			default:
				t.Errorf("%s is called from %s(): only the single actuator "+
					"(applyOutput/applyOutputOn) may drive a pump output — a second "+
					"call site is how #450 happened", prim, fn)
			}
		}
	}
}

// TestPoolPump_NoClosureAllocatedPerReconcile asserts the allocation
// invariant: #421 measured a single per-call closure at ~1050 bytes of
// mem_peak on a Pro1, so the reconciler's hot path must pass named function
// references to queueTask, never freshly built anonymous functions.
func TestPoolPump_NoClosureAllocatedPerReconcile(t *testing.T) {
	src, err := os.ReadFile(poolPumpScriptPath)
	if err != nil {
		t.Fatalf("read pool-pump.js: %v", err)
	}
	code := stripJSComments(string(src))

	for _, fn := range []string{"reconcile", "reconcileNow", "applyOutput", "applyDone", "applyOutputOn"} {
		body := functionBody(t, code, fn)
		if body == "" {
			t.Fatalf("function %s() not found in pool-pump.js", fn)
		}
		if strings.Contains(body, "queueTask(function") || strings.Contains(body, "Shelly.call") && strings.Contains(body, ", function") {
			t.Errorf("%s() allocates an anonymous function on the reconcile path; "+
				"pass a named function reference instead (#421: ~1050 bytes of mem_peak per closure)", fn)
		}
	}
}

// poolPumpWindowNow builds a Pro1 device whose winter night jobs bracket
// "now", so the policy wants the pump running from the moment the window fact
// is seeded. Winter is the mode a device with no forecast falls back to, and
// the night pair is the one readWindow() reads in that mode.
func poolPumpWindowNow(t *testing.T, offsetStart, offsetStop time.Duration, switchOn bool) *script.DeviceState {
	t.Helper()
	now := time.Now()
	start := now.Add(offsetStart)
	stop := now.Add(offsetStop)

	job := func(id int, code, timespec string, enable bool) map[string]interface{} {
		return map[string]interface{}{
			"id": id, "enable": enable, "timespec": timespec,
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": code},
			}},
		}
	}
	cs := pro1ComponentStatus()
	if switchOn {
		cs["switch:0"] = map[string]interface{}{"id": 0, "output": true}
	}
	return &script.DeviceState{
		KVS:             pro1KVS(),
		Storage:         map[string]interface{}{"schedule-mode": "winter"},
		ComponentStatus: cs,
		Schedules: []map[string]interface{}{
			job(1, "handleNightStart()", poolPumpTimespec(start.Hour(), start.Minute()), true),
			job(2, "handleNightStop()", poolPumpTimespec(stop.Hour(), stop.Minute()), true),
		},
	}
}

// runPoolPump starts the script and waits for init. The caller must call the
// returned stop().
func runPoolPump(t *testing.T, d *script.DeviceState) (stop func()) {
	t.Helper()
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	buf := readPoolPumpScript(t)
	ctx, cancel := poolPumpRunContext(t)
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, d)
	}()

	if !waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, ok := d.KVSValue("script/pool-pump/schedule-mode")
		return ok
	}) {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}
	return func() {
		cancel()
		<-done
	}
}

func waitActiveOutput(t *testing.T, d *script.DeviceState, want string) bool {
	t.Helper()
	return waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := d.KVSValue("script/pool-pump/active-output")
		return ok && v == want
	})
}

// TestPoolPump_ReconcilerStartsInsideWindowWithoutAnyEvent is the #441 shape:
// the pump is off, the run window contains "now", and no schedule job is due
// to fire (the start instant is already in the past). Edge-triggered control
// left the pump off for the whole day; level-triggered control starts it
// because the answer to "should I be running?" is asked continuously.
func TestPoolPump_ReconcilerStartsInsideWindowWithoutAnyEvent(t *testing.T) {
	d := poolPumpWindowNow(t, -1*time.Hour, 1*time.Hour, false)
	stop := runPoolPump(t, d)
	defer stop()

	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("inside the run window with the pump off, the reconciler must start it; active-output = %v",
			kvsValue(d, "script/pool-pump/active-output"))
	}
}

// TestPoolPump_ReconcilerStopsOutsideWindowAfterRestart is the deliberate
// behaviour change recorded in #476: a restart re-derives the relay from the
// policy instead of adopting whatever it finds. A pump left running outside
// its window is stopped.
func TestPoolPump_ReconcilerStopsOutsideWindowAfterRestart(t *testing.T) {
	d := poolPumpWindowNow(t, -5*time.Hour, -4*time.Hour, true)
	stop := runPoolPump(t, d)
	defer stop()

	if !waitActiveOutput(t, d, "-1") {
		t.Fatalf("outside the run window with the pump running, the reconciler must stop it; active-output = %v",
			kvsValue(d, "script/pool-pump/active-output"))
	}
}

// TestPoolPump_UnknownWindowMeansNoAction pins the -2 path. pro1Schedules()
// carries a night START and no night STOP, so the window is unresolvable. A
// two-valued desired state would read that as "off" and stop a running pump
// because a schedule read came back incomplete (#441/#436). It must do
// nothing at all.
func TestPoolPump_UnknownWindowMeansNoAction(t *testing.T) {
	cs := pro1ComponentStatus()
	cs["switch:0"] = map[string]interface{}{"id": 0, "output": true}
	d := &script.DeviceState{
		KVS:             pro1KVS(),
		Storage:         map[string]interface{}{"schedule-mode": "winter"},
		ComponentStatus: cs,
		Schedules:       pro1Schedules(), // start only, no stop -> window unknown
	}
	stop := runPoolPump(t, d)
	defer stop()

	// Give the reconciler ample opportunity to do the wrong thing.
	settlePoolPumpTaskQueue(t)
	settlePoolPumpTaskQueue(t)

	if v := kvsValue(d, "script/pool-pump/active-output"); v != nil && v != "0" {
		t.Fatalf("window unresolvable: the policy must have no opinion and leave the relay alone, "+
			"but active-output settled on %v", v)
	}
	entry, ok := componentStatusValue(d, "switch:0")
	if !ok {
		t.Fatalf("switch:0 status missing")
	}
	m, _ := entry.(map[string]interface{})
	if on, _ := m["output"].(bool); !on {
		t.Fatalf("window unresolvable: the pump was switched OFF on a guess — " +
			"desiredOutput() must return -2, not -1, when the window cannot be resolved")
	}
}

// TestPoolPump_ManualOverrideSurvivesOutsideWindow: a button press outside the
// run window must hold, or the reconciler would undo it within one 200ms tick
// — the cost the study flagged and the reason override-ms exists.
func TestPoolPump_ManualOverrideSurvivesOutsideWindow(t *testing.T) {
	d := poolPumpWindowNow(t, -5*time.Hour, -4*time.Hour, false)
	injector := make(chan []byte, 4)
	d.EventInjector = injector
	stop := runPoolPump(t, d)
	defer stop()

	injector <- shellyButtonEvent()
	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("button press outside the window did not start the pump; active-output = %v",
			kvsValue(d, "script/pool-pump/active-output"))
	}

	// Several task-queue ticks later it must still be on: the policy wants
	// -1 (outside the window) and only the override is holding it.
	settlePoolPumpTaskQueue(t)
	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "0" {
		t.Fatalf("the reconciler reverted a manual override within %v; active-output = %v — "+
			"override-ms must hold it", 4*time.Second, v)
	}
}

// TestPoolPump_ScheduleEdgeClearsManualOverride: an override expires at
// override-ms OR at the next schedule edge, whichever comes first.
func TestPoolPump_ScheduleEdgeClearsManualOverride(t *testing.T) {
	d := poolPumpWindowNow(t, -5*time.Hour, -4*time.Hour, false)
	injector := make(chan []byte, 4)
	d.EventInjector = injector
	stop := runPoolPump(t, d)
	defer stop()

	injector <- shellyButtonEvent()
	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("setup: button press did not start the pump")
	}

	// The evening-stop edge fires. It clears the override; the policy then
	// says -1 because "now" is outside the window.
	d.SetKVSValue("test/pool/edge", "fired")
	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("could not fire the schedule edge: %v", err)
	}
	if !waitActiveOutput(t, d, "-1") {
		t.Fatalf("a schedule edge must clear the manual override and let the policy reassert; "+
			"active-output = %v", kvsValue(d, "script/pool-pump/active-output"))
	}
}

// TestPoolPump_FuseRefusesButtonOnAfterRapidCycling: routing the button
// through the single actuator means button-driven runs finally record fuse
// changes. Before #476 they did not, so a human could cycle the relay
// arbitrarily fast and the anti-cycling protection never saw it.
func TestPoolPump_FuseRefusesButtonOnAfterRapidCycling(t *testing.T) {
	d := poolPumpWindowNow(t, -5*time.Hour, -4*time.Hour, false)
	injector := make(chan []byte, 16)
	d.EventInjector = injector
	stop := runPoolPump(t, d)
	defer stop()

	// FUSE_MAX_CHANGES is 4 in a 2-minute window. Four presses = four relay
	// changes; the fifth ON must be refused.
	want := []string{"0", "-1", "0", "-1"}
	for i, w := range want {
		injector <- shellyButtonEvent()
		if !waitActiveOutput(t, d, w) {
			t.Fatalf("press %d: expected active-output=%s, got %v", i+1, w,
				kvsValue(d, "script/pool-pump/active-output"))
		}
	}

	injector <- shellyButtonEvent()
	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("the 5th rapid button press must be refused by the anti-cycling fuse, "+
			"but active-output = %v — button presses are bypassing the fuse again (#475 defect 1)", v)
	}
}

// TestPoolPump_NestedEventDuringActuationProducesOneChain is the #450 shape
// generalised: a schedule edge arriving INSIDE an in-flight water-supply
// protect must not start a second Switch.Set chain. SetPendingNestedEvent is
// the only way to reproduce the real device's nesting (see the long comment
// on the flap tests).
func TestPoolPump_NestedEventDuringActuationProducesOneChain(t *testing.T) {
	d := poolPumpWindowNow(t, -1*time.Hour, 1*time.Hour, false)
	injector := make(chan []byte, 8)
	d.EventInjector = injector
	stop := runPoolPump(t, d)
	defer stop()

	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("setup: the window contains now, the pump should be running")
	}

	// While the protect's Switch.Set is in flight, deliver a switch:0 event
	// nested inside it — the shape that overflowed the interpreter stack.
	d.SetPendingNestedEvent(shellySwitchEvent(0, false))
	injector <- shellyInputEvent(0, true)

	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("water-supply protection with a nested switch event must settle OFF, got %v", v)
	}
	assertSwitchOutput(t, d, false, "nested event during actuation")
}

// shellySwitchEvent builds the raw device event a switch:N state change emits.
func shellySwitchEvent(id int, state bool) []byte {
	return mustJSON(map[string]interface{}{
		"info": map[string]interface{}{
			"component": "switch:" + itoa(id),
			"id":        id,
			"state":     state,
		},
	})
}

// === small helpers ===

var jsLineComment = regexp.MustCompile(`(?m)//.*$`)

func stripJSComments(s string) string {
	return jsLineComment.ReplaceAllString(s, "")
}

// functionBody returns the source of the named top-level function, brace
// matched, or "" if it is not present.
func functionBody(t *testing.T, code, name string) string {
	t.Helper()
	idx := strings.Index(code, "\nfunction "+name+"(")
	if idx < 0 {
		return ""
	}
	open := strings.Index(code[idx:], "{")
	if open < 0 {
		return ""
	}
	depth := 0
	for i := idx + open; i < len(code); i++ {
		switch code[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return code[idx : i+1]
			}
		}
	}
	return ""
}

// callersOf returns the names of the top-level functions whose bodies contain
// the given call token.
func callersOf(t *testing.T, code, token string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^function ([A-Za-z0-9_]+)\(`).FindAllStringSubmatchIndex(code, -1) {
		name := code[m[2]:m[3]]
		body := functionBody(t, code, name)
		if body != "" && strings.Contains(body, token) {
			out = append(out, name)
		}
	}
	return out
}
