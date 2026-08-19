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
func poolPumpBrokenForecastURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "?daily=sunrise,sunset"
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
// temperature scale only matters below CONFIG.maxTemp).
func poolPumpSummerWindowNow(t *testing.T, runHours float64, offsetStart, offsetStop time.Duration) *script.DeviceState {
	t.Helper()
	now := time.Now()
	return &script.DeviceState{
		KVS: poolPumpWindowKVS(runHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  poolPumpBrokenForecastURL(t),
		},
		ComponentStatus:      pro1ComponentStatus(),
		Schedules:            poolPumpSummerSchedules(now.Add(offsetStart), now.Add(offsetStop)),
		ScheduleEvalInjector: make(chan []byte, 4),
	}
}

// seedPoolPumpForecastState directly assigns STATE.maxForecastTemp and
// STATE.sunsetHour, standing in for a real Open-Meteo fetch (maxForecastTemp
// >= poolPumpWindowKVS's max-temp=35 clamps computeRunHours()'s temperature
// scale to 1, so the pinned runHours from poolPumpWindowKVS applies exactly).
//
// Waits first: init's own handleDailyCheck() -> performDailyModeCheck() ->
// decideModeFromForecast() runs automatically, once, some seconds after init
// completes (runPoolPump()'s init-done check fires as soon as the
// schedule-mode KVS key is written, which is EARLY in continueInit() --
// well before the sequential initSteps chain and the forecast HTTP round
// trip that follow it) -- racing this seed. If this write landed first, the
// automatic check's onForecast() leaves STATE.maxForecastTemp untouched on
// its "invalid structure" bail (it only ever assigns it on a successful
// parse), so decideModeFromForecast() would read this seeded value back as
// if it were a real forecast and rewrite the window via updateScheduleMode()
// (peakHour/sunrise/sunset defaults, NOT the fixture's start/stop),
// corrupting the very window this test is trying to control. 1s was
// measured NOT to be enough (observed live, 2026-08-19: the automatic check
// still won the race and rewrote the window); 4s comfortably covers the
// initSteps chain (four ~200ms-ticked async steps) plus the forecast HTTP
// round trip to the local httptest server.
func seedPoolPumpForecastState(t *testing.T, d *script.DeviceState, maxForecastTemp, sunsetHour float64) {
	t.Helper()
	time.Sleep(4 * time.Second)
	code := fmt.Sprintf("STATE.maxForecastTemp = %g; STATE.sunsetHour = %g;", maxForecastTemp, sunsetHour)
	if err := evalPoolPumpSchedule(d, code); err != nil {
		t.Fatalf("failed to seed forecast state: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
}

// seedPoolPumpRuntimeToday directly assigns STATE.runtimeTodaySec (with a
// fresh STATE.runtimeTs so ensureRuntimeDay() never treats it as stale),
// standing in for time the pump has already filtered today -- whether from
// an earlier part of the window or, per the #524 maintainer decision, from
// solar running before the window ever opened.
func seedPoolPumpRuntimeToday(t *testing.T, d *script.DeviceState, sec float64) {
	t.Helper()
	code := fmt.Sprintf("STATE.runtimeTs = Math.floor(Date.now()/1000); STATE.runtimeTodaySec = %g;", sec)
	if err := evalPoolPumpSchedule(d, code); err != nil {
		t.Fatalf("failed to seed runtime state: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
}

// readPoolPumpWindowStop probes F_WIN_STOP (minutes since midnight) by
// having the script itself write it to a test-only KVS key -- the only way
// to observe a plain JS global from Go.
func readPoolPumpWindowStop(t *testing.T, d *script.DeviceState) int64 {
	t.Helper()
	if err := evalPoolPumpSchedule(d, "Shelly.call('KVS.Set',{key:'test/win-stop',value:F_WIN_STOP})"); err != nil {
		t.Fatalf("failed to probe F_WIN_STOP: %v", err)
	}
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		_, ok := d.KVSValue("test/win-stop")
		return ok
	}) {
		t.Fatalf("timed out waiting for F_WIN_STOP probe")
	}
	return parseKVSInt(t, d, "test/win-stop")
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
	d := poolPumpSummerWindowNow(t, 1 /* intended = 3600s */, -2*time.Hour, -1*time.Minute)
	stop := runPoolPump(t, d)
	defer stop()

	sunsetHour := float64(minutesSinceMidnight(time.Now())+180) / 60 // far away: not the bound under test
	seedPoolPumpForecastState(t, d, 40, sunsetHour)
	seedPoolPumpRuntimeToday(t, d, 1800) // half of the 3600s intent achieved -> 1800s (30min) shortfall

	originalStopMin := minutesSinceMidnight(time.Now()) - 1 // the schedule's own (now past) stop

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
	d := poolPumpSummerWindowNow(t, 1, -2*time.Hour, -1*time.Minute)
	stop := runPoolPump(t, d)
	defer stop()

	sunsetHour := float64(minutesSinceMidnight(time.Now())+180) / 60
	seedPoolPumpForecastState(t, d, 40, sunsetHour)
	seedPoolPumpRuntimeToday(t, d, 3600) // intent already met: no shortfall

	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("failed to fire handleEveningStop(): %v", err)
	}

	settlePoolPumpTaskQueue(t)
	if v := kvsValue(d, "script/pool-pump/active-output"); v != nil && v != "-1" {
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
		d := poolPumpSummerWindowNow(t, intendedSec/3600, -2*time.Hour, -1*time.Minute)
		stop := runPoolPump(t, d)
		defer stop()

		sunsetHour := float64(minutesSinceMidnight(time.Now())+180) / 60
		seedPoolPumpForecastState(t, d, 40, sunsetHour)
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
	d := poolPumpSummerWindowNow(t, 10 /* intended = 36000s, deliberately huge */, -2*time.Hour, -1*time.Minute)
	stop := runPoolPump(t, d)
	defer stop()

	nowMin := minutesSinceMidnight(time.Now())
	sunsetHour := float64(nowMin+40) / 60 // stopCeil = sunset - 0.5h = now + 10min
	seedPoolPumpForecastState(t, d, 40, sunsetHour)
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
	d := &script.DeviceState{
		KVS: kvs,
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  poolPumpBrokenForecastURL(t),
		},
		ComponentStatus:      pro3ComponentStatus(),
		Schedules:            poolPumpSummerSchedules(now.Add(-2*time.Hour), now.Add(-1*time.Minute)),
		ScheduleEvalInjector: make(chan []byte, 4),
	}
	stop := runPoolPump(t, d)
	defer stop()

	sunsetHour := float64(minutesSinceMidnight(now)+180) / 60
	seedPoolPumpForecastState(t, d, 40, sunsetHour)
	// Comfortably past the ~7.7s hard ceiling, but nowhere near the schema-default
	// computeRunHours() intent (~38700s) -- extendWindowForShortfall() sees a
	// large shortfall and extends the window regardless of the ceiling; only
	// desiredOutput()'s solarHardCeilingReached() check may refuse the start.
	seedPoolPumpRuntimeToday(t, d, 3600)

	originalStopMin := minutesSinceMidnight(time.Now()) - 1

	if err := evalPoolPumpSchedule(d, "handleEveningStop()"); err != nil {
		t.Fatalf("failed to fire handleEveningStop(): %v", err)
	}
	settlePoolPumpTaskQueue(t)

	if got := readPoolPumpWindowStop(t, d); got <= int64(originalStopMin) {
		t.Fatalf("setup: window did not extend (F_WIN_STOP=%d, original stop=%d) -- "+
			"this test needs the window extended to prove the ceiling still blocks it anyway",
			got, originalStopMin)
	}
	if v := kvsValue(d, "script/pool-pump/active-output"); v != nil && v != "-1" {
		t.Fatalf("hard ceiling exceeded: expected the pump to stay off despite the extended window, "+
			"got active-output=%v", v)
	}
}
