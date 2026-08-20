package scripts

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/script"
)

// poolPumpBrokenForecastURL returns a local server whose response has no
// "hourly" field, so onForecast() bails with "Invalid forecast structure" and
// getMaxForecastTemp() stays null -- the same technique
// TestPoolPump_WaterSupplyRestoresSpeed uses. Without this, init's own
// handleDailyCheck() -> performDailyModeCheck() can reach the real
// api.open-meteo.com in a sandbox with internet access and silently rewrite
// the run window via updateScheduleMode() before this test ever gets to seed
// its own STATE facts, racing every assertion below.
//
// Also returns `served`, signalled once per request the server actually
// handles. #524 review round 2: a fixed pre-seed sleep is not sufficient to
// know init's own automatic daily-check fetch has resolved -- the sleep
// bounds a GUESS about how long the four initSteps plus the HTTP round trip
// take, not the round trip itself, and under load the guess can still be too
// short (observed live 2026-08-19: a seed still landed before the in-flight
// automatic check's own callback ran, this time surfacing as a REWRITTEN
// window even though the pre-write probe looked correct a moment earlier,
// because the callback resolved in the gap between that probe and the
// eventual read). Waiting on `served` bounds the real bottleneck -- the HTTP
// round trip -- directly instead of guessing its duration.
func poolPumpBrokenForecastURL(t *testing.T) (url string, served <-chan struct{}) {
	t.Helper()
	ch := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
		select {
		case ch <- struct{}{}:
		default:
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "?daily=sunrise,sunset", ch
}

// === #524: runtime-recovery arithmetic ===
//
// desiredOutput()'s run window is a fixed clock interval sized each morning
// from computeRunHours(). extendWindowForShortfall() closes the gap between
// what was intended for the day and what was actually achieved
// (STATE.runtimeTodaySec) by pushing the window's stop bound later, bounded
// by the same stopCeil decideModeFromForecast() uses (sunset - 0.5h) so the
// pump never runs into the night. See setWindow()/extendWindowForShortfall()
// in pool-pump.js.
//
// These tests drive the arithmetic directly via evalPoolPumpSchedule rather
// than waiting out real minutes of simulated day: they seed STATE facts the
// same way TestPoolPump_SolarHardCeiling_DayRolloverUnblocksWithoutRestart
// does, fire the handler that triggers the recovery check, and either read
// the resulting policy decision (active-output) or probe the in-memory
// F_WIN_STOP fact itself via a KVS.Set round trip (there is no other way to
// observe a plain JS global from Go).

// minutesSinceMidnight mirrors the `d.getHours()*60+d.getMinutes()` the
// script itself uses everywhere it reads "now" as a window-relative instant.
func minutesSinceMidnight(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// poolPumpSummerWindowNow builds a Pro1, summer-mode device whose
// morning/evening schedule jobs bracket "now" by the given offsets, with
// computeRunHours() pinned to exactly runHours regardless of the forecast
// temp seeded later (poolPumpWindowKVS rigs the pool geometry so the
// temperature scale only matters below CONFIG.maxTemp). Also returns the
// broken forecast server's `served` channel -- see poolPumpBrokenForecastURL
// and seedPoolPumpForecastState.
func poolPumpSummerWindowNow(t *testing.T, runHours float64, offsetStart, offsetStop time.Duration) (*script.DeviceState, <-chan struct{}) {
	t.Helper()
	now := time.Now()
	url, served := poolPumpBrokenForecastURL(t)
	return &script.DeviceState{
		KVS: poolPumpWindowKVS(runHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  url,
		},
		ComponentStatus:      pro1ComponentStatus(),
		Schedules:            poolPumpSummerSchedules(now.Add(offsetStart), now.Add(offsetStop)),
		ScheduleEvalInjector: make(chan []byte, 4),
	}, served
}

// poolPumpProbeSeq makes every readPoolPumpWindowStop() probe key unique.
// #524 review round 2 diagnostic (2026-08-19): a fixed probe key
// ("test/win-stop") meant that once the FIRST probe had written it, every
// later call's waitFor() predicate -- `_, ok := d.KVSValue(key); return ok`
// -- was satisfied INSTANTLY by that stale leftover value, before the new
// probe's own write had actually landed. This silently returned an
// arbitrarily-stale reading instead of the current F_WIN_STOP, which is what
// every failure chased as a phantom "race with init's automatic daily check"
// in this round -- confirmed empirically by combining a fire+read into one
// atomic eval (no separate probe involved) and observing the extension DID
// happen correctly, immediately. Real bug: the probe helper, not the script.
var poolPumpProbeSeq int64

// readPoolPumpWindowStop probes F_WIN_STOP (minutes since midnight) by
// having the script itself write it to a fresh, uniquely-named test-only KVS
// key each call -- the only way to observe a plain JS global from Go, and
// the only way to be sure a read is not a stale leftover from an earlier
// probe (see poolPumpProbeSeq).
func readPoolPumpWindowStop(t *testing.T, d *script.DeviceState) int64 {
	t.Helper()
	poolPumpProbeSeq++
	key := fmt.Sprintf("test/win-stop-%d", poolPumpProbeSeq)
	if err := evalPoolPumpSchedule(d, fmt.Sprintf("Shelly.call('KVS.Set',{key:%q,value:F_WIN_STOP})", key)); err != nil {
		t.Fatalf("failed to probe F_WIN_STOP: %v", err)
	}
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		_, ok := d.KVSValue(key)
		return ok
	}) {
		t.Fatalf("timed out waiting for F_WIN_STOP probe")
	}
	return parseKVSInt(t, d, key)
}

// waitForWindowSeeded polls F_WIN_STOP (via readPoolPumpWindowStop) until it
// equals wantStopMin -- an observable proxy for "init's sequential chain
// (four ~200ms-ticked steps, the last of which is verifySchedules()'s
// onWindowJobs()) has reached the point where the fixture's own window is in
// effect." Bounds the variable, load-dependent portion of the wait against a
// real signal instead of a blind sleep; fails loudly (not silently) if the
// window never gets there at all.
func waitForWindowSeeded(t *testing.T, d *script.DeviceState, wantStopMin int64) {
	t.Helper()
	var last int64 = -1
	if !waitFor(initTimeout, 100*time.Millisecond, func() bool {
		last = readPoolPumpWindowStop(t, d)
		return last == wantStopMin
	}) {
		t.Fatalf("F_WIN_STOP never settled at the fixture's stop (%d minutes); last observed %d -- "+
			"init's schedule chain did not complete as this test expected", wantStopMin, last)
	}
}

// assertWindowStillMatchesFixture fails loudly if F_WIN_STOP has moved away
// from the fixture's own value. This is the #524 review's "lost race with
// init's automatic daily check" failure mode: if that automatic check ever
// consumes a seed this test wrote for its own later use, it rewrites the
// window via updateScheduleMode() using peakHour/sunrise/sunset defaults, not
// the fixture's start/stop -- and every assertion downstream would silently
// measure the wrong thing instead of failing. Call this right after seeding
// any STATE the automatic check could plausibly race with.
func assertWindowStillMatchesFixture(t *testing.T, d *script.DeviceState, wantStopMin int64) {
	t.Helper()
	if got := readPoolPumpWindowStop(t, d); got != wantStopMin {
		t.Fatalf("F_WIN_STOP drifted from the fixture's stop (%d) to %d before this test fired its "+
			"own action -- init's automatic daily check must have won the race and rewritten the "+
			"window from a seed meant for later; this test's setup is no longer valid", wantStopMin, got)
	}
}

// seedPoolPumpForecastState directly assigns STATE.maxForecastTemp and
// STATE.sunsetHour, standing in for a real Open-Meteo fetch (maxForecastTemp
// >= poolPumpWindowKVS's max-temp=35 clamps computeRunHours()'s temperature
// scale to 1, so the pinned runHours from poolPumpWindowKVS applies exactly).
//
// fixtureStopMin is the caller's fixture's own schedule stop (minutes since
// midnight); forecastServed is poolPumpBrokenForecastURL()'s `served`
// channel. Both bound this function's wait against real observables (the
// window having settled from the fixture, and init's own automatic
// handleDailyCheck() having actually reached the broken forecast server)
// instead of a guessed sleep duration -- and, after writing, fail loudly
// rather than silently if the automatic check still won the race anyway
// (see assertWindowStillMatchesFixture). onForecast() leaves
// STATE.maxForecastTemp untouched on its "invalid structure" bail (it only
// ever assigns it on a successful parse), so a seed written before that
// automatic check's own HTTP round trip resolves would otherwise be
// silently picked up and misread as a real forecast -- and #524 review
// round 2 found live that even a generous FIXED sleep is not sufficient:
// the callback can resolve in the gap between a pre-write probe and this
// function returning, so only waiting on the request having actually
// reached the server (not a guess about how long that takes) closes it.
func seedPoolPumpForecastState(t *testing.T, d *script.DeviceState, forecastServed <-chan struct{}, maxForecastTemp, sunsetHour float64, fixtureStopMin int64) {
	t.Helper()
	waitForWindowSeeded(t, d, fixtureStopMin)
	select {
	case <-forecastServed:
	case <-time.After(initTimeout):
		t.Fatalf("init's automatic daily check never reached the broken forecast server within %v", initTimeout)
	}
	// Bounded margin for the script to receive and synchronously process the
	// (already-sent) HTTP response -- onForecast()'s "invalid structure" bail
	// and decideModeFromForecast()'s "no forecast data" bail, both plain
	// synchronous JS once the response is delivered. Not bounding an HTTP
	// round trip anymore, just local event-loop delivery.
	time.Sleep(500 * time.Millisecond)
	code := fmt.Sprintf("STATE.maxForecastTemp = %g; STATE.sunsetHour = %g;", maxForecastTemp, sunsetHour)
	if err := evalPoolPumpSchedule(d, code); err != nil {
		t.Fatalf("failed to seed forecast state: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	assertWindowStillMatchesFixture(t, d, fixtureStopMin)
}

// seedPoolPumpRuntimeToday directly assigns STATE.runtimeTodaySec (with a
// fresh STATE.runtimeTs so ensureRuntimeDay() never treats it as stale),
// standing in for time the pump has already filtered today -- whether from
// an earlier part of the window or, per the #524 maintainer decision, from
// solar running before the window ever opened. STATE.runtimeTodaySec counts
// only CLOSED intervals (see its own declaration in pool-pump.js); a
// still-open run is seeded separately via seedPoolPumpOpenRunInterval.
func seedPoolPumpRuntimeToday(t *testing.T, d *script.DeviceState, sec float64) {
	t.Helper()
	code := fmt.Sprintf("STATE.runtimeTs = Math.floor(Date.now()/1000); STATE.runtimeTodaySec = %g;", sec)
	if err := evalPoolPumpSchedule(d, code); err != nil {
		t.Fatalf("failed to seed runtime state: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
}

// seedPoolPumpOpenRunInterval sets STATE.runStartTs to openForSec seconds in
// the past, standing in for a pump that has been running continuously since
// then without the interval having closed yet -- the shape
// handleEveningStop() itself always fires into (see the #524 review's
// Blocker 1: the pump is still running at that exact instant). Lets a test
// pin an arbitrary open-interval duration deterministically instead of
// sleeping in real time.
func seedPoolPumpOpenRunInterval(t *testing.T, d *script.DeviceState, openForSec float64) {
	t.Helper()
	code := fmt.Sprintf("STATE.runStartTs = Date.now() - %g;", openForSec*1000)
	if err := evalPoolPumpSchedule(d, code); err != nil {
		t.Fatalf("failed to seed open run interval: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
}

// TestPoolPump_RuntimeRecovery_GapExtendsWindowToConverge is the core #524
// shape from the issue's own measured evidence: a non-filtering gap (here,
// simulated directly as a shortfall between STATE.runtimeTodaySec and
// computeRunHours()'s intent, standing in for the water-supply interlock
// hold that produced it live on 2026-08-19) costs the day 30 of an intended
// 60 minutes. The schedule's own evening-stop edge has already passed by the
// time it fires -- without recovery the pump would simply stay off -- but
// extendWindowForShortfall() must push the window's stop bound forward
// enough to recover the lost time, and the reconciler must then start the
// pump because "now" is back inside the (extended) window.
func TestPoolPump_RuntimeRecovery_GapExtendsWindowToConverge(t *testing.T) {
	d, forecastServed := poolPumpSummerWindowNow(t, 1 /* intended = 3600s */, -2*time.Hour, -1*time.Minute)
	fixtureStopMin := int64(minutesSinceMidnight(time.Now()) - 1) // the schedule's own (now past) stop
	stop := runPoolPump(t, d)
	defer stop()

	sunsetHour := float64(minutesSinceMidnight(time.Now())+180) / 60 // far away: not the bound under test
	seedPoolPumpForecastState(t, d, forecastServed, 40, sunsetHour, fixtureStopMin)
	seedPoolPumpRuntimeToday(t, d, 1800) // half of the 3600s intent achieved -> 1800s (30min) shortfall

	originalStopMin := fixtureStopMin

	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("failed to fire handleEveningStop(): %v", err)
	}

	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("a 1800s shortfall against a 3600s intent must extend the window and (re)start the "+
			"pump past its originally scheduled stop, but active-output = %v",
			kvsValue(d, "script/pool-pump/active-output"))
	}

	if got := readPoolPumpWindowStop(t, d); got <= int64(originalStopMin) {
		t.Fatalf("F_WIN_STOP = %d did not move past the original scheduled stop (%d) -- "+
			"the pump started some other way, not via window recovery", got, originalStopMin)
	}
}

// TestPoolPump_RuntimeRecovery_NoShortfallDoesNotExtend is the control for
// the test above: once STATE.runtimeTodaySec already meets the day's intent
// (computeRunHours()'s target), extendWindowForShortfall() must be a no-op
// and the pump must stay off past a stop that has already elapsed -- proving
// the feature only ever recovers a real shortfall, never runs unconditionally
// past the scheduled edge.
func TestPoolPump_RuntimeRecovery_NoShortfallDoesNotExtend(t *testing.T) {
	d, forecastServed := poolPumpSummerWindowNow(t, 1, -2*time.Hour, -1*time.Minute)
	fixtureStopMin := int64(minutesSinceMidnight(time.Now()) - 1)
	stop := runPoolPump(t, d)
	defer stop()

	sunsetHour := float64(minutesSinceMidnight(time.Now())+180) / 60
	seedPoolPumpForecastState(t, d, forecastServed, 40, sunsetHour, fixtureStopMin)
	seedPoolPumpRuntimeToday(t, d, 3600) // intent already met: no shortfall

	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("failed to fire handleEveningStop(): %v", err)
	}

	settlePoolPumpTaskQueue(t)
	// Deliberately NOT `v != nil && v != "-1"` -- that earlier form silently
	// accepted a MISSING active-output key as a pass. This must observe the
	// pump actually off, not merely "not observed to be on" (#524 review).
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("no shortfall: expected the pump to stay off past its elapsed stop, got active-output=%v", v)
	}
}

// TestPoolPump_RuntimeRecovery_SolarCreditShortensExtension is the #524
// maintainer decision that solar-triggered runtime counts toward the day's
// intent: STATE.runtimeTodaySec already includes any second the pump ran via
// F_SOLAR_WANT (desiredOutput() checks solar before the window), so the same
// shortfall arithmetic that recovers a water-supply gap automatically credits
// solar runtime too. Compares two otherwise-identical devices, one with a
// large pre-existing runtimeTodaySec (standing in for a solar-heavy morning)
// and one with none, and asserts the solar-credited device needs -- and gets
// -- a much shorter extension.
func TestPoolPump_RuntimeRecovery_SolarCreditShortensExtension(t *testing.T) {
	const intendedSec = 3600 // 1h

	run := func(t *testing.T, alreadyRanSec float64) int64 {
		d, forecastServed := poolPumpSummerWindowNow(t, intendedSec/3600, -2*time.Hour, -1*time.Minute)
		fixtureStopMin := int64(minutesSinceMidnight(time.Now()) - 1)
		stop := runPoolPump(t, d)
		defer stop()

		sunsetHour := float64(minutesSinceMidnight(time.Now())+180) / 60
		seedPoolPumpForecastState(t, d, forecastServed, 40, sunsetHour, fixtureStopMin)
		seedPoolPumpRuntimeToday(t, d, alreadyRanSec)

		if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
			t.Fatalf("failed to fire handleEveningStop(): %v", err)
		}
		settlePoolPumpTaskQueue(t)
		return readPoolPumpWindowStop(t, d)
	}

	noCreditStop := run(t, 0)               // full 3600s shortfall
	solarCreditStop := run(t, intendedSec-600) // only 600s (10min) shortfall left

	nowMin := int64(minutesSinceMidnight(time.Now()))
	noCreditExtension := noCreditStop - nowMin
	solarCreditExtension := solarCreditStop - nowMin

	// 60min vs ~10min of extension: assert a wide, timing-noise-tolerant gap
	// rather than exact minute equality.
	if solarCreditExtension >= noCreditExtension-20 {
		t.Fatalf("solar-credited runtime did not meaningfully shorten the extension: "+
			"no-credit extended by ~%dmin, solar-credited extended by ~%dmin (want the latter "+
			"at least 20min shorter)", noCreditExtension, solarCreditExtension)
	}
}

// TestPoolPump_RuntimeRecovery_BoundedBySunset asserts extendWindowForShortfall()
// never pushes F_WIN_STOP past stopCeil (sunset - 0.5h, the same bound
// decideModeFromForecast() itself uses) even when the shortfall computed is
// far larger than the time remaining before sunset -- the "must not chase
// the target indefinitely" design constraint.
func TestPoolPump_RuntimeRecovery_BoundedBySunset(t *testing.T) {
	d, forecastServed := poolPumpSummerWindowNow(t, 10 /* intended = 36000s, deliberately huge */, -2*time.Hour, -1*time.Minute)
	fixtureStopMin := int64(minutesSinceMidnight(time.Now()) - 1)
	stop := runPoolPump(t, d)
	defer stop()

	nowMin := minutesSinceMidnight(time.Now())
	sunsetHour := float64(nowMin+40) / 60 // stopCeil = sunset - 0.5h = now + 10min
	seedPoolPumpForecastState(t, d, forecastServed, 40, sunsetHour, fixtureStopMin)
	seedPoolPumpRuntimeToday(t, d, 0) // the whole 36000s is "owed"

	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("failed to fire handleEveningStop(): %v", err)
	}
	settlePoolPumpTaskQueue(t)

	stopCeilMin := int64(nowMin + 10)
	got := readPoolPumpWindowStop(t, d)
	// +/-2min tolerance for the real time elapsed between capturing nowMin and
	// the script evaluating its own "now".
	if got > stopCeilMin+2 {
		t.Fatalf("F_WIN_STOP = %d must not exceed stopCeil (~%d, sunset-0.5h) even for a huge "+
			"shortfall -- the pump must not be pushed to run into the night", got, stopCeilMin)
	}
	if got < stopCeilMin-2 {
		t.Fatalf("F_WIN_STOP = %d should have been extended up to stopCeil (~%d) given a shortfall "+
			"this large", got, stopCeilMin)
	}
}

// TestPoolPump_RuntimeRecovery_HardCeilingStillRefusesRunaway asserts
// solarHardCeilingReached() -- checked ahead of the window in desiredOutput()
// -- still refuses to start the pump even once extendWindowForShortfall()
// has pushed the window well past "now". Recovery must never bypass the
// existing hard-ceiling bound.
func TestPoolPump_RuntimeRecovery_HardCeilingStillRefusesRunaway(t *testing.T) {
	// Same tiny ceiling override as TestPoolPump_SolarRespectsHardCeiling
	// (~7.7s target against the schema-default pool geometry).
	kvs := solarKVS(map[string]string{"solar-max-turnover": "0.001"})

	now := time.Now()
	forecastURL, forecastServed := poolPumpBrokenForecastURL(t)
	d := &script.DeviceState{
		KVS: kvs,
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  forecastURL,
		},
		ComponentStatus:      pro3ComponentStatus(),
		Schedules:            poolPumpSummerSchedules(now.Add(-2*time.Hour), now.Add(-1*time.Minute)),
		ScheduleEvalInjector: make(chan []byte, 4),
	}
	fixtureStopMin := int64(minutesSinceMidnight(now) - 1)
	stop := runPoolPump(t, d)
	defer stop()

	sunsetHour := float64(minutesSinceMidnight(now)+180) / 60
	seedPoolPumpForecastState(t, d, forecastServed, 40, sunsetHour, fixtureStopMin)
	// Comfortably past the ~7.7s hard ceiling, but nowhere near the schema-default
	// computeRunHours() intent (~38700s) -- extendWindowForShortfall() sees a
	// large shortfall and extends the window regardless of the ceiling; only
	// desiredOutput()'s solarHardCeilingReached() check may refuse the start.
	seedPoolPumpRuntimeToday(t, d, 3600)

	originalStopMin := fixtureStopMin

	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("failed to fire handleEveningStop(): %v", err)
	}
	settlePoolPumpTaskQueue(t)

	if got := readPoolPumpWindowStop(t, d); got <= originalStopMin {
		t.Fatalf("setup: window did not extend (F_WIN_STOP=%d, original stop=%d) -- "+
			"this test needs the window extended to prove the ceiling still blocks it anyway",
			got, originalStopMin)
	}
	// Deliberately NOT `v != nil && v != "-1"` -- see NoShortfallDoesNotExtend.
	if v := kvsValue(d, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("hard ceiling exceeded: expected the pump to stay off despite the extended window, "+
			"got active-output=%v", v)
	}
}

// TestPoolPump_RuntimeRecovery_OpenIntervalCountsTowardShortfall is the
// regression test for BLOCKER 1 of the #524 silent-failure review:
// handleEveningStop() fires with the pump STILL RUNNING -- that is exactly
// what it is about to stop -- so at that instant STATE.runtimeTodaySec (which
// only ever counts CLOSED intervals) excludes the entire in-progress run.
// Before the fix, extendWindowForShortfall() read STATE.runtimeTodaySec alone
// and so treated a run that was already most of the way to the day's intent
// as if none of it had happened, computing a shortfall roughly the size of
// the WHOLE intent instead of the true remaining gap -- and clamping to
// stopCeil on essentially every summer evening, not just the days that
// actually had an interruption.
//
// Seeds STATE.runtimeTodaySec = 0 (no CLOSED intervals) plus a 1800s-old
// STATE.runStartTs (an OPEN interval that has been running for exactly
// 1800s, injected directly rather than waited out in real time -- see
// seedPoolPumpOpenRunInterval) against a 3600s intent, so the correct
// shortfall is ~1800s (~30min) and the buggy one is the full ~3600s
// (~60min). The two differ by a full 30 minutes, which the +/-5min tolerance
// below cannot confuse with timing noise.
func TestPoolPump_RuntimeRecovery_OpenIntervalCountsTowardShortfall(t *testing.T) {
	d, forecastServed := poolPumpSummerWindowNow(t, 1 /* intended = 3600s */, -2*time.Hour, -1*time.Minute)
	fixtureStopMin := int64(minutesSinceMidnight(time.Now()) - 1)
	stop := runPoolPump(t, d)
	defer stop()

	sunsetHour := float64(minutesSinceMidnight(time.Now())+180) / 60 // far away: not the bound under test
	seedPoolPumpForecastState(t, d, forecastServed, 40, sunsetHour, fixtureStopMin)
	seedPoolPumpRuntimeToday(t, d, 0)          // no CLOSED interval at all
	seedPoolPumpOpenRunInterval(t, d, 1800)    // but an OPEN one, running 1800s already

	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("failed to fire handleEveningStop(): %v", err)
	}
	settlePoolPumpTaskQueue(t)

	nowMin := int64(minutesSinceMidnight(time.Now()))
	wantExtension := int64(30) // (3600 - 1800) / 60
	got := readPoolPumpWindowStop(t, d)
	gotExtension := got - nowMin

	if gotExtension < wantExtension-5 || gotExtension > wantExtension+5 {
		t.Fatalf("extension = %dmin (F_WIN_STOP=%d), want ~%dmin (+/-5): the open interval's 1800s "+
			"was not counted toward the shortfall -- got the ~60min extension the pre-fix bug would "+
			"produce by treating the whole in-progress run as if it had not happened", gotExtension, got, wantExtension)
	}
}

// TestPoolPump_RuntimeRecovery_DailyCheckReseedsLeakedExtension is the
// regression test for BLOCKER 2 of the #524 silent-failure review:
// extendWindowForShortfall() moves F_WIN_STOP without rewriting the schedule
// job, so the in-memory fact and the on-device schedule can diverge. Before
// the fix, updateScheduleMode() only re-seeded the fact from the schedule
// (onWindowJobs()) when windowChanged was true -- false on the common
// no-op-rewrite morning, when the freshly recomputed window matches the jobs
// already on the device. On such a morning a leftover extension from the day
// before would silently survive, because nothing ever pulled F_WIN_STOP back
// to what the schedule (and the day's own forecast) actually say.
//
// Uses a REAL (working, not broken) forecast server, exactly like
// TestPoolPump_DailyCheckNoOpRewriteLeavesWindowAndScheduleUnchanged: the
// fixture's own Schedules are seeded to precisely the window that server's
// forecast recomputes (poolPumpWideStartMin/StopMin), so re-running the daily
// check against the SAME forecast is a genuine no-op rewrite -- windowChanged
// stays false -- which is exactly the case the pre-fix gate mishandled. No
// manual STATE seeding, so there is no race with init's own automatic daily
// check to guard against here.
func TestPoolPump_RuntimeRecovery_DailyCheckReseedsLeakedExtension(t *testing.T) {
	skipUnlessNowInside(t, poolPumpWideStartMin, poolPumpWideStopMin)

	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, now.Location())
	stop2 := time.Date(now.Year(), now.Month(), now.Day(), 23, 29, 0, 0, now.Location())

	d := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus:      pro1ComponentStatus(),
		Schedules:            poolPumpSummerSchedules(start, stop2),
		ScheduleEvalInjector: make(chan []byte, 4),
	}
	stop := runPoolPump(t, d)
	defer stop()

	// The window already contains "now" (poolPumpWideStartMin/StopMin), so
	// init's own onWindowJobs() call starts the pump without any event.
	if !waitActiveOutput(t, d, "0") {
		t.Fatalf("setup: pump did not start even though the pre-existing window already contains now")
	}
	if got := readPoolPumpWindowStop(t, d); got != int64(poolPumpWideStopMin) {
		t.Fatalf("setup: F_WIN_STOP = %d, want the fixture's own stop %d before simulating a leak",
			got, poolPumpWideStopMin)
	}

	// Simulate "yesterday's extension carried into today": push F_WIN_STOP
	// two hours past the fixture's own stop, exactly as extendWindowForShortfall()
	// would have -- via setWindow(), never touching the schedule job itself.
	leakedStopMin := poolPumpWideStopMin + 120
	if err := evalPoolPumpSchedule(d, fmt.Sprintf("setWindow(F_WIN_START, %d)", leakedStopMin)); err != nil {
		t.Fatalf("failed to simulate a leaked extension: %v", err)
	}
	if got := readPoolPumpWindowStop(t, d); got != int64(leakedStopMin) {
		t.Fatalf("setup: leaked F_WIN_STOP did not take (%d), got %d", leakedStopMin, got)
	}

	// Re-run the daily check against the SAME (unchanged) forecast -- a
	// genuine no-op rewrite, windowChanged stays false.
	if err := evalPoolPumpSchedule(d, "handleDailyCheck()"); err != nil {
		t.Fatalf("failed to re-fire handleDailyCheck(): %v", err)
	}
	settlePoolPumpTaskQueue(t)

	if got := readPoolPumpWindowStop(t, d); got != int64(poolPumpWideStopMin) {
		t.Fatalf("F_WIN_STOP = %d after a no-op daily check, want it pulled back to the schedule's own "+
			"stop %d -- a leaked extension from a prior day must not survive an unchanged forecast",
			got, poolPumpWideStopMin)
	}
}
