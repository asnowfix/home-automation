package scripts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/script"
)

// === #512: the run window must end up resolved, not only when the forecast
// moves the schedule ===
//
// F_WIN_START / F_WIN_STOP hold the run window in minutes since midnight, -1
// meaning "unknown". While either is negative desiredOutput() returns -2 ("no
// opinion — leave the relay alone"), so every reconcile is a no-op and the
// pump never actuates.
//
// The window has exactly two writers, both routed through onWindowJobs():
//
//	pool-pump.js:2358  verifySchedules()  — init step 4/4, one attempt
//	pool-pump.js:2600  updateScheduleMode() completion, gated `if (windowChanged)`
//
// Which job pair the init attempt reads depends on STATE.scheduleMode at that
// instant. loadState() defaults it to "winter" (:1138) whenever Script.storage
// yields nothing for the schedule-mode key, and in winter it looks for the
// NIGHT jobs — which are disabled on a summer device, so the `!job.enable`
// guard skips them, a/b stay -1, and the "still symbolic" guard at :1708
// returns without calling setWindow(). Nothing retries: the only other writer
// is gated on windowChanged, which is false whenever today's forecast
// recomputes a window the schedule already carries.
//
// Measured on the production pump on 2026-08-17 (issue #512), twice — after
// the 09:03:50 and 22:19 restarts the window read -1/-1 for the rest of the
// day, with correct enabled morning and evening jobs sitting on the device the
// whole time. One hand-call of Shelly.call("Schedule.List", {}, onWindowJobs)
// resolved it instantly. The pool lost its whole 11:32–16:28 filtration
// window.
//
// The emulator (goja) reproduces neither the device's heap nor its stack nor
// its timing, so these tests are a witness for the CONTROL LOGIC only. They
// say nothing about mem_peak or about whether a fix fits in the real
// interpreter.

// poolPumpProbeWindow reads F_WIN_START / F_WIN_STOP out of the running
// script, which is the same thing the maintainer did on hardware with a
// wrapped Script.Eval to produce the evidence in #512:
//
//	{"ws":-1,"we":-1,...}   7½ hours after a restart, with resolvable jobs present
//
// The emulator's ScheduleEvalInjector runs code against the script's global
// scope on the VM's own goroutine, exactly as a due Schedule job's
// Script.Eval(id, code) does, so the probe observes the live values rather
// than a mirror of them. It writes them to a KVS key unique per probe, so a
// later probe can never read an earlier probe's answer.
//
// This deliberately asserts an internal variable. It is the fact the issue is
// about, and the pump-state tests below assert the consequence instead —
// both are needed: the state test cannot run during the hour the window
// excludes, and the probe test cannot see whether the relay actually moved.
func poolPumpProbeWindow(t *testing.T, d *script.DeviceState) (int, int) {
	t.Helper()

	key := fmt.Sprintf("test/pool/window-probe-%d", atomic.AddInt64(&poolPumpProbeSeq, 1))
	code := "Shelly.call('KVS.Set', {key: '" + key + "', value: '' + F_WIN_START + ',' + F_WIN_STOP});"
	if err := evalPoolPumpSchedule(d, code); err != nil {
		t.Fatalf("could not send the window probe: %v", err)
	}

	var raw interface{}
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := d.KVSValue(key)
		if !ok {
			return false
		}
		raw = v
		return true
	}) {
		t.Fatalf("the window probe %q never landed in KVS — the script did not answer "+
			"the injected Script.Eval at all", key)
	}

	s, _ := raw.(string)
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		t.Fatalf("window probe returned %q, want \"start,stop\"", s)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatalf("window probe start %q is not an integer: %v", parts[0], err)
	}
	stop, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("window probe stop %q is not an integer: %v", parts[1], err)
	}
	return start, stop
}

var poolPumpProbeSeq int64

// waitPoolPumpWindowResolved polls the probe until both ends of the window are
// non-negative, or the budget runs out. Polling rather than probing once
// leaves a fix free to resolve the window at init, at the end of the daily
// check, or anywhere in between — the contract is "resolved by the time
// things settle", not "resolved at one named instant".
func waitPoolPumpWindowResolved(t *testing.T, d *script.DeviceState, budget time.Duration) (int, int) {
	t.Helper()

	var start, stop int
	end := time.Now().Add(budget)
	for {
		start, stop = poolPumpProbeWindow(t, d)
		if start >= 0 && stop >= 0 {
			return start, stop
		}
		if !time.Now().Before(end) {
			return start, stop
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func poolPumpHM(min int) string {
	if min < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

// poolPumpJob builds one Schedule.List job the way the fixtures in
// pool_pump_test.go do.
func poolPumpJob(id int, code, timespec string, enable bool) map[string]interface{} {
	return map[string]interface{}{
		"id": id, "enable": enable, "timespec": timespec,
		"calls": []interface{}{map[string]interface{}{
			"method": "script.eval",
			"params": map[string]interface{}{"id": 1, "code": code},
		}},
	}
}

// poolPumpCountingWinterForecastServer is poolPumpWinterForecastServer with a
// request counter. A test whose whole assertion is "nothing moved" needs a
// positive marker that the daily check ran at all, or "nothing moved" is
// indistinguishable from "nothing happened".
func poolPumpCountingWinterForecastServer(t *testing.T) (*httptest.Server, func() int64) {
	t.Helper()

	temps := make([]float64, 24)
	for i := range temps {
		temps[i] = 10 // below temp-threshold=20 -> decideModeFromForecast picks winter
	}
	body, err := json.Marshal(map[string]interface{}{
		"hourly": map[string]interface{}{"temperature_2m": temps},
		"daily": map[string]interface{}{
			"sunrise": []string{"2026-08-07T06:00"},
			"sunset":  []string{"2026-08-07T20:00"},
		},
	})
	if err != nil {
		t.Fatalf("failed to build winter forecast body: %v", err)
	}

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() int64 { return atomic.LoadInt64(&hits) }
}

// poolPumpWinterDefaultSummerDevice is the 2026-08-17 device, exactly.
//
//   - Script.storage carries NO schedule-mode key, so loadState() falls back to
//     its "winter" default — the state the maintainer observed at 22:18, when
//     getItem("schedule-mode") returned null while the device was plainly in
//     summer. (Why the key was unreadable is recorded as unexplained in #512
//     and is deliberately not what this test asserts; it only needs the
//     resulting boot state, which is also what a genuinely first-ever boot
//     produces.)
//   - The schedule is a complete, correct, ENABLED summer pair at 01:00 /
//     23:29, with the night pair disabled.
//   - forecast-url carries the `daily=` suffix ensureForecastUrl() requires,
//     pointed at a local server: without it the script discards the stored URL
//     and falls back to Shelly.DetectLocation plus a REAL Open-Meteo fetch,
//     which would make the test depend on today's weather over the pool.
//   - The forecast recomputes exactly 01:00 / 23:29 (see poolPumpWide* in
//     pool_pump_test.go), so every moves() comparison in updateScheduleMode()
//     is false and windowChanged stays false — the "the forecast did not move
//     the schedule" case, which is every day the weather is stable.
func poolPumpWinterDefaultSummerDevice(t *testing.T, srvURL string, pumpRunning bool) *script.DeviceState {
	t.Helper()

	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, now.Location())
	stop := time.Date(now.Year(), now.Month(), now.Day(), 23, 29, 0, 0, now.Location())

	cs := pro1ComponentStatus()
	if pumpRunning {
		cs["switch:0"] = map[string]interface{}{"id": 0, "output": true}
	}

	return &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"forecast-url": srvURL + "?daily=sunrise,sunset",
		},
		ComponentStatus:      cs,
		Schedules:            poolPumpSummerSchedules(start, stop),
		ScheduleEvalInjector: make(chan []byte, 4),
	}
}

// waitPoolPumpDailyCheckDecidedSummer waits for the marker that the daily
// check ran to completion and updateScheduleMode() committed the mode: the
// script writes schedule-mode to KVS from saveState() on a mode change.
// Without this the tests below would race the daily check and could pass or
// fail on scheduling luck rather than on the defect.
func waitPoolPumpDailyCheckDecidedSummer(t *testing.T, d *script.DeviceState) {
	t.Helper()
	if !waitFor(initTimeout, 100*time.Millisecond, func() bool {
		v, ok := d.KVSValue("script/pool-pump/schedule-mode")
		return ok && v == "summer"
	}) {
		t.Fatalf("the daily check never decided 'summer' (schedule-mode = %v) — this test "+
			"never reached the case it is about; check the forecast fixture",
			kvsValue(d, "script/pool-pump/schedule-mode"))
	}
}

// assertPoolPumpScheduleUntouched pins the precondition the whole issue turns
// on: the daily check must NOT have rewritten the run-window jobs, so
// windowChanged is false and the gated second window write at :2600 cannot
// run. If a fixture drifts and the schedule does move, the test would pass
// for the wrong reason — it would be exercising the working path.
func assertPoolPumpScheduleUntouched(t *testing.T, d *script.DeviceState) {
	t.Helper()
	jobs := d.ScheduleJobs()
	if ts := poolPumpScheduleTimespec(jobs, "handleMorningStart()"); ts != poolPumpTimespec(1, 0) {
		t.Fatalf("fixture drift: the daily check moved the morning-start job to %q, so "+
			"windowChanged was TRUE and this test is no longer exercising #512", ts)
	}
	if ts := poolPumpScheduleTimespec(jobs, "handleEveningStop()"); ts != poolPumpTimespec(23, 29) {
		t.Fatalf("fixture drift: the daily check moved the evening-stop job to %q, so "+
			"windowChanged was TRUE and this test is no longer exercising #512", ts)
	}
}

// TestPoolPump_WindowResolvesAfterNoOpDailyCheck is #512's core case.
//
// A restart at an arbitrary hour boots believing "winter" (no stored
// schedule-mode), so init's single onWindowJobs() attempt looks at the
// disabled night jobs and resolves nothing. The daily check then decides
// "summer" off a hot forecast and recomputes a window the schedule already
// carries, so updateScheduleMode() writes no change, windowChanged stays
// false, and the second window write is skipped.
//
// By the time everything settles the window must nevertheless be resolved:
// the morning/evening jobs on the device are enabled and concrete, and the
// script is now in the mode that reads them. "Resolvable the entire time.
// Nothing ever asked" is the defect.
//
// This test is wall-clock independent on purpose — it asserts the fact, not
// the relay, so it runs at any hour. TestPoolPump_RestartInsideWindowStartsPump
// asserts the consequence.
func TestPoolPump_WindowResolvesAfterNoOpDailyCheck(t *testing.T) {
	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	d := poolPumpWinterDefaultSummerDevice(t, srv.URL, false)

	stopScript := runPoolPump(t, d)
	defer stopScript()

	waitPoolPumpDailyCheckDecidedSummer(t, d)
	assertPoolPumpScheduleUntouched(t, d)

	ws, we := waitPoolPumpWindowResolved(t, d, 12*time.Second)
	if ws != poolPumpWideStartMin || we != poolPumpWideStopMin {
		t.Fatalf("after a restart whose daily check recomputes the window the schedule "+
			"already carries (windowChanged false), the run window must still end up "+
			"resolved from those jobs.\n"+
			"  F_WIN_START/F_WIN_STOP = %d/%d (%s - %s)\n"+
			"  want                   = %d/%d (%s - %s)\n"+
			"The device carries an ENABLED handleMorningStart() at %s and an ENABLED "+
			"handleEveningStop() at %s the whole time; init read the disabled night pair "+
			"instead because loadState() defaulted the mode to winter, and nothing retried. "+
			"While the window is unknown desiredOutput() returns -2 and the pump cannot run "+
			"for the rest of the day (#512).",
			ws, we, poolPumpHM(ws), poolPumpHM(we),
			poolPumpWideStartMin, poolPumpWideStopMin,
			poolPumpHM(poolPumpWideStartMin), poolPumpHM(poolPumpWideStopMin),
			poolPumpHM(poolPumpWideStartMin), poolPumpHM(poolPumpWideStopMin))
	}
}

// TestPoolPump_RestartInsideWindowStartsPump is the consequence that actually
// cost the pool a day: same setup as the core case, but "now" is inside the
// window, so the pump must be running once things settle.
//
// This asserts the relay, not an internal variable — it is the test that
// corresponds to what the maintainer saw at the pump house: healthy script,
// correct enabled schedule, `switch output:false` from 11:30 to 16:28.
func TestPoolPump_RestartInsideWindowStartsPump(t *testing.T) {
	skipUnlessNowInside(t, poolPumpWideStartMin, poolPumpWideStopMin)

	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	d := poolPumpWinterDefaultSummerDevice(t, srv.URL, false)

	stopScript := runPoolPump(t, d)
	defer stopScript()

	waitPoolPumpDailyCheckDecidedSummer(t, d)
	assertPoolPumpScheduleUntouched(t, d)

	started := waitFor(eventTimeout, 100*time.Millisecond, func() bool {
		v, ok := d.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	})
	if !started {
		ws, we := poolPumpProbeWindow(t, d)
		t.Fatalf("a restart INSIDE the day's run window must leave the pump running, but "+
			"active-output = %v and F_WIN_START/F_WIN_STOP = %d/%d.\n"+
			"Local time is %s; the schedule on the device says %s - %s and both jobs are "+
			"enabled. The window was resolvable for the whole run and nothing asked, so "+
			"desiredOutput() returned -2 ('no opinion') on every reconcile — this is the "+
			"11:32-16:28 filtration window the pool lost on 2026-08-17 (#512).",
			kvsValue(d, "script/pool-pump/active-output"), ws, we,
			poolPumpHM(nowMinutes()),
			poolPumpHM(poolPumpWideStartMin), poolPumpHM(poolPumpWideStopMin))
	}

	// And it must stay on: a late reconcile must not undo it.
	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "0" {
		t.Fatalf("the pump started inside the window and was then turned back off "+
			"(active-output = %v)", v)
	}
}

// TestPoolPump_SymbolicWinterWindowLeavesRunningPumpAlone is the guard that
// must SURVIVE the fix.
//
// It passes on unmodified main and must go on passing afterwards. That is the
// point of it: #512 is about the window being unknown when it is perfectly
// knowable, and the obvious cheap "fix" — treat an unresolved window as
// closed, or force a value into it — would silently convert -2 into "off" and
// stop a running pump on a device whose timespecs are still symbolic. The
// pump is seeded ALREADY RUNNING here so that collapse fails loudly instead of
// looking like a no-op.
//
// It also covers the winter arm of updateScheduleMode(), which the two tests
// above never reach: a cold forecast calls updateScheduleMode('winter', null,
// null), and with the mode already winter that hits the
// `if (!modeChanged && !hasTimings)` early return at :2493 — before any
// Schedule.List, so a fix placed only in updateNext()'s completion never runs
// on this path at all. Either way the answer must be the same: the night jobs
// carry "@sunset"/"@sunrise", parseHM() returns null for both, and the "still
// symbolic" guard must leave the facts alone.
func TestPoolPump_SymbolicWinterWindowLeavesRunningPumpAlone(t *testing.T) {
	srv, forecastHits := poolPumpCountingWinterForecastServer(t)

	cs := pro1ComponentStatus()
	cs["switch:0"] = map[string]interface{}{"id": 0, "output": true} // already running

	d := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "winter",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: cs,
		// Winter mode, so onWindowJobs() reads the night pair. Both night jobs
		// are ENABLED — nothing is skipped by the enable guard — but their
		// timespecs are the symbolic forms createSchedules() lays down and
		// updateScheduleMode() never rewrites (it only ever touches night
		// ENABLE flags). parseHM() returns null for both, forever.
		Schedules: []map[string]interface{}{
			poolPumpJob(1, "handleDailyCheck()", "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
			poolPumpJob(2, "handleMorningStart()", poolPumpTimespec(11, 30), false),
			poolPumpJob(3, "handleEveningStop()", poolPumpTimespec(16, 30), false),
			poolPumpJob(4, "handleNightStart()", "@sunset * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
			poolPumpJob(5, "handleNightStop()", "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
		},
		ScheduleEvalInjector: make(chan []byte, 4),
	}

	stopScript := runPoolPump(t, d)
	defer stopScript()

	// Positive marker: the daily check reached the forecast, so "nothing
	// moved" below is a decision, not an absence of one.
	if !waitFor(initTimeout, 100*time.Millisecond, func() bool { return forecastHits() > 0 }) {
		t.Fatalf("the daily check never fetched the forecast — this test did not reach " +
			"decideModeFromForecast() and asserts nothing")
	}

	// Give every deferred path a generous chance to do the wrong thing.
	settlePoolPumpTaskQueue(t)
	settlePoolPumpTaskQueue(t)

	ws, we := poolPumpProbeWindow(t, d)
	if ws >= 0 || we >= 0 {
		t.Fatalf("both night jobs carry symbolic timespecs that parseHM() cannot resolve, "+
			"so the window must stay UNKNOWN, but F_WIN_START/F_WIN_STOP = %d/%d — a value "+
			"was invented for a window nobody can know (#512 must not weaken the :1708 guard)",
			ws, we)
	}
	if v := kvsValue(d, "script/pool-pump/active-output"); v != nil && v != "0" {
		t.Fatalf("window unresolvable: the policy must have no opinion (-2) and leave the "+
			"relay alone, but active-output settled on %v", v)
	}
	entry, ok := componentStatusValue(d, "switch:0")
	if !ok {
		t.Fatalf("switch:0 status missing")
	}
	m, _ := entry.(map[string]interface{})
	if on, _ := m["output"].(bool); !on {
		t.Fatalf("the running pump was switched OFF while the window was unresolvable — " +
			"desiredOutput() must return -2, not -1, when the window cannot be resolved " +
			"(#441/#436; #512 must not trade this away for the retry)")
	}
	if forecastHits() == 0 {
		t.Fatalf("forecast was never fetched")
	}
}

// TestPoolPump_WindowIsResolvableByHandTheWholeTime is the control for the two
// red tests above, and it passes on unmodified main.
//
// It runs the identical fixture, then does by injection exactly what the
// maintainer did on the live pump at 16:33:27 on 2026-08-17 — one
// Shelly.call("Schedule.List", {}, onWindowJobs) — and shows the window
// resolving instantly to the same 01:00/23:29 the jobs had carried since boot.
//
// Without this, a reader could not tell whether
// TestPoolPump_WindowResolvesAfterNoOpDailyCheck fails because the script
// never asks, or because 60/1409 is simply the wrong expectation. It is the
// right expectation: it is what the script's own onWindowJobs() computes from
// the jobs already on the device.
//
// It stays valid after the fix — a fix resolves the window before the
// injection, which then re-derives the same pair and setWindow() returns early
// on the unchanged values.
func TestPoolPump_WindowIsResolvableByHandTheWholeTime(t *testing.T) {
	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	d := poolPumpWinterDefaultSummerDevice(t, srv.URL, false)

	stopScript := runPoolPump(t, d)
	defer stopScript()

	waitPoolPumpDailyCheckDecidedSummer(t, d)
	assertPoolPumpScheduleUntouched(t, d)

	if err := evalPoolPumpSchedule(d, "Shelly.call('Schedule.List', {}, onWindowJobs);"); err != nil {
		t.Fatalf("could not inject the by-hand window resolution: %v", err)
	}

	ws, we := waitPoolPumpWindowResolved(t, d, eventTimeout)
	if ws != poolPumpWideStartMin || we != poolPumpWideStopMin {
		t.Fatalf("one hand-called Schedule.List -> onWindowJobs must resolve the window from "+
			"the jobs sitting on the device, but F_WIN_START/F_WIN_STOP = %d/%d (%s - %s), "+
			"want %d/%d (%s - %s). If this fails, the fixture — not the script — is wrong, "+
			"and the expectations in the #512 red tests cannot be trusted.",
			ws, we, poolPumpHM(ws), poolPumpHM(we),
			poolPumpWideStartMin, poolPumpWideStopMin,
			poolPumpHM(poolPumpWideStartMin), poolPumpHM(poolPumpWideStopMin))
	}
}
