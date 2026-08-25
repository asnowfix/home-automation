package scripts

import (
	"encoding/json"
	"fmt"
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
// the night pair is the one onWindowJobs() resolves in that mode.
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
	d.ScheduleEvalInjector = make(chan []byte, 4)
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

// TestPoolPump_ButtonChangesDoNotAccumulateInFuseWindow supersedes what used
// to be TestPoolPump_FuseRefusesButtonOnAfterRapidCycling. #476 routed the
// button through the single actuator, which made button-driven runs record
// fuse changes for the first time — closing #475 defect 1 (a human could
// cycle the relay arbitrarily fast and the fuse never saw it) but opening
// #549: overnight evidence on filtration-hiver during a real backwash showed
// the fuse and the button's own override FIGHTING each other — override
// wants ON, fuse forces OFF, cooldown expires, override still wants ON,
// repeat — producing exactly the cycling the fuse exists to prevent. Per the
// maintainer's explicit decision ("manual button push does not count against
// the fuse"), a button-driven transition must never accumulate in the fuse's
// window at all, so there is nothing left for the fuse to ever act on from
// button activity alone.
func TestPoolPump_ButtonChangesDoNotAccumulateInFuseWindow(t *testing.T) {
	d := poolPumpWindowNow(t, -5*time.Hour, -4*time.Hour, false)
	injector := make(chan []byte, 16)
	d.EventInjector = injector
	stop := runPoolPump(t, d)
	defer stop()

	// FUSE_MAX_CHANGES is 4 in a 2-minute window. Six rapid presses — more
	// than any other source is ever allowed — must ALL take effect: #549
	// requires that a button-driven change never counts toward the fuse's
	// window, so the fuse has nothing to trip on here.
	want := []string{"0", "-1", "0", "-1", "0", "-1"}
	for i, w := range want {
		injector <- shellyButtonEvent()
		if !waitActiveOutput(t, d, w) {
			t.Fatalf("press %d: expected active-output=%s, got %v — a button press must never be "+
				"refused by the fuse (#549), even after more presses than FUSE_MAX_CHANGES", i+1, w,
				kvsValue(d, "script/pool-pump/active-output"))
		}
	}
	if got := d.EmittedEventCount("pool.fuse_tripped"); got != 0 {
		t.Fatalf("expected the fuse to never trip from button-only activity (#549: a button-driven "+
			"change must not count toward the fuse's window), got %d pool.fuse_tripped events", got)
	}
}

// TestPoolPump_ButtonBypassesFuseRefusalAfterTrip is the #549 hardware trace,
// reproduced deterministically: the fuse trips from NON-button activity (a
// rapid sequence of schedule-driven window changes, standing in for the
// solar/schedule churn that can legitimately trip it), and the very next
// event is a physical button press wanting the pump ON. Before #549 this was
// refused — worse, the override held and re-fought the fuse every time the
// cooldown expired (the overnight 21:51-22:06 trace on filtration-hiver).
// Per the maintainer's decision, the button must win outright: F_WATER (not
// exercised here — see TestPoolPump_WaterInterlockRefusesButtonPress) is the
// only fact allowed to override a manual press.
func TestPoolPump_ButtonBypassesFuseRefusalAfterTrip(t *testing.T) {
	d := poolPumpWindowNow(t, -1*time.Hour, 1*time.Hour, false) // window contains "now": pump starts on its own
	d.ScheduleEvalInjector = make(chan []byte, 8)
	buttonInjector := make(chan []byte, 4)
	d.EventInjector = buttonInjector
	stop := runPoolPump(t, d)
	defer stop()

	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("setup: window contains now, pump should have started on its own")
	}

	// Three more schedule-driven window moves — four relay changes in total
	// counting the automatic start above — prime the fuse without a single
	// button press. setWindow() is the window fact's one writer (#476) and
	// calls reconcile() itself, so each move drives a real reconciler-path
	// actuation (through fuseAllowOn(), unlike a direct applyOutput() call).
	now := time.Now()
	nowMin := now.Hour()*60 + now.Minute()
	outStart := (nowMin + 120) % 1440
	outStop := (outStart + 1) % 1440
	inStart := (nowMin - 30 + 1440) % 1440
	inStop := (nowMin + 30) % 1440
	moves := []struct {
		startMin, stopMin int
		want              string
	}{
		{outStart, outStop, "-1"}, // change #2: leaves the window, stops
		{inStart, inStop, "0"},    // change #3: re-enters, starts
		{outStart, outStop, "-1"}, // change #4: leaves again, stops
	}
	for i, mv := range moves {
		if err := evalPoolPumpSchedule(d, fmt.Sprintf("setWindow(%d, %d)", mv.startMin, mv.stopMin)); err != nil {
			t.Fatalf("move %d: could not move the schedule window: %v", i+2, err)
		}
		if !waitActiveOutput(t, d, mv.want) {
			t.Fatalf("move %d: expected active-output=%s, got %v", i+2, mv.want,
				kvsValue(d, "script/pool-pump/active-output"))
		}
	}

	// The fuse now holds 4 changes in its 2-minute window and the pump is
	// off. A physical button press (Pro1: off -> 0) must be applied
	// regardless — the very act of evaluating it is what trips the fuse
	// (fuseAllowOn() is only ever consulted on an ON attempt), and #549
	// requires the button win that race, not lose it.
	buttonInjector <- shellyButtonEvent()
	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("a physical button press must never be refused by the anti-cycling fuse (#549), "+
			"even immediately after 4 non-button changes tripped it; active-output = %v",
			kvsValue(d, "script/pool-pump/active-output"))
	}
	if got := d.EmittedEventCount("pool.fuse_tripped"); got < 1 {
		t.Fatalf("setup invariant broken: expected the fuse to have actually tripped from the "+
			"non-button changes, got %d pool.fuse_tripped events — this test is not exercising "+
			"the refused case", got)
	}
}

// TestPoolPump_ScheduleDrivenChangesStillTripFuse is the fuse's control: #549
// changes ONLY the button's relationship with the fuse. A rapid sequence of
// schedule-driven changes (standing in for schedule/solar churn — no button
// anywhere in this test) must still trip the fuse and still be refused,
// exactly as before.
func TestPoolPump_ScheduleDrivenChangesStillTripFuse(t *testing.T) {
	d := poolPumpWindowNow(t, -1*time.Hour, 1*time.Hour, false) // window contains "now": pump starts on its own
	d.ScheduleEvalInjector = make(chan []byte, 8)
	stop := runPoolPump(t, d)
	defer stop()

	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("setup: window contains now, pump should have started on its own")
	}

	now := time.Now()
	nowMin := now.Hour()*60 + now.Minute()
	outStart := (nowMin + 120) % 1440
	outStop := (outStart + 1) % 1440
	inStart := (nowMin - 30 + 1440) % 1440
	inStop := (nowMin + 30) % 1440
	moves := []struct {
		startMin, stopMin int
		want              string
	}{
		{outStart, outStop, "-1"}, // change #2
		{inStart, inStop, "0"},    // change #3
		{outStart, outStop, "-1"}, // change #4
	}
	for i, mv := range moves {
		if err := evalPoolPumpSchedule(d, fmt.Sprintf("setWindow(%d, %d)", mv.startMin, mv.stopMin)); err != nil {
			t.Fatalf("move %d: could not move the schedule window: %v", i+2, err)
		}
		if !waitActiveOutput(t, d, mv.want) {
			t.Fatalf("move %d: expected active-output=%s, got %v", i+2, mv.want,
				kvsValue(d, "script/pool-pump/active-output"))
		}
	}

	// A 5th, still non-button, ON attempt must be refused: 4 changes already
	// sit in the fuse's 2-minute window and nothing here is a button press.
	if err := evalPoolPumpSchedule(d, fmt.Sprintf("setWindow(%d, %d)", inStart, inStop)); err != nil {
		t.Fatalf("could not move the schedule window back inside: %v", err)
	}
	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("the 5th non-button ON attempt must be refused by the anti-cycling fuse, but "+
			"active-output = %v — #549 must not weaken the fuse for anything but a button press", v)
	}
	if got := d.EmittedEventCount("pool.fuse_tripped"); got < 1 {
		t.Fatalf("expected at least one pool.fuse_tripped event once the 5th non-button ON was "+
			"attempted, got %d", got)
	}
}

// TestPoolPump_WaterInterlockRefusesButtonPress pins the one exception #549
// explicitly preserves: "the only thing that may override a manual press is
// the water-supply interlock." desiredOutput()'s first line,
// `if (F_WATER) return -1;`, is unconditional and ahead of the override
// branch — untouched by this fix — so a button press while water protection
// is active must still be ignored outright, not merely "refused by the
// fuse".
func TestPoolPump_WaterInterlockRefusesButtonPress(t *testing.T) {
	d := poolPumpWindowNow(t, -5*time.Hour, -4*time.Hour, false)
	d.ComponentStatus["input:0"] = map[string]interface{}{"id": 0, "state": true} // water supply active from boot
	injector := make(chan []byte, 4)
	d.EventInjector = injector
	stop := runPoolPump(t, d)
	defer stop()

	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != nil && v != "-1" {
		t.Fatalf("setup: expected active-output=-1 with water supply already active, got %v", v)
	}

	injector <- shellyButtonEvent()
	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("a button press during water-supply protection must still be refused; "+
			"active-output = %v — #549's fuse exemption must not weaken the water interlock", v)
	}
}

// TestPoolPump_NestedEventDuringActuationProducesOneChain is the #450 shape
// generalised: a schedule edge arriving INSIDE an in-flight water-supply
// protect must not start a second Switch.Set chain. SetPendingNestedEvent is
// the only way to reproduce the real device's nesting (see the long comment
// on the flap tests).
//
// It also pins the fix for the #479-review finding-2 race: handleSwitchEvent()
// writes STATE.activeOutput from the relay's own switch:0 status-change event
// OUTSIDE its RC_BUSY guard, so if that event (the nested one injected below)
// is delivered before applyDone() runs, STATE.activeOutput already holds the
// post-transition value by the time applyDone() used to re-read it there.
// Both edge branches were then skipped: no startRuntimeAccounting()/
// stopRuntimeAccounting() call and no pool.pump_start/pump_stop event, while
// the final active-output value and switch output settle correctly either
// way -- which is exactly why the assertions below, not just settled state,
// are needed to see the bug. applyOutput() now captures the pre-transition
// output into RC_WAS at dispatch time, before any event can race it.
func TestPoolPump_NestedEventDuringActuationProducesOneChain(t *testing.T) {
	d := poolPumpWindowNow(t, -1*time.Hour, 1*time.Hour, false)
	injector := make(chan []byte, 8)
	d.EventInjector = injector
	stop := runPoolPump(t, d)
	defer stop()

	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("setup: the window contains now, the pump should be running")
	}
	if got := d.EmittedEventCount("pool.pump_start"); got != 1 {
		t.Fatalf("setup: expected exactly one pool.pump_start from the initial OFF->ON transition, got %d", got)
	}
	// persistRuntimeState()'s KVS mirror is skipped while STATE.initializing
	// is true (same guard as saveState()), and the initial OFF->ON transition
	// above runs as part of init -- so script/pool-pump/runtime-sec has no
	// real value yet (KVSValue reports it "present" but nil: run.go's kvs.get
	// emulation caches a failed lookup as an explicit nil so repeated reads
	// don't keep mutating the map — see run.go's "Key not found" comment).
	// Confirmed empirically, 2026-08-14: the raw KVSValue() read at this point
	// is (nil, true), not (_, false). Checked here so the >=1 assertion below
	// can only be explained by stopRuntimeAccounting()'s persistRuntimeState()
	// call, not by anything residual from setup.
	if v, ok := d.KVSValue("script/pool-pump/runtime-sec"); ok && v != nil {
		t.Fatalf("setup: expected no real runtime-sec KVS value yet (persistRuntimeState() "+
			"is skipped during init), got %v -- test assumption changed, fix the accounting "+
			"assertion below to compare against this baseline instead of presence", v)
	}

	// Let a full second of runtime accrue before triggering the stop, so
	// that IF stopRuntimeAccounting() runs, runtime-sec deterministically
	// lands on >=1 -- Math.round() of >=1s of elapsed time is never 0.
	// Without this, a stop landing within the same second as the start would
	// round to 0 even with the fix present, making the assertion below flaky
	// rather than a clean bug detector.
	time.Sleep(1100 * time.Millisecond)

	// While the protect's Switch.Set is in flight, deliver a switch:0 event
	// nested inside it — the shape that overflowed the interpreter stack,
	// and (per the #479 finding-2 comment above) the shape that raced
	// applyDone()'s belief about the pre-transition output.
	d.SetPendingNestedEvent(shellySwitchEvent(0, false))
	injector <- shellyInputEvent(0, true)

	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("water-supply protection with a nested switch event must settle OFF, got %v", v)
	}
	assertSwitchOutput(t, d, false, "nested event during actuation")

	// The two assertions the #450 regression test alone could not make: the
	// ON->OFF transition's accounting ran, and its event fired exactly once.
	// Both are silently skipped if the race in finding 2 reintroduces itself,
	// while the two assertions above keep passing regardless.
	if got := d.EmittedEventCount("pool.pump_stop"); got != 1 {
		t.Fatalf("expected exactly one pool.pump_stop from the ON->OFF transition, got %d "+
			"(0 means stopRuntimeAccounting()/the emit were skipped because applyDone() "+
			"believed the transition had already happened -- #479 finding 2)", got)
	}
	// settlePoolPumpTaskQueue() already waited out the 200ms task-queue tick
	// that queueTask()s the KVS mirror writes inside persistRuntimeState(), so
	// this need not poll further.
	secAfterStop := parseKVSInt(t, d, "script/pool-pump/runtime-sec")
	if secAfterStop < 1 {
		t.Fatalf("expected runtime-sec >= 1 once stopRuntimeAccounting() ran after >=1s of "+
			"runtime, got %d -- accounting was skipped (#479 finding 2)", secAfterStop)
	}
}

// TestPoolPump_BreakBeforeMakeOffStepFailureAbortsTransition is the #479
// review finding-4 regression: turnOffOtherOutputsNext() used to ignore each
// off-step's error_code and always proceed to turn the target output on. A
// silent failure there could leave TWO Pro3 stages energized at once —
// exactly what break-before-make exists to prevent, and exactly the failure
// mode applyOutput() as the single trusted actuator (#476) is supposed to
// make impossible: there is nothing downstream of it to catch a bad
// actuation.
//
// applyOutput(1) is invoked directly via evalPoolPumpSchedule rather than
// through a button press: a button-driven, reconciler-retried path keeps
// re-dispatching the same failing transition every 200ms task-queue tick,
// and applyOutput() records a fuse change on every dispatch regardless of
// whether it succeeds — a handful of retries alone (unrelated to this fix)
// trips the anti-cycling fuse and forces the pump off through
// turnOffAllSwitches(), a different code path with its own pre-existing
// error-handling gap (out of scope here; see turnOffOtherOutputsNext's
// sibling turnOffAllSwitchesNext). Calling applyOutput(1) once, directly,
// isolates the one behaviour this fix changes.
func TestPoolPump_BreakBeforeMakeOffStepFailureAbortsTransition(t *testing.T) {
	injector := make(chan []byte, 8)
	d := &script.DeviceState{
		KVS:                  controllerKVS(),
		Storage:              make(map[string]interface{}),
		ComponentStatus:      pro3ComponentStatus(),
		Schedules:            poolPumpSchedules(),
		EventInjector:        injector,
		ScheduleEvalInjector: make(chan []byte, 4),
	}
	stop := runPoolPump(t, d)
	defer stop()

	// Button press: off -> output 0.
	injector <- shellyButtonEvent()
	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("setup: expected active-output=0 after button press, got %v",
			kvsValue(d, "script/pool-pump/active-output"))
	}

	// Arm output 0's off-step to fail, then drive the single actuator
	// directly towards output 1. The break-before-make sequence for target=1
	// tries to turn off outputs 0 and 2 first; output 0's off-command will
	// report a failure.
	d.FailSwitchSet(0, -114, "simulated relay fault")
	if err := evalPoolPumpSchedule(d, "applyOutput(1)"); err != nil {
		t.Fatalf("could not drive applyOutput(1): %v", err)
	}

	settlePoolPumpTaskQueue(t)

	if v := kvsValue(d, "script/pool-pump/active-output"); v != "0" {
		t.Fatalf("a failed break-before-make off-step must leave active-output unchanged "+
			"(belief comes from observation, never intent), got %v", v)
	}
	// The physical outcome, not just the bookkeeping: output 0 must still be
	// on (its off-command failed) and output 1 must NOT have been turned on
	// — the whole point of break-before-make is that both are never
	// simultaneously energized.
	requirePro3SwitchOutput(t, d, 0, true, "after failed off-step: output 0")
	requirePro3SwitchOutput(t, d, 1, false, "after failed off-step: output 1 must not have been energized")
	requirePro3SwitchOutput(t, d, 2, false, "after failed off-step: output 2 unaffected")
}

// requirePro3SwitchOutput fails the test if switchId's physical output does
// not match want. Unlike assertSwitchOutput (hardcoded to switch:0 for the
// Pro1 tests), this takes the switch id, since break-before-make only
// applies to Pro3's multiple outputs.
func requirePro3SwitchOutput(t *testing.T, d *script.DeviceState, switchId int, want bool, context string) {
	t.Helper()
	key := fmt.Sprintf("switch:%d", switchId)
	entry, ok := componentStatusValue(d, key)
	if !ok {
		t.Fatalf("%s: %s component status missing", context, key)
	}
	m, ok := entry.(map[string]interface{})
	if !ok {
		t.Fatalf("%s: %s component status not a map: %#v", context, key, entry)
	}
	on, _ := m["output"].(bool)
	if on != want {
		t.Fatalf("%s: %s physically %v, want %v", context, key, on, want)
	}
}

// evalPoolPumpSchedule fires a Schedule job's code against the running
// script, the same way a real device's Schedule.eval -> Script.Eval(id, code)
// would when the job's timespec comes due. The emulator does not track
// wall-clock time against Schedules (Schedule.List/.Create/.Update in run.go
// are pure storage — see ScheduleEvalInjector's doc comment in
// device_state.go), so tests that need a specific job's handler to run send
// its code here instead of waiting on real time.
//
// The caller must have set d.ScheduleEvalInjector before starting the script
// (runPoolPump reads deviceState.ScheduleEvalInjector when wiring handlers).
// A returned error means the request could not be sent, not that the code
// itself failed to evaluate — exactly like injecting a raw device event.
func evalPoolPumpSchedule(d *script.DeviceState, code string) error {
	if d.ScheduleEvalInjector == nil {
		return fmt.Errorf("device state has no ScheduleEvalInjector; set it before runPoolPump")
	}
	b, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return err
	}
	d.ScheduleEvalInjector <- b
	return nil
}

// shellySwitchEvent builds the raw device event a switch:N state change emits.
func shellySwitchEvent(id int, state bool) []byte {
	event := map[string]interface{}{
		"info": map[string]interface{}{
			"component": fmt.Sprintf("switch:%d", id),
			"id":        id,
			"state":     state,
		},
	}
	data, _ := json.Marshal(event)
	return data
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
