package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

const poolPumpScriptPath = "pool-pump.js"

func readPoolPumpScript(t *testing.T) []byte {
	t.Helper()
	buf, err := os.ReadFile(poolPumpScriptPath)
	if err != nil {
		t.Fatalf("failed to read pool-pump.js: %v", err)
	}
	return buf
}

func controllerKVS() map[string]interface{} {
	return map[string]interface{}{
		"script/pool-pump/preferred":      "shellyplus1-b8d61a85a970",
		"script/pool-pump/pro3-id":        "shellyplus1-b8d61a85a970",
		"script/pool-pump/pro1-id":        "shellypro1-ddeeff445566",
		"script/pool-pump/mqtt-topic":     "pool/pump",
		"script/pool-pump/logging":        "false",
		"script/pool-pump/speed":          "eco",
		"script/pool-pump/eco-speed":      "0",
		"script/pool-pump/mid-speed":      "1",
		"script/pool-pump/high-speed":     "2",
		"script/pool-pump/night-duration": "3600000",
		"script/pool-pump/grace-delay":    "10000",
		"script/pool-pump/temp-threshold": "20",
	}
}

func poolPumpSchedules() []map[string]interface{} {
	scriptID := 1
	return []map[string]interface{}{
		{
			"id": 1, "enable": true,
			"timespec": "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": "handleDailyCheck()"},
			}},
		},
		{
			"id": 2, "enable": true,
			"timespec": "@sunrise+3h * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": "handleMorningStart()"},
			}},
		},
		{
			"id": 3, "enable": true,
			"timespec": "@sunset * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": "handleEveningStop()"},
			}},
		},
		{
			"id": 4, "enable": true,
			"timespec": "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": "handleNightStart()"},
			}},
		},
		{
			"id": 5, "enable": true,
			"timespec": "0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": "handleNightStop()"},
			}},
		},
	}
}

func pro3ComponentStatus() map[string]interface{} {
	return map[string]interface{}{
		"switch:0": map[string]interface{}{"id": 0, "output": false},
		"switch:1": map[string]interface{}{"id": 1, "output": false},
		"switch:2": map[string]interface{}{"id": 2, "output": false},
		"input:0":  map[string]interface{}{"id": 0, "state": false},
		"input:1":  map[string]interface{}{"id": 1, "state": false},
		"input:2":  map[string]interface{}{"id": 2, "state": false},
		"mqtt":     map[string]interface{}{"connected": true},
		"sys":      map[string]interface{}{"device_id": "shellyplus1-b8d61a85a970"},
	}
}

// Timeouts for the goja-backed pool-pump tests. These are deliberately
// generous rather than tight: every waitFor() below returns the moment its
// predicate holds, so a large budget costs nothing on a passing run and only
// decides how much scheduler jitter the suite tolerates before calling a
// working script broken.
//
// They were raised after TestPoolPump_RuntimeAccounting_TracksElapsedRunAndTurnover
// failed under `make test`'s parallel workspace load (see #435 "Problem 2"):
// init consumed ~7.7s of a 9s budget and the 3s post-event wait then elapsed
// before the script had processed the injected input event, producing
// "pump did not stop after water supply ON" — a timing verdict wearing a
// behavioural failure's clothing. The same test passes 3/3 in isolation.
const (
	// initTimeout bounds the async KVS/schedule init chain reaching the point
	// where the script has written schedule-mode.
	initTimeout = 25 * time.Second
	// eventTimeout bounds one injected event (input, MQTT, button) travelling
	// through the script to its resulting KVS write.
	eventTimeout = 10 * time.Second
)

// poolPumpRunContext bounds the script VM's lifetime. It is intentionally far
// larger than any single test's phase budgets: the waitFor() calls are what
// assert timing, and they produce precise failure messages. When this context
// is the binding deadline instead, the VM is killed mid-test and whatever
// assertion runs next reports a behavioural fault that never happened.
func poolPumpRunContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		2*time.Minute,
	)
}

func waitFor(deadline time.Duration, pollInterval time.Duration, pred func() bool) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if pred() {
			return true
		}
		time.Sleep(pollInterval)
	}
	return false
}

func shellyInputEvent(inputID int, state bool) []byte {
	event := map[string]interface{}{
		"info": map[string]interface{}{
			"component": fmt.Sprintf("input:%d", inputID),
			"id":        inputID,
			"state":     state,
		},
	}
	data, _ := json.Marshal(event)
	return data
}

func shellyButtonEvent() []byte {
	event := map[string]interface{}{
		"info": map[string]interface{}{
			"component": "sys",
			"event":     "sys_btn_push",
		},
	}
	data, _ := json.Marshal(event)
	return data
}

// pro1ComponentStatus returns component statuses for a Pro1 (1-switch) device.
func pro1ComponentStatus() map[string]interface{} {
	return map[string]interface{}{
		"switch:0": map[string]interface{}{"id": 0, "output": false},
		"input:0":  map[string]interface{}{"id": 0, "state": false},
		"input:1":  map[string]interface{}{"id": 1, "state": false},
		"mqtt":     map[string]interface{}{"connected": true},
		"sys":      map[string]interface{}{"device_id": "shellyplus1-b8d61a85a970"},
	}
}

// pro1KVS returns KVS for a Pro1 device. Same preferred ID as the mock device.
func pro1KVS() map[string]interface{} {
	return map[string]interface{}{
		"script/pool-pump/preferred":      "shellyplus1-b8d61a85a970",
		"script/pool-pump/pro3-id":        "shellypro3-aabbcc112233",
		"script/pool-pump/pro1-id":        "shellyplus1-b8d61a85a970",
		"script/pool-pump/logging":        "false",
		"script/pool-pump/speed":          "eco",
		"script/pool-pump/eco-speed":      "0",
		"script/pool-pump/mid-speed":      "1",
		"script/pool-pump/high-speed":     "2",
		"script/pool-pump/grace-delay":    "10000",
		"script/pool-pump/temp-threshold": "20",
	}
}

// pro1Schedules returns a minimal schedule set for Pro1 (just one pool schedule
// so verifySchedules passes).
func pro1Schedules() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"id": 1, "enable": true,
			"timespec": "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": "handleNightStart()"},
			}},
		},
	}
}

func TestPoolPump_InitVerifiesSchedules(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedules(),
	}

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	ok := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	cancel()
	<-done

	if !ok {
		t.Fatalf("timed out waiting for init to complete")
	}
}

// poolPumpSchedulesWrapped is poolPumpSchedules with every job's code field
// passed through poolPumpWrapScheduleCall — the shape a real device carries
// once createSchedules() has registered #480-wrapped jobs.
func poolPumpSchedulesWrapped() []map[string]interface{} {
	scriptID := 1
	wrap := func(code string) string { return poolPumpWrapScheduleCall(code) }
	return []map[string]interface{}{
		{
			"id": 1, "enable": true,
			"timespec": "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": wrap("handleDailyCheck()")},
			}},
		},
		{
			"id": 2, "enable": true,
			"timespec": "@sunrise+3h * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": wrap("handleMorningStart()")},
			}},
		},
		{
			"id": 3, "enable": true,
			"timespec": "@sunset * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": wrap("handleEveningStop()")},
			}},
		},
		{
			"id": 4, "enable": true,
			"timespec": "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": wrap("handleNightStart()")},
			}},
		},
		{
			"id": 5, "enable": true,
			"timespec": "0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": scriptID, "code": wrap("handleNightStop()")},
			}},
		},
	}
}

// TestPoolPump_InitVerifiesWrappedSchedules is TestPoolPump_InitVerifiesSchedules
// with #480-wrapped schedule code. verifySchedules() is the last of init's four
// steps (finishContinueInit()'s initSteps) and init stalls forever if it fails
// to recognize the wrapped jobs, so this fails by timeout if the substring
// match in verifySchedules() regresses back to the old prefix-equality check.
func TestPoolPump_InitVerifiesWrappedSchedules(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedulesWrapped(),
	}

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	ok := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	cancel()
	<-done

	if !ok {
		t.Fatalf("timed out waiting for init to complete with wrapped schedule code -- " +
			"verifySchedules() likely regressed from substring match back to exact/prefix match")
	}
}

// TestPoolPump_WaterSupplyRestoresSpeed asserts that water-supply protection
// removes a running speed and that the speed comes back when protection
// clears.
//
// #476 changed WHAT comes back. This test used to boot a Pro3 with switch:2
// already on, outside any run window, and assert it was still on after init —
// i.e. that a remembered boot-time relay state outranks the run window. Under
// the level-triggered reconciler it does not: a restart re-derives the relay
// from the policy, which is the whole point of #421/#441 (see also
// TestPoolPump_ScheduleRewriteAwayStopsPump, which already pinned "a running
// pump whose window moves away must stop"). The premise, not the intent, is
// what changed, so the test now establishes the running speed the way a real
// device does — from a run window that contains "now" — and asserts the same
// protect/restore behaviour against it.
func TestPoolPump_WaterSupplyRestoresSpeed(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)

	// "max" speed must map to switch 2 unambiguously. controllerKVS() only
	// sets eco-speed (to 0); day-speed/max-speed fall back to their schema
	// defaults (1 and 0), which collides eco and max on switch 0 — pin
	// max-speed explicitly so the policy wants switch:2 whenever the window
	// contains now, matching this test's assertions below.
	kvs := controllerKVS()
	kvs["script/pool-pump/speed"] = "max"
	kvs["script/pool-pump/max-speed"] = "2"

	// handleDailyCheck() runs automatically at the end of init (#476: it must
	// still run under water-supply protection, see the comment on
	// handleDailyCheck() itself) and, given a reachable forecast URL, its
	// Open-Meteo fetch can succeed against the real network in a sandbox with
	// internet access — silently rewriting the run window mid-test via
	// updateScheduleMode(), racing the assertions below (observed live
	// 2026-08-12: it rewrote the window to 15:09-18:51 and stopped a pump
	// this test had just started). A local server returning a body with no
	// "hourly" field makes onForecast() bail with "Invalid forecast
	// structure" and getMaxForecastTemp() stay null, so
	// decideModeFromForecast() returns before touching the window
	// (pool-pump.js:2430-2436) — deterministic regardless of what network
	// access the test environment happens to have.
	brokenForecast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(brokenForecast.Close)

	now := time.Now()
	deviceState := &script.DeviceState{
		KVS: kvs,
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  brokenForecast.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSummerSchedules(now.Add(-1*time.Hour), now.Add(1*time.Hour)),
		EventInjector:   injector,
	}

	// 20s ceiling: init (up to 9s) + two water-supply transitions (up to 5s
	// each, see below) — generous headroom, not the expected runtime.
	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("script did not complete init within timeout")
	}

	// The window contains "now", so the reconciler starts the pump at the
	// preferred speed without any event at all (#441: nothing used to ask
	// "should I be running?" unless a schedule rewrite happened to move).
	if !waitFor(eventTimeout, 100*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "2"
	}) {
		cancel()
		<-done
		t.Fatalf("expected the reconciler to start switch:2 inside the run window, got %v",
			kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// 5s (not 2s): activateOutput() now also queues the #402 runtime/turnover
	// KVS mirror writes (stopRuntimeAccounting/startRuntimeAccounting), which
	// adds two more serialized 200ms task-queue ticks ahead of this
	// transition's own active-output write — comfortably inside 2s when
	// idle, but this run's own full `make test` observed it slip past 2s
	// under concurrent-package CI load (see AGENTS.md "stress-test before
	// pushing timing-sensitive tests").
	injector <- shellyInputEvent(0, true)
	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	})
	if !stopped {
		t.Fatalf("pump did not stop after water supply ON; active-output = %v",
			kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	injector <- shellyInputEvent(0, false)
	restored := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "2"
	})

	cancel()
	<-done

	if !restored {
		t.Fatalf("pump speed not restored after water supply OFF; active-output = %v",
			kvsValue(deviceState, "script/pool-pump/active-output"))
	}
}

// TestPoolPump_ButtonCyclesPro3 verifies that sys_btn_push events cycle
// through speeds: off → 0 → 1 → 2 → off (the last transition exercises turnOffAllSwitches).
//
// #479 review: this test flaked CI-only (never locally) with "button press 4:
// expected active-output=-1, got 2" after ~19.85s -- eventTimeout (10s)
// expiring. Root cause was NOT in the reconciler: pool-pump.js's daily
// forecast fetch (Shelly.call("HTTP.GET", {url: api.open-meteo.com...}))
// runs on the exact same task queue as the button-cycle chain, and the
// emulator used to make that a real, synchronous, BLOCKING network call on
// the single goroutine driving the whole script's event loop -- so a slow
// real network round trip (intermittent on CI, essentially never on a fast
// home connection) stalled every other event, including this test's own
// active-output write. Fixed in pkg/shelly/script/run.go by making the
// emulator's "http.get" async like Timer.set/MQTT.subscribe already are --
// see that file's "http.get" method for the full writeup and reproduction.
func TestPoolPump_ButtonCyclesPro3(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 8)

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedules(),
		EventInjector:   injector,
	}

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	// Wait for init.
	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Start from off.
	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("expected active-output=-1 before button presses, got %v", v)
	}

	// Press 1: off → 0
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	}) {
		t.Fatalf("button press 1: expected active-output=0, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Press 2: 0 → 1
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "1"
	}) {
		t.Fatalf("button press 2: expected active-output=1, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Press 3: 1 → 2
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "2"
	}) {
		t.Fatalf("button press 3: expected active-output=2, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Press 4: 2 → off (exercises turnOffAllSwitches)
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	}) {
		t.Fatalf("button press 4: expected active-output=-1, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Verify all switches are off.
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("switch:%d", i)
		if entry, ok := componentStatusValue(deviceState, key); ok {
			if m, ok := entry.(map[string]interface{}); ok {
				if on, _ := m["output"].(bool); on {
					t.Errorf("switch %d still on after cycling to off", i)
				}
			}
		}
	}

	cancel()
	<-done
}

// TestPoolPump_Pro1ToggleAndWaterSupply verifies Pro1 behaviour:
// init, button toggle on/off, and water supply protection with restore.
func TestPoolPump_Pro1ToggleAndWaterSupply(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 8)

	deviceState := &script.DeviceState{
		KVS:             pro1KVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro1ComponentStatus(),
		Schedules:       pro1Schedules(),
		EventInjector:   injector,
	}

	// 25s ceiling: init (up to 9s) + three button toggles (up to 2s each,
	// unaffected by #402 — cycleOutputs() bypasses activateOutput()) + two
	// water-supply transitions (up to 5s each, see below) — generous
	// headroom, not the expected runtime.
	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	// Wait for init.
	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("Pro1 init timeout")
	}

	// Should start off.
	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("Pro1: expected active-output=-1 after init, got %v", v)
	}

	// Button press: toggle ON
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	}) {
		t.Fatalf("Pro1 toggle on: expected active-output=0, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// #476: the middle "toggle OFF then ON again" pair that used to sit here
	// has been removed, and its removal is the point. Button presses now go
	// through the single actuator, so they record fuse changes like every
	// other actuation — which they did NOT before (#475 defect 1: a
	// button-driven run bypassed both the fuse and runtime accounting). Five
	// relay changes inside ten seconds legitimately trips the anti-cycling
	// fuse (4 changes / 2 min), so the old sequence now ends with the restore
	// refused. That is correct behaviour, not a regression; the toggle-off
	// path is covered by TestPoolPump_ButtonCyclesPro3, and the fuse now
	// covering button presses is asserted by
	// TestPoolPump_FuseRefusesButtonOnAfterRapidCycling.

	// Water supply ON → should turn off. 5s (not 2s): activateOutput() now
	// also queues the #402 runtime/turnover KVS mirror writes ahead of this
	// transition's own active-output write — see the comment in
	// TestPoolPump_WaterSupplyRestoresSpeed.
	injector <- shellyInputEvent(0, true)
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	}) {
		t.Fatalf("Pro1 water supply ON: expected active-output=-1, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Water supply OFF → should restore switch:0.
	injector <- shellyInputEvent(0, false)
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	}) {
		t.Fatalf("Pro1 water supply OFF: expected active-output=0, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	cancel()
	<-done
}

// === Water-supply flap re-entrancy regression (#450) ===
//
// The live crash trace (issue #450, 2026-08-11 18:42 CEST comment) showed
// input:0 flapping faster than a Switch.Set round-trip: handleWaterSupply
// was re-entered from a fresh handleInputEvent while the PREVIOUS
// transition's Shelly.call callback had not yet fired, nesting a second
// activateOutput()/setOutput() chain inside the first and eventually
// overflowing the interpreter stack on real hardware ("Too much recursion
// ... in function acquireCallSlot").
//
// The emulator's default event loop (a Go `select` over EventInjector, see
// run.go) cannot reproduce that interleaving on its own: it always runs one
// event's JS handler fully to completion — including every synchronous
// nested Shelly.call/callback chain — before dequeuing the next, so two
// events sent back-to-back on the injector are processed strictly in
// sequence, never nested. That is a real emulator fidelity gap: without a
// change here, no test could ever exercise this race (matching the
// class of gap already on record for this file — missing Schedule.Update,
// Switch.Set emitting no events, KVS.List's signature, unsynchronised state
// maps).
//
// DeviceState.SetPendingNestedEvent (pkg/shelly/script/device_state.go)
// closes that gap: it arms the emulator's Switch.Set emulation to deliver
// one more raw device event to the script's event handlers SYNCHRONOUSLY,
// nested inside the very Switch.Set call the script issued — exactly where
// the live crash trace shows the second event landing.
//
// Before the #450 fix, handleWaterSupply called activateOutput()
// unconditionally on every event. With the nested event landing inside the
// first Switch.Set, this reproduces the documented symptom
// without needing a literal stack overflow (goja's Go-native call stack
// does not overflow at this shallow a nesting depth the way Espruino's
// interpreter stack does): the two nested completions race to write
// STATE.activeOutput/KVS active-output, and whichever happens to unwind
// LAST wins — which is not necessarily the one matching the physical
// Switch.Set applied last, so the script's own bookkeeping ends up out of
// sync with the hardware it just set. After the fix, the second event is
// deferred instead of acted on immediately (no nested activateOutput() call
// happens at all), and the coalesced follow-up transition — dispatched via
// queueTask once the first settles — leaves output in the state matching
// the LAST observed input, with bookkeeping and hardware agreeing.

// poolPumpWaterSupplyFlapDeviceState returns a Pro1 DeviceState (single
// output, the device type on which #450 was observed live) wired up for
// event injection.
func poolPumpWaterSupplyFlapDeviceState(injector chan []byte) *script.DeviceState {
	return &script.DeviceState{
		KVS:             pro1KVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro1ComponentStatus(),
		Schedules:       pro1Schedules(),
		EventInjector:   injector,
	}
}

// assertSwitchOutput fails the test if switch:0's physical output does not
// match want. Used to check the #450 symptom directly: the script's own
// active-output bookkeeping diverging from the hardware state it just set.
func assertSwitchOutput(t *testing.T, deviceState *script.DeviceState, want bool, context string) {
	t.Helper()
	entry, ok := componentStatusValue(deviceState, "switch:0")
	if !ok {
		t.Errorf("%s: switch:0 component status missing", context)
		return
	}
	m, ok := entry.(map[string]interface{})
	if !ok {
		t.Errorf("%s: switch:0 component status not a map: %#v", context, entry)
		return
	}
	on, _ := m["output"].(bool)
	if on != want {
		t.Errorf("%s: switch:0 physically %v, want %v — script bookkeeping diverged from hardware state (the #450 race)", context, on, want)
	}
}

// TestPoolPump_WaterSupplyFlapFalseThenTrue drives input:0 false -> true
// (restore starts, then protection re-engages before the restore's
// Switch.Set completes) and asserts the pump ends OFF, matching the LAST
// observed input (true = protected) — not the first (false = clear), and
// not some interleaved mix of the two.
func TestPoolPump_WaterSupplyFlapFalseThenTrue(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 8)
	deviceState := poolPumpWaterSupplyFlapDeviceState(injector)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	if !waitPoolPumpInit(t, deviceState) {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Get the pump running so there is something to protect and restore.
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	}) {
		cancel()
		<-done
		t.Fatalf("setup: pump did not start, active-output = %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Enter protection normally (no flap yet) so there is a saved_output to
	// restore from.
	injector <- shellyInputEvent(0, true)
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	}) {
		cancel()
		<-done
		t.Fatalf("setup: pump did not stop for protection, active-output = %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Arm the nested event: while the upcoming restore's Switch.Set is in
	// flight, deliver a SECOND input:0 event (back to true / protect)
	// nested inside it, before the restore's own callback fires. This is
	// the live crash's exact shape: a restore in flight, protection
	// re-arriving before it completes.
	deviceState.SetPendingNestedEvent(shellyInputEvent(0, true))

	// Fire the flap: input:0 -> false (restore starts; the armed nested
	// event above fires the second leg from inside the restore's own
	// Switch.Set call).
	injector <- shellyInputEvent(0, false)

	// This flap starts AND ends "protected" (active-output=-1 both before
	// and after), matching the live incident exactly: input:0 dipped low
	// and came straight back high. That means a plain waitFor() for
	// active-output==-1 would return on its very FIRST poll — matching the
	// stale pre-flap value — without ever giving the queued follow-up
	// transition (dispatched via queueTask once the in-flight one settles)
	// a chance to run, so it would pass unconditionally regardless of
	// whether the fix works. Settle explicitly instead: several task-queue
	// ticks (200ms each) is enough for the synchronous nested chain plus
	// every queued follow-up to fully drain in both the fixed and the
	// unfixed script.
	settlePoolPumpTaskQueue(t)

	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "-1" {
		cancel()
		<-done
		t.Fatalf("false->true flap: expected active-output=-1 (matching last input=protected) after settling, got %v — "+
			"without the #450 fix this settles on the WRONG value because the nested transition's completion "+
			"races the outer one instead of being deferred",
			v)
	}

	assertSwitchOutput(t, deviceState, false, "false->true flap")

	cancel()
	<-done
}

// TestPoolPump_WaterSupplyFlapTrueThenFalse drives input:0 true -> false
// (protection starts, then clears before the protect's Switch.Set
// completes) and asserts the pump ends restored, matching the LAST observed
// input (false = clear) — not the first (true = protected).
func TestPoolPump_WaterSupplyFlapTrueThenFalse(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 8)
	deviceState := poolPumpWaterSupplyFlapDeviceState(injector)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	if !waitPoolPumpInit(t, deviceState) {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Get the pump running: this becomes savedOutput once protection
	// engages below, and must be what gets restored at the end.
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	}) {
		cancel()
		<-done
		t.Fatalf("setup: pump did not start, active-output = %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Arm the nested event: while the upcoming protect's Switch.Set is in
	// flight, deliver a SECOND input:0 event (back to false / clear) nested
	// inside it, before the protect's own callback fires.
	deviceState.SetPendingNestedEvent(shellyInputEvent(0, false))

	// Fire the flap: input:0 -> true (protection starts; the armed nested
	// event above fires the second leg from inside the protect's own
	// Switch.Set call).
	injector <- shellyInputEvent(0, true)

	// As in the false->true test above, this flap starts AND ends
	// "restored" (active-output=0 both before and after) — a plain
	// waitFor() for active-output==0 would match the stale pre-flap value
	// on its first poll and never observe whether the queued follow-up
	// transition actually ran (or ran correctly). Settle explicitly first.
	settlePoolPumpTaskQueue(t)

	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "0" {
		cancel()
		<-done
		t.Fatalf("true->false flap: expected active-output=0 (matching last input=clear) after settling, got %v — "+
			"without the #450 fix this settles on the WRONG value because the nested transition's completion "+
			"races the outer one instead of being deferred",
			v)
	}

	assertSwitchOutput(t, deviceState, true, "true->false flap")

	cancel()
	<-done
}

// settlePoolPumpTaskQueue waits long enough for pool-pump.js's TASK_QUEUE
// (a single 200ms-period Timer, see queueTask() in pool-pump.js) to fully
// drain a multi-step follow-up chain: the deferred transition itself, plus
// its own saveState() write, is at most a handful of queued tasks deep.
// Generous rather than tight, like initTimeout/eventTimeout elsewhere in
// this file — the point of the flap tests below is to observe the SETTLED
// final state, not the first state a poll happens to see, so a fixed wait
// is used instead of waitFor().
func settlePoolPumpTaskQueue(t *testing.T) {
	t.Helper()
	time.Sleep(2 * time.Second)
}

// === Runtime/turnover tracking (#402) ===
//
// controllerKVS() sets preferred speed "eco" and doesn't override
// pool-volume/max-flow-rate/max-rpm/eco-rpm, so pool-pump.js's CONFIG_SCHEMA
// defaults apply: maxFlowRate=31, maxRpm=2900, ecoRpm=2000, poolVolume=46.
// These mirror computeFlowRate()/computeTurnoverToday() in pool-pump.js so
// tests can independently recompute the expected persisted values.
const (
	testMaxFlowRate = 31.0
	testMaxRpm      = 2900.0
	testEcoRpm      = 2000.0
	testPoolVolume  = 46.0
)

func expectedFlowRateM3PerHour() float64 {
	return testMaxFlowRate * (testEcoRpm / testMaxRpm)
}

// expectedTurnover mirrors computeTurnoverToday(sec) in pool-pump.js.
func expectedTurnover(sec float64) float64 {
	t := (sec / 3600) * expectedFlowRateM3PerHour() / testPoolVolume
	return math.Round(t*100) / 100
}

// dateString mirrors todayDateString() in pool-pump.js ("YYYY-M-D", no
// zero-padding) for an arbitrary time, so tests can seed Script.storage with
// both "today" and a stale ("yesterday") value.
func dateString(t time.Time) string {
	return fmt.Sprintf("%d-%d-%d", t.Year(), int(t.Month()), t.Day())
}

func parseKVSInt(t *testing.T, deviceState *script.DeviceState, key string) int64 {
	t.Helper()
	raw, ok := deviceState.KVSValue(key)
	if !ok {
		t.Fatalf("KVS key %q not present", key)
	}
	s, _ := raw.(string)
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		t.Fatalf("KVS key %q = %v is not an int: %v", key, raw, err)
	}
	return v
}

func parseKVSFloat(t *testing.T, deviceState *script.DeviceState, key string) float64 {
	t.Helper()
	raw, ok := deviceState.KVSValue(key)
	if !ok {
		t.Fatalf("KVS key %q not present", key)
	}
	s, _ := raw.(string)
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
		t.Fatalf("KVS key %q = %v is not a float: %v", key, raw, err)
	}
	return v
}

// TestPoolPump_RuntimeAccounting_TracksElapsedRunAndTurnover verifies the
// #402 accumulator: the pump is already "on" at boot (enforceOutputState()
// resumes accounting per #402 point 6), runs for a short real interval, and
// is stopped via water-supply protection — the same activateOutput() choke
// point doStart/doStop use. Asserts script/pool-pump/runtime-sec reflects
// the elapsed time and turnover-today is consistent with computeFlowRate()'s
// output for the configured eco speed/RPMs.
//
// (The test harness has no way to invoke doStart()/doStop() directly from Go
// — there is no cron emulation and RunWithDeviceState doesn't expose the
// running goja VM — so this uses the water-supply on/off transition as the
// stand-in start/stop trigger; both paths go through the same
// activateOutput() hook being tested.)
func TestPoolPump_RuntimeAccounting_TracksElapsedRunAndTurnover(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)

	cs := pro3ComponentStatus()
	cs["switch:2"] = map[string]interface{}{"id": 2, "output": true} // eco speed already running at boot

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: cs,
		Schedules:       poolPumpSchedules(),
		EventInjector:   injector,
	}

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// enforceOutputState() should have detected the already-running pump and
	// started runtime accounting automatically (#402 point 6).
	runStart := time.Now()

	// Let it run for a short, real interval before stopping it.
	time.Sleep(2 * time.Second)

	// Water supply ON stops the pump: activateOutput(-1, ...) -> stopRuntimeAccounting().
	injector <- shellyInputEvent(0, true)
	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	})
	elapsed := time.Since(runStart)
	cancel()
	<-done

	if !stopped {
		t.Fatalf("pump did not stop after water supply ON; active-output = %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	runtimeSec := parseKVSInt(t, deviceState, "script/pool-pump/runtime-sec")
	if runtimeSec <= 0 {
		t.Fatalf("expected positive runtime-sec, got %d", runtimeSec)
	}
	// Generous slack for scheduler/test jitter — runtime-sec must not exceed
	// (by more than a couple seconds) the real wall-clock time we measured.
	if float64(runtimeSec) > elapsed.Seconds()+2 {
		t.Errorf("runtime-sec %d exceeds measured elapsed %.1fs by more than slack", runtimeSec, elapsed.Seconds())
	}

	turnoverToday := parseKVSFloat(t, deviceState, "script/pool-pump/turnover-today")
	wantTurnover := expectedTurnover(float64(runtimeSec))
	if math.Abs(turnoverToday-wantTurnover) > 0.01 {
		t.Errorf("turnover-today = %v, want ~%v (runtime_sec=%d, flow_rate=%.4f m3/h)",
			turnoverToday, wantTurnover, runtimeSec, expectedFlowRateM3PerHour())
	}
}

// TestPoolPump_RuntimeAccounting_ContinuesAfterReboot simulates a script/device
// restart while the pump was left running: Script.storage is pre-seeded with
// a runtime baseline for *today*, and the pump is already "on" in
// ComponentStatus (as if the process just restarted but the hardware kept
// running). Asserts runtime-sec continues accumulating from the persisted
// baseline instead of resetting to 0 — the crash/reboot resilience #402
// point 6 exists for.
// runPoolPumpPhase starts pool-pump.js against deviceState in the
// background and returns once init has completed (schedule-mode written to
// KVS), leaving the script running. Call the returned stop() to cancel the
// script and wait for it to fully exit before starting a second phase
// against the *same* deviceState — reusing the same DeviceState pointer
// across two phases is what makes a two-phase test a genuine restart:
// Script.storage isn't reset between phases, only whatever the running
// script itself wrote to it, exactly like Script.Stop/Script.Start on a
// real device reusing the same on-flash storage.
func runPoolPumpPhase(t *testing.T, buf []byte, deviceState *script.DeviceState) (stop func()) {
	t.Helper()
	// KVS (unlike Storage) is deliberately NOT reset between phases either —
	// but that means "schedule-mode" is already present in deviceState.KVS
	// the instant a second phase starts, left over from the previous phase's
	// own saveState() write. Left alone, the waitFor below would return
	// immediately on a stale key instead of waiting for *this* run's init to
	// reach the same point, racing the very state restore under test. Clear
	// it first so the predicate can only be satisfied by a fresh write.
	deviceState.DeleteKVSValue("script/pool-pump/schedule-mode")

	ctx, cancel := poolPumpRunContext(t)
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()
	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}
	return func() {
		cancel()
		<-done
	}
}

// runtimeStorageState mirrors the JSON object pool-pump.js persists at
// Script.storage["runtime"] (see STORAGE_KEYS.runtime, #469): {sec, ts}.
type runtimeStorageState struct {
	Sec float64 `json:"sec"`
	Ts  float64 `json:"ts"`
}

// readRuntimeStorage reads and JSON-decodes Script.storage["runtime"],
// failing the test if it is absent, not a string (Script.storage only ever
// holds strings — Web Storage semantics), or not valid JSON. This is the
// "assert the value and type come back intact" check #469 asked for: unlike
// the pre-#469 loose scalars, a malformed round trip fails loudly here
// instead of silently coercing to 0/null.
func readRuntimeStorage(t *testing.T, d *script.DeviceState) runtimeStorageState {
	t.Helper()
	raw, ok := d.StorageValue("runtime")
	if !ok {
		t.Fatalf(`Script.storage["runtime"] not present`)
	}
	s, ok := raw.(string)
	if !ok {
		t.Fatalf(`Script.storage["runtime"] = %v (%T), want a string (Web Storage semantics)`, raw, raw)
	}
	var out runtimeStorageState
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf(`Script.storage["runtime"] = %q is not valid JSON: %v`, s, err)
	}
	return out
}

// seedRuntimeStorage directly overwrites Script.storage["runtime"] between
// two runPoolPumpPhase phases, to force a scenario a real clock can't
// produce inside a unit test (a specific past day, or corruption).
func seedRuntimeStorage(d *script.DeviceState, sec float64, ts int64) {
	raw, _ := json.Marshal(runtimeStorageState{Sec: sec, Ts: float64(ts)})
	d.SetStorageValue("runtime", string(raw))
}

// localDayNumber mirrors pool-pump.js's localDayNumber(epochSec): a
// comparable YYYYMMDD integer in local time, so Go-side assertions about
// "which day" a persisted ts belongs to use the same representation as the
// script itself, rather than a separately-formatted date string.
func localDayNumber(epochSec int64) int64 {
	tm := time.Unix(epochSec, 0)
	return int64(tm.Year())*10000 + int64(tm.Month())*100 + int64(tm.Day())
}

// TestPoolPump_RuntimeAccounting_ContinuesAfterReboot is the storage
// round-trip test #469 asked for, and a rewrite of the pre-#469 test of the
// same name: that version seeded Script.storage directly with Go-string
// values, bypassing the script's own storeStorageValue()/loadStorageValue()
// write-read path entirely — which is exactly why it kept passing while the
// bug was live, and why #469 asked for it to be fixed, not just supplemented.
// This version does NOT seed Script.storage directly: phase 1 runs the pump
// for real and lets its own stop-event handler persist a nonzero runtime
// through the actual storeStorageValue()/JSON.stringify code path; phase 2
// is a genuine second script instance (new goja VM, same DeviceState) that
// must restore that exact value through loadStorageObject()/JSON.parse —
// proving the round trip is intact end to end, and that a same-day restart
// carries the count forward instead of resetting it.
func TestPoolPump_RuntimeAccounting_ContinuesAfterReboot(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)
	cs := pro3ComponentStatus()
	cs["switch:2"] = map[string]interface{}{"id": 2, "output": true} // pump on at boot

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: cs,
		Schedules:       poolPumpSchedules(),
		EventInjector:   injector,
	}

	// --- Phase 1: pump runs briefly, then a real water-supply-ON event
	// stops it, which calls stopRuntimeAccounting() -> persistRuntimeState()
	// -> the script's own storeStorageValue() write. ---
	stop1 := runPoolPumpPhase(t, buf, deviceState)
	time.Sleep(1200 * time.Millisecond)
	injector <- shellyInputEvent(0, true)
	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	})
	stop1()
	if !stopped {
		t.Fatalf("pump did not stop after water supply ON in phase 1; active-output = %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	phase1Sec := parseKVSInt(t, deviceState, "script/pool-pump/runtime-sec")
	if phase1Sec <= 0 {
		t.Fatalf("phase 1 KVS runtime-sec = %d, want > 0 (the real write path never ran)", phase1Sec)
	}
	phase1Storage := readRuntimeStorage(t, deviceState)
	if phase1Storage.Sec <= 0 {
		t.Fatalf(`phase 1 Script.storage["runtime"].sec = %v, want > 0`, phase1Storage.Sec)
	}

	// --- Phase 2: a fresh script instance (new VM) against the SAME
	// DeviceState. Water supply is still "on" so the pump stays off; this
	// isolates the restore itself from any further accumulation. ---
	cs2 := pro3ComponentStatus()
	cs2["input:0"] = map[string]interface{}{"id": 0, "state": true}
	deviceState.ComponentStatus = cs2

	stop2 := runPoolPumpPhase(t, buf, deviceState)
	defer stop2()

	restoredSec := parseKVSInt(t, deviceState, "script/pool-pump/runtime-sec")
	if restoredSec < phase1Sec {
		t.Fatalf("phase 2 KVS runtime-sec = %d, want >= phase 1's %d (round trip lost data, #469)", restoredSec, phase1Sec)
	}

	restoredStorage := readRuntimeStorage(t, deviceState)
	if restoredStorage.Sec < phase1Storage.Sec {
		t.Fatalf(`phase 2 Script.storage["runtime"].sec = %v, want >= phase 1's %v (#469)`, restoredStorage.Sec, phase1Storage.Sec)
	}
	if localDayNumber(int64(restoredStorage.Ts)) != localDayNumber(time.Now().Unix()) {
		t.Fatalf(`phase 2 Script.storage["runtime"].ts = %v, want today`, restoredStorage.Ts)
	}
}

// TestPoolPump_RuntimeAccounting_PreviousDayRestartResets verifies the
// day-rollover reset using the current {sec, ts} format end to end across a
// genuine two-phase restart (unlike TestPoolPump_RuntimeAccounting_
// LegacyMigrationDiscardsStaleDate below, which single-run-seeds the
// pre-#469 legacy pair). Phase 1 persists a real baseline; between phases
// the test backdates Script.storage["runtime"].ts to yesterday — standing in
// for a device that sat idle overnight, since this Go harness can't fast-
// forward goja's Date() past a real midnight. Phase 2 must reset the count
// to 0 rather than carrying yesterday's total into today.
func TestPoolPump_RuntimeAccounting_PreviousDayRestartResets(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(), // pump off throughout
		Schedules:       poolPumpSchedules(),
	}

	stop1 := runPoolPumpPhase(t, buf, deviceState)
	stop1()

	yesterday := time.Now().AddDate(0, 0, -1)
	seedRuntimeStorage(deviceState, 43200, yesterday.Unix()) // 12h — clearly stale if carried over

	stop2 := runPoolPumpPhase(t, buf, deviceState)
	defer stop2()

	got := readRuntimeStorage(t, deviceState)
	if got.Sec != 0 {
		t.Errorf(`Script.storage["runtime"].sec after cross-day restart = %v, want 0 (stale total must not carry over, #469)`, got.Sec)
	}
	if localDayNumber(int64(got.Ts)) != localDayNumber(time.Now().Unix()) {
		t.Errorf(`Script.storage["runtime"].ts after cross-day restart = %v, want today`, got.Ts)
	}
}

// TestPoolPump_RuntimeAccounting_CorruptStorageFallsBackToKVS exercises the
// KVS recovery path (#469 design point 5): phase 1 persists a real baseline
// to both Script.storage and its KVS mirrors (runtime-sec, runtime-ts).
// Between phases, Script.storage["runtime"] is corrupted (not valid JSON) —
// standing in for a script reinstall wiping Script.storage while KVS,
// external to the script package, survives. Phase 2 must recover the KVS
// baseline instead of resetting to 0, exercising the async loadValueAsync/
// loadRuntimeStateFromKVS path that loadStorageObject() alone cannot reach.
func TestPoolPump_RuntimeAccounting_CorruptStorageFallsBackToKVS(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)
	cs := pro3ComponentStatus()
	cs["switch:2"] = map[string]interface{}{"id": 2, "output": true}

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: cs,
		Schedules:       poolPumpSchedules(),
		EventInjector:   injector,
	}

	stop1 := runPoolPumpPhase(t, buf, deviceState)
	time.Sleep(1200 * time.Millisecond)
	injector <- shellyInputEvent(0, true)
	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	})
	stop1()
	if !stopped {
		t.Fatalf("pump did not stop after water supply ON in phase 1")
	}

	kvsSecBefore := parseKVSInt(t, deviceState, "script/pool-pump/runtime-sec")
	if kvsSecBefore <= 0 {
		t.Fatalf("phase 1 KVS runtime-sec = %d, want > 0", kvsSecBefore)
	}
	if _, ok := deviceState.KVSValue("script/pool-pump/runtime-ts"); !ok {
		t.Fatalf("phase 1 did not write KVS runtime-ts")
	}

	deviceState.SetStorageValue("runtime", "not valid json")

	cs2 := pro3ComponentStatus()
	cs2["input:0"] = map[string]interface{}{"id": 0, "state": true}
	deviceState.ComponentStatus = cs2

	stop2 := runPoolPumpPhase(t, buf, deviceState)
	defer stop2()

	// applyRuntimeState() re-persists to Script.storage synchronously as
	// part of the same init chain runPoolPumpPhase already waited for
	// (finishLoadState/onDone only fires after the KVS fallback resolves),
	// so this is safe to read immediately.
	recovered := readRuntimeStorage(t, deviceState)
	if recovered.Sec < float64(kvsSecBefore) {
		t.Fatalf(`after corrupt Script.storage, recovered Script.storage["runtime"].sec = %v, want >= phase 1's KVS baseline %d (KVS fallback must carry forward, not reset, #469)`, recovered.Sec, kvsSecBefore)
	}
}

// TestPoolPump_RuntimeAccounting_LegacyMigrationDiscardsStaleDate verifies
// migrateLegacyRuntimeState()'s one-time upgrade path for a pre-#469 device
// that only has the old loose-scalar pair (runtime-sec + runtime-date), in
// the specific case where that legacy date is stale: as if the device was
// left running overnight and only rebooted the next day (or the daemon
// never stopped it before midnight). At boot, the migrated total must be
// discarded (reset to 0) rather than carried into today.
//
// This is a single-run, directly-seeded test (unlike the two-phase tests
// above) because it is specifically about migrating a value the *test*
// plants in the legacy format, not about round-tripping through the
// script's own write path — migrateLegacyRuntimeState() only ever reads
// runtime-sec/runtime-date, it never writes them again once migrated.
//
// The exact mid-run rollover interleaving (ensureRuntimeDay() pulling
// STATE.runStartTs forward to Date.now() so a still-open run doesn't
// re-credit its pre-midnight time to the new day) isn't independently
// reproducible from this Go harness: Date.now()/Timer.set are tied to the
// real system clock with no injectable virtual clock, so there's no
// deterministic way to force a stale restored ts while STATE.runStartTs is
// non-null without a real device restart in between (which always goes
// through this same boot-time reset path).
func TestPoolPump_RuntimeAccounting_LegacyMigrationDiscardsStaleDate(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	deviceState := &script.DeviceState{
		KVS: controllerKVS(),
		Storage: map[string]interface{}{
			"runtime-sec":  "43200", // 12h — clearly stale if carried over
			"runtime-date": dateString(time.Now().AddDate(0, 0, -1)),
		},
		ComponentStatus: pro3ComponentStatus(), // pump off at boot
		Schedules:       poolPumpSchedules(),
	}

	stop := runPoolPumpPhase(t, buf, deviceState)
	stop()

	got := readRuntimeStorage(t, deviceState)
	if got.Sec != 0 {
		t.Errorf(`Script.storage["runtime"].sec after migrating a stale legacy date = %v, want 0 (stale total must not carry over)`, got.Sec)
	}
	if localDayNumber(int64(got.Ts)) != localDayNumber(time.Now().Unix()) {
		t.Errorf(`Script.storage["runtime"].ts after migrating a stale legacy date = %v, want today`, got.Ts)
	}
}

// TestPoolPump_RuntimeAccounting_AnchorDoesNotDriftAcrossCheckpoints is the
// regression test for #502's actual root cause: persistRuntimeState() used
// to re-stamp Script.storage["runtime"].ts with Date.now() on EVERY call --
// including flushRuntimeCheckpoint()'s 60s-recurring checkpoint while the
// pump runs -- rather than persisting the unchanged day anchor
// (STATE.runtimeTs). That is exactly how a total accumulated over several
// idle days on `mezzanine` kept looking freshly written: ts always read
// "just now" because every persist stamped it fresh, never the day the sec
// total actually started accruing for.
//
// Unlike the round-1 regression test (which pinned STATE directly and read
// a downstream decision, bypassing the persist path entirely), this test
// calls the REAL flushRuntimeCheckpoint() -- the exact production call site
// -- repeatedly at different real wall-clock instants and asserts
// Script.storage["runtime"].ts stays pinned to the anchor set at boot,
// rather than drifting toward whatever moment each checkpoint happened to
// run at.
func TestPoolPump_RuntimeAccounting_AnchorDoesNotDriftAcrossCheckpoints(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	evalCh := make(chan []byte, 4)
	cs := pro3ComponentStatus()
	cs["switch:2"] = map[string]interface{}{"id": 2, "output": true} // pump on at boot -> real startRuntimeAccounting()

	deviceState := &script.DeviceState{
		KVS:                  controllerKVS(),
		Storage:              make(map[string]interface{}),
		ComponentStatus:      cs,
		Schedules:            poolPumpSchedules(),
		ScheduleEvalInjector: evalCh,
	}

	stop := runPoolPumpPhase(t, buf, deviceState)
	defer stop()

	// Boot itself already performed one real persistRuntimeState() call
	// (applyRuntimeState() at load); this is the anchor every later
	// checkpoint must keep agreeing with.
	anchorTs := int64(readRuntimeStorage(t, deviceState).Ts)

	for i := 0; i < 3; i++ {
		time.Sleep(300 * time.Millisecond) // a real, different wall-clock instant each time
		if err := evalPoolPumpSchedule(deviceState, "flushRuntimeCheckpoint()"); err != nil {
			t.Fatalf("checkpoint %d: failed to inject: %v", i, err)
		}
		time.Sleep(100 * time.Millisecond) // let the injected eval land
		got := readRuntimeStorage(t, deviceState)
		if int64(got.Ts) != anchorTs {
			t.Fatalf("checkpoint %d: anchor drifted from %v to %v (now=%v) -- ts must stay pinned to the "+
				"day it was set, not the time of the last write (#502)", i, anchorTs, got.Ts, time.Now().Unix())
		}
	}
}

// TestPoolPump_RuntimeAccounting_StopRightAfterMidnightDoesNotCreditWholeRun
// is the regression test for the second #502-class gap found while building
// this test: stopRuntimeAccounting() -- unlike its siblings
// startRuntimeAccounting() and flushRuntimeCheckpoint() -- never called
// ensureRuntimeDay(). A stop landing exactly at a day boundary, before the
// next 60s flushRuntimeCheckpoint() tick gets a chance to split pre/post-
// midnight time, would blindly add the WHOLE elapsed span (spanning both
// yesterday and today) to "today's" total.
//
// A single continuous run with its 60s checkpoint ticking normally through
// a real midnight is NOT actually broken by either bug: reconcileRuntimeState()
// compares calendar days, not elapsed duration, so an in-day anchor drift
// never fools it, and the very next tick after midnight always detects the
// rollover correctly regardless of exactly when within the previous day the
// anchor was last touched. The genuine failure mode is specifically "stop
// fires before any checkpoint had a chance to see the new day" -- which
// this test reproduces by stopping the pump immediately after pinning a
// pre-midnight state, with no intervening flushRuntimeCheckpoint() call.
//
// This harness cannot fast-forward goja's Date()/Timer.set past a real
// midnight (same limitation noted on
// TestPoolPump_RuntimeAccounting_LegacyMigrationDiscardsStaleDate), so it
// pins the two markers a real midnight crossing would leave stale
// (STATE.runtimeTs, STATE.runStartTs) via ScheduleEvalInjector, then stops
// the pump for real -- exercising the actual, production stopRuntimeAccounting()
// -> persistRuntimeState() call chain, not a Go-side Storage overwrite.
func TestPoolPump_RuntimeAccounting_StopRightAfterMidnightDoesNotCreditWholeRun(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	evalCh := make(chan []byte, 4)
	injector := make(chan []byte, 4)
	cs := pro3ComponentStatus()
	cs["switch:2"] = map[string]interface{}{"id": 2, "output": true} // pump "running since before midnight"

	deviceState := &script.DeviceState{
		KVS:                  controllerKVS(),
		Storage:              make(map[string]interface{}),
		ComponentStatus:      cs,
		Schedules:            poolPumpSchedules(),
		EventInjector:        injector,
		ScheduleEvalInjector: evalCh,
	}

	stop := runPoolPumpPhase(t, buf, deviceState)
	defer stop()

	// Stand in for "this run started yesterday and had accrued 20000s by
	// the moment a real midnight passed, with no checkpoint tick in
	// between": pin the anchor and the open interval's start marker to
	// yesterday.
	yesterday := time.Now().AddDate(0, 0, -1)
	pin := fmt.Sprintf("STATE.runtimeTodaySec = 20000; STATE.runtimeTs = %d; STATE.runStartTs = %d;",
		yesterday.Unix(), yesterday.UnixMilli())
	if err := evalPoolPumpSchedule(deviceState, pin); err != nil {
		t.Fatalf("failed to inject pre-midnight state: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	// Stop the pump for real, immediately -- no flushRuntimeCheckpoint() has
	// run since the pin, so stopRuntimeAccounting()'s OWN day check (or lack
	// of one) is what's under test.
	injector <- shellyInputEvent(0, true) // water supply ON stops the pump
	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	})
	if !stopped {
		t.Fatalf("pump did not stop after water supply ON")
	}

	final := readRuntimeStorage(t, deviceState)
	if final.Sec >= 20000 {
		t.Fatalf(`Script.storage["runtime"].sec after a stop landing at a day boundary = %v, want well `+
			`under 20000 (yesterday's 20000s must not be credited to today just because the stop event, `+
			`not a periodic checkpoint, was what observed the rollover)`, final.Sec)
	}
	if localDayNumber(int64(final.Ts)) != localDayNumber(time.Now().Unix()) {
		t.Fatalf(`Script.storage["runtime"].ts after a stop landing at a day boundary = %v, want today`, final.Ts)
	}
}

// === Solar-driven hysteresis (#405) ===
//
// solarKVS extends controllerKVS() with solar hysteresis enabled and
// near-zero start/stop delays, so tests can assert on hysteresis outcomes
// without waiting out real-world delay windows. Individual tests override
// specific keys (e.g. solar-max-turnover) via extra.
func solarKVS(extra map[string]string) map[string]interface{} {
	kvs := controllerKVS()
	kvs["script/pool-pump/solar-enabled"] = "true"
	kvs["script/pool-pump/solar-start-w"] = "500"
	kvs["script/pool-pump/solar-stop-w"] = "200"
	kvs["script/pool-pump/solar-start-delay"] = "0"
	kvs["script/pool-pump/solar-stop-delay"] = "0"
	kvs["script/pool-pump/solar-min-turnover"] = "5"
	kvs["script/pool-pump/solar-max-turnover"] = "7"
	kvs["script/pool-pump/solar-stale-ms"] = "300000"
	for k, v := range extra {
		kvs["script/pool-pump/"+k] = v
	}
	return kvs
}

// solarPayload builds the myhome/energy/solar/available JSON payload (see
// #403's SolarAvailablePayload): available_w plus ts (unix-epoch-seconds —
// the field pool-pump.js's staleness check is based on, not local receipt
// time; see the SOLAR var comment in pool-pump.js).
func solarPayload(availableW float64, tsUnixSec int64) []byte {
	payload := map[string]interface{}{
		"available_w": availableW,
		"ts":          tsUnixSec,
	}
	data, _ := json.Marshal(payload)
	return data
}

// TestPoolPump_SolarStartsAndStopsPump verifies the core hysteresis: a fresh,
// above-threshold sample starts the pump (via doStart, so the fuse/
// isMyTurnToRun/water-supply checks all still apply), and a subsequent fresh,
// below-threshold sample stops it again. Zero start/stop delays (solarKVS)
// mean both transitions should happen essentially immediately.
func TestPoolPump_SolarStartsAndStopsPump(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mc := mqtt.NewMockClient()
	mqtt.SetClient(mc)
	t.Cleanup(mqtt.ResetClient)

	deviceState := &script.DeviceState{
		KVS:             solarKVS(nil),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedules(),
	}

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Fresh (ts = now), above the 500W start threshold.
	if err := mc.Publish(ctx, "myhome/energy/solar/available", solarPayload(600, time.Now().Unix()), 0, true, "test"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	started := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0" // eco switch, per controllerKVS()'s eco-speed=0
	})
	if !started {
		cancel()
		<-done
		t.Fatalf("solar start: expected active-output=0, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	// Fresh (ts = now), below the 200W stop threshold.
	if err := mc.Publish(ctx, "myhome/energy/solar/available", solarPayload(100, time.Now().Unix()), 0, true, "test"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "-1"
	})

	cancel()
	<-done

	if !stopped {
		t.Fatalf("solar stop: expected active-output=-1, got %v", kvsValue(deviceState, "script/pool-pump/active-output"))
	}
}

// TestPoolPump_SolarRespectsHardCeiling verifies that a runtime baseline
// already at/above the solar hard-ceiling target blocks a solar start even
// with fresh, well-above-threshold availability. solar-max-turnover is
// overridden to a tiny value so the ceiling is reachable by a modest
// pre-seeded runtime-sec without waiting out the default target in real time.
func TestPoolPump_SolarRespectsHardCeiling(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mc := mqtt.NewMockClient()
	mqtt.SetClient(mc)
	t.Cleanup(mqtt.ResetClient)

	// Ceiling target = poolVolume(46) * solarMaxTurnover(0.001) / flowRate(≈21.38 m3/h) * 3600s ≈ 7.7s.
	kvs := solarKVS(map[string]string{"solar-max-turnover": "0.001"})

	deviceState := &script.DeviceState{
		KVS: kvs,
		Storage: map[string]interface{}{
			"runtime-sec":  "3600", // 1h — comfortably exceeds the ~7.7s ceiling above
			"runtime-date": dateString(time.Now()),
		},
		ComponentStatus: pro3ComponentStatus(), // pump off at boot
		Schedules:       poolPumpSchedules(),
	}

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Fresh, well above the start threshold, zero start delay — would start
	// immediately if the hard ceiling weren't in effect.
	if err := mc.Publish(ctx, "myhome/energy/solar/available", solarPayload(600, time.Now().Unix()), 0, true, "test"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// Give checkSolarHysteresis time to run; the pump must not start.
	time.Sleep(2 * time.Second)
	cancel()
	<-done

	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("solar hard ceiling: expected pump to stay off, got active-output=%v", v)
	}
}

// TestPoolPump_SolarHardCeiling_DayRolloverUnblocksWithoutRestart is the
// regression test for #502: a device with ZERO Schedule.List jobs at all
// (Schedules is nil, mirroring `mezzanine`'s "only a firmware-update job"
// reality) whose in-flight runtime accounting is still pinned to a stale
// day, purely because the script has been running continuously with no
// restart -- exactly the catch-22 that left mezzanine stuck at 42002s
// against a 37393s ceiling: the ceiling blocks solar, solar staying off
// means the pump never runs either, and with no schedule job and no
// restart, nothing else ever re-checks the day.
//
// The goja harness has no injectable virtual clock (Date.now()/Timer.set
// are tied to the real system clock -- see the comment on
// TestPoolPump_RuntimeAccounting_LegacyMigrationDiscardsStaleDate), so a
// real day boundary crossing mid-run cannot be produced by waiting. Instead
// this uses ScheduleEvalInjector -- the same mechanism a real device's
// Schedule.eval -> Script.Eval(id, code) uses to run a due job's code
// against the live script -- to directly overwrite the *in-memory*
// STATE.runtimeTs/runtimeTodaySec the way a real midnight rollover would,
// without going through loadState() (which already resets correctly on any
// fresh boot -- see TestPoolPump_RuntimeAccounting_PreviousDayRestartResets
// -- and would therefore mask a regression in the *other* reset path this
// test targets: solarHardCeilingReached()'s own ensureRuntimeDay() call).
func TestPoolPump_SolarHardCeiling_DayRolloverUnblocksWithoutRestart(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mc := mqtt.NewMockClient()
	mqtt.SetClient(mc)
	t.Cleanup(mqtt.ResetClient)

	// Same tiny ceiling override as TestPoolPump_SolarRespectsHardCeiling
	// (~7.7s target), so a modest injected sec total blocks solar start.
	kvs := solarKVS(map[string]string{"solar-max-turnover": "0.001"})

	evalCh := make(chan []byte, 4)
	deviceState := &script.DeviceState{
		KVS:                  kvs,
		Storage:              make(map[string]interface{}),
		ComponentStatus:      pro3ComponentStatus(), // pump off at boot
		Schedules:            nil,                   // #502: the mezzanine case -- no pool schedules at all
		ScheduleEvalInjector: evalCh,
	}

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Pin the running script's in-memory runtime state to "yesterday, way
	// over the ceiling" -- standing in for many idle days spent stuck
	// against the ceiling with no restart, since the script's own boot-time
	// reset (loadState) already ran moments ago on a fresh (today) total.
	yesterday := time.Now().AddDate(0, 0, -1).Unix()
	stale := fmt.Sprintf("STATE.runtimeTs = %d; STATE.runtimeTodaySec = 3600;", yesterday)
	if err := evalPoolPumpSchedule(deviceState, stale); err != nil {
		t.Fatalf("failed to inject stale runtime state: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // let the injected eval land

	// Fresh, well above the start threshold, zero start delay: would stay
	// blocked forever by the stale (yesterday's) ceiling if nothing but a
	// restart could ever re-check the day -- there is no restart here, and
	// no schedule job to fall back on either.
	if err := mc.Publish(ctx, "myhome/energy/solar/available", solarPayload(600, time.Now().Unix()), 0, true, "test"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	started := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0" // eco switch, per controllerKVS()'s eco-speed=0
	})

	cancel()
	<-done

	if !started {
		t.Fatalf("expected solar start after an in-flight day rollover with no restart and no schedule "+
			"jobs, got active-output=%v (the stale ceiling was never re-checked)",
			kvsValue(deviceState, "script/pool-pump/active-output"))
	}
}

// TestPoolPump_SolarStaleFallsBackToSchedule verifies that solar hysteresis
// being enabled but never having received a myhome/energy/solar/available
// message (SOLAR.publishedTs stays 0 — the "absent" case) does not interfere
// with the schedule-independent control path. This harness has no way to
// fire a Schedule.Create'd handler directly (see the comment on
// TestPoolPump_RuntimeAccounting_TracksElapsedRunAndTurnover), so a button
// press stands in for "the existing forecast-driven schedule keeps running
// as today" — both go through doStart()/activateOutput(), unaffected by
// checkSolarHysteresis's early-return-on-stale/absent path.
func TestPoolPump_SolarStaleFallsBackToSchedule(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)

	deviceState := &script.DeviceState{
		KVS:             solarKVS(nil),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedules(),
		EventInjector:   injector,
	}

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	injector <- shellyButtonEvent()
	started := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	})

	cancel()
	<-done

	if !started {
		t.Fatalf("expected button press to start pump despite solar enabled/never-received, got active-output=%v",
			kvsValue(deviceState, "script/pool-pump/active-output"))
	}
}

// TestPoolPump_SolarStaleTsFallsBackImmediately is the regression test for
// the ts-based staleness correction (see #405 PR description / pool-pump.js
// SOLAR var comment): the issue's own suggested onSolarAvailable() snippet
// tracked Date.now() at *message-receipt* time as the freshness marker. MQTT
// delivers retained messages to a new subscriber immediately regardless of
// how old they are — if the daemon published a value and then died, and this
// script rebooted and re-subscribed hours later, it would receive that old
// retained message immediately, and a receipt-time freshness marker would
// make it look perfectly fresh. This test publishes a message whose `ts` is
// 1 hour old (well past the 5-minute default solar-stale-ms) and asserts the
// pump does NOT solar-start, proving staleness is judged from the payload's
// own `ts`, not from receipt time — the failure mode would otherwise start
// the pump immediately (zero start delay, 600W above the 500W threshold).
func TestPoolPump_SolarStaleTsFallsBackImmediately(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mc := mqtt.NewMockClient()
	mqtt.SetClient(mc)
	t.Cleanup(mqtt.ResetClient)

	// solar-stale-ms defaults to 300000 (5 min) via CONFIG_SCHEMA — not
	// overridden here, so this exercises the real default.
	kvs := solarKVS(nil)

	deviceState := &script.DeviceState{
		KVS:             kvs,
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedules(),
	}

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	staleTs := time.Now().Unix() - 3600 // 1h old — stale under the 5-minute solar-stale-ms
	if err := mc.Publish(ctx, "myhome/energy/solar/available", solarPayload(600, staleTs), 0, true, "test"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// Give onSolarAvailable/checkSolarHysteresis (called synchronously on
	// message receipt) time to run. A receipt-time-based implementation
	// would start the pump within this window; the ts-based implementation
	// must not.
	time.Sleep(2 * time.Second)
	cancel()
	<-done

	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("stale ts: expected pump to stay off (stale detected immediately on receipt), got active-output=%v", v)
	}
}

// === Schedule-rewrite re-evaluation (#441) ===
//
// updateScheduleMode() rewrites the morning-start / evening-stop schedule jobs
// from the day's forecast. Before #441 nothing re-evaluated the pump
// afterwards, so a rewrite that moved the start into the past silently skipped
// the whole day's filtration: handleMorningStart() only ever fires at its
// scheduled instant, and an instant that has already passed never fires. That
// is exactly what happened on the production pump on 2026-08-06.
//
// These tests drive the real path — init → handleDailyCheck() →
// performDailyModeCheck() → forecast → decideModeFromForecast() →
// updateScheduleMode() — against a local forecast server, and assert on the
// pump state that results.

// poolPumpForecastServer serves an Open-Meteo response shaped the way
// pool-pump.js's onForecast() parses it: 24 hourly temperatures whose index is
// the hour of day (so the index of the maximum becomes STATE.peakForecastHour)
// plus one daily sunrise/sunset pair, which decideModeFromForecast() turns
// into the window's start floor (sunrise + 1h) and stop ceiling (sunset - 0.5h).
//
// Local on purpose: the forecast fetch is a plain Shelly.call("HTTP.GET"),
// which the emulator executes for real. Pointing it at a test server keeps
// these tests deterministic and offline.
func poolPumpForecastServer(t *testing.T, peakHour int, sunrise, sunset string) *httptest.Server {
	t.Helper()

	temps := make([]float64, 24)
	for i := range temps {
		temps[i] = 25
	}
	// Above poolPumpWindowKVS's max-temp, so computeRunHours()'s temperature
	// scale clamps to 1 and run hours land exactly on the configured base hours.
	temps[peakHour] = 40

	body, err := json.Marshal(map[string]interface{}{
		"hourly": map[string]interface{}{"temperature_2m": temps},
		"daily": map[string]interface{}{
			"sunrise": []string{"2026-08-07T" + sunrise},
			"sunset":  []string{"2026-08-07T" + sunset},
		},
	})
	if err != nil {
		t.Fatalf("failed to build forecast body: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// poolPumpWindowKVS is pro1KVS() with the pool geometry rigged so
// computeRunHours() returns exactly runHours: flow rate is
// max-flow-rate × (eco-rpm / max-rpm) = 1 m³/h, and base hours are
// pool-volume × turnover / flow = runHours.
func poolPumpWindowKVS(runHours float64) map[string]interface{} {
	kvs := pro1KVS()
	kvs["script/pool-pump/pool-volume"] = strconv.FormatFloat(runHours, 'f', -1, 64)
	kvs["script/pool-pump/turnover"] = "1"
	kvs["script/pool-pump/max-flow-rate"] = "1"
	kvs["script/pool-pump/eco-rpm"] = "2900"
	kvs["script/pool-pump/max-rpm"] = "2900"
	kvs["script/pool-pump/max-temp"] = "35"
	kvs["script/pool-pump/temp-threshold"] = "20"
	return kvs
}

// poolPumpTimespec renders the cron form makeTimespec() produces.
func poolPumpTimespec(h, m int) string {
	return fmt.Sprintf("0 %d %d * * SUN,MON,TUE,WED,THU,FRI,SAT", m, h)
}

// poolPumpSummerSchedules is a complete summer-mode job set: a daily check, a
// morning/evening pair at the given times, and the two night jobs already
// disabled — which is what summer mode looks like on a real device.
func poolPumpSummerSchedules(start, stop time.Time) []map[string]interface{} {
	job := func(id int, code, timespec string, enable bool) map[string]interface{} {
		return map[string]interface{}{
			"id": id, "enable": enable, "timespec": timespec,
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": code},
			}},
		}
	}
	return []map[string]interface{}{
		job(1, "handleDailyCheck()", "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
		job(2, "handleMorningStart()", poolPumpTimespec(start.Hour(), start.Minute()), true),
		job(3, "handleEveningStop()", poolPumpTimespec(stop.Hour(), stop.Minute()), true),
		job(4, "handleNightStart()", poolPumpTimespec(23, 15), false),
		job(5, "handleNightStop()", poolPumpTimespec(0, 15), false),
	}
}

// poolPumpScheduleTimespec reads back the timespec currently on the job whose
// script.eval code contains handlerCall, so a test can prove the rewrite
// actually landed before asserting on what the rewrite should have caused.
//
// Matches by substring, not equality: #480 wraps every registered code field
// in wrapScheduleCall()'s try/catch boilerplate, so a live (or
// wrapped-fixture) job's code is never exactly "handleMorningStart()" — it
// contains it. Plain unwrapped fixtures used elsewhere still match, since a
// string trivially contains itself.
func poolPumpScheduleTimespec(schedules []map[string]interface{}, handlerCall string) string {
	for _, job := range schedules {
		calls, ok := job["calls"].([]interface{})
		if !ok || len(calls) == 0 {
			continue
		}
		call, ok := calls[0].(map[string]interface{})
		if !ok {
			continue
		}
		params, ok := call["params"].(map[string]interface{})
		if !ok {
			continue
		}
		code, ok := params["code"].(string)
		if !ok || !strings.Contains(code, handlerCall) {
			continue
		}
		if ts, ok := job["timespec"].(string); ok {
			return ts
		}
	}
	return ""
}

// poolPumpWrapScheduleCall mirrors pool-pump.js's wrapScheduleCall() exactly,
// so Go fixtures can seed Schedule.List with the same wrapped source a live
// device carries after #480 — proving isWithinRunWindow(),
// updateScheduleMode() and verifySchedules() still recognize handler jobs by
// substring once the code field is no longer the bare handler call.
func poolPumpWrapScheduleCall(handlerCall string) string {
	return "(function(){try{" + handlerCall + "}catch(e){log('schedule handler error:',e)}})()"
}

// poolPumpSummerSchedulesWrapped is poolPumpSummerSchedules with every job's
// code field passed through poolPumpWrapScheduleCall — the shape a real
// device carries once createSchedules() has registered #480-wrapped jobs.
func poolPumpSummerSchedulesWrapped(start, stop time.Time) []map[string]interface{} {
	job := func(id int, code, timespec string, enable bool) map[string]interface{} {
		return map[string]interface{}{
			"id": id, "enable": enable, "timespec": timespec,
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": poolPumpWrapScheduleCall(code)},
			}},
		}
	}
	return []map[string]interface{}{
		job(1, "handleDailyCheck()", "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
		job(2, "handleMorningStart()", poolPumpTimespec(start.Hour(), start.Minute()), true),
		job(3, "handleEveningStop()", poolPumpTimespec(stop.Hour(), stop.Minute()), true),
		job(4, "handleNightStart()", poolPumpTimespec(23, 15), false),
		job(5, "handleNightStop()", poolPumpTimespec(0, 15), false),
	}
}

// poolPumpRewriteResult runs the script against the given fixture, waits for
// the schedule rewrite to land, then reports the active-output the script
// settled on. wantOutput is what the caller expects; the helper polls for it
// so a passing test finishes quickly and a failing one still reports the
// value actually reached.
func poolPumpRewriteResult(t *testing.T, deviceState *script.DeviceState, wantOutput string) (string, []map[string]interface{}) {
	t.Helper()

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	buf := readPoolPumpScript(t)
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	// The rewrite has to land before the reconciliation it triggers can be
	// judged, so wait for the morning job's timespec to change first.
	initial := poolPumpScheduleTimespec(deviceState.ScheduleJobs(), "handleMorningStart()")
	if !waitFor(15*time.Second, 100*time.Millisecond, func() bool {
		return poolPumpScheduleTimespec(deviceState.ScheduleJobs(), "handleMorningStart()") != initial
	}) {
		cancel()
		<-done
		t.Fatalf("schedule rewrite never happened (morning start still %q)", initial)
	}

	// Then give the reconciliation its own window to act — or to provably not.
	waitFor(8*time.Second, 100*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == wantOutput
	})

	got, _ := kvsValue(deviceState, "script/pool-pump/active-output").(string)
	schedules := deviceState.ScheduleJobs()
	cancel()
	<-done
	return got, schedules
}

// Two window shapes, both derived from the forecast by decideModeFromForecast():
//
//   - wide: sunrise 00:00 → start floor 01:00; sunset 23:59 → stop ceiling
//     23:29; peak at noon with 22.48 run hours. The unclamped window starts
//     before the floor, so it is pinned to [01:00, 23:29) — containing almost
//     any "now".
//   - narrow: peak at 02:00 with 0.1 run hours → [01:57, 02:03), excluding
//     almost any "now".
const (
	poolPumpWideRunHours   = 22.48
	poolPumpWidePeakHour   = 12
	poolPumpWideStartMin   = 60   // 01:00
	poolPumpWideStopMin    = 1409 // 23:29
	poolPumpNarrowRunHours = 0.1
	poolPumpNarrowPeakHour = 2
	poolPumpNarrowStartMin = 117 // 01:57
	poolPumpNarrowStopMin  = 123 // 02:03
)

// nowMinutes is the minutes-since-midnight value pool-pump.js's
// isWithinRunWindow() computes from new Date().
func nowMinutes() int {
	now := time.Now()
	return now.Hour()*60 + now.Minute()
}

// skipUnlessNowInside guards the one wall-clock dependency these tests cannot
// design away: pool-pump.js derives the window from a real forecast and
// compares it against the device's real clock, and its own clamps refuse to
// schedule a start before 01:00 — so "now is inside the window" is simply not
// expressible during the midnight hour.
func skipUnlessNowInside(t *testing.T, startMin, stopMin int) {
	t.Helper()
	if n := nowMinutes(); n < startMin || n >= stopMin {
		t.Skipf("local time %02d:%02d is outside the [%d, %d) minute window this scenario builds",
			n/60, n%60, startMin, stopMin)
	}
}

func skipIfNowInside(t *testing.T, startMin, stopMin int) {
	t.Helper()
	if n := nowMinutes(); n >= startMin && n < stopMin {
		t.Skipf("local time %02d:%02d falls inside the [%d, %d) minute window this scenario builds",
			n/60, n%60, startMin, stopMin)
	}
}

// A rewrite that moves the morning start from the future into the past, with
// "now" inside the new window, must start the pump. This is the 2026-08-06
// production failure: without the fix the pump stays off for the whole day.
func TestPoolPump_ScheduleRewriteIntoPastStartsPump(t *testing.T) {
	skipUnlessNowInside(t, poolPumpWideStartMin, poolPumpWideStopMin)

	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	now := time.Now()

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: pro1ComponentStatus(), // pump off
		// Old window: the start is still ahead of us, so nothing was due yet.
		Schedules: poolPumpSummerSchedules(now.Add(2*time.Hour), now.Add(4*time.Hour)),
	}

	got, schedules := poolPumpRewriteResult(t, deviceState, "0")

	if ts := poolPumpScheduleTimespec(schedules, "handleMorningStart()"); ts != poolPumpTimespec(1, 0) {
		t.Fatalf("expected morning start rewritten to 01:00, got %q", ts)
	}
	if got != "0" {
		t.Fatalf("rewrite moved the start into the past with now inside the new window: "+
			"expected the pump to start (active-output=0), got %q", got)
	}
}

// The sunrise case, with no restart involved: the previous window has already
// elapsed, the daily re-forecast rewrites it to one containing "now", and the
// pump must start even though no schedule job fires.
func TestPoolPump_SunriseRewriteStartsPump(t *testing.T) {
	skipUnlessNowInside(t, poolPumpWideStartMin, poolPumpWideStopMin)

	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	now := time.Now()

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: pro1ComponentStatus(), // pump off, correctly so
		// Yesterday's window, entirely behind us.
		Schedules: poolPumpSummerSchedules(now.Add(-5*time.Hour), now.Add(-4*time.Hour)),
	}

	got, _ := poolPumpRewriteResult(t, deviceState, "0")
	if got != "0" {
		t.Fatalf("sunrise rewrite brought now inside the window: "+
			"expected the pump to start (active-output=0), got %q", got)
	}
}

// The converse: a rewrite that moves the window off "now" must stop a pump
// that is currently running, rather than leave it running until an
// evening-stop instant that no longer matches anything.
func TestPoolPump_ScheduleRewriteAwayStopsPump(t *testing.T) {
	skipIfNowInside(t, poolPumpNarrowStartMin, poolPumpNarrowStopMin)

	srv := poolPumpForecastServer(t, poolPumpNarrowPeakHour, "00:00", "23:59")
	now := time.Now()

	cs := pro1ComponentStatus()
	cs["switch:0"] = map[string]interface{}{"id": 0, "output": true} // running

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpNarrowRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: cs,
		// Old window contains "now", which is why the pump is running.
		Schedules: poolPumpSummerSchedules(now.Add(-1*time.Hour), now.Add(1*time.Hour)),
	}

	got, schedules := poolPumpRewriteResult(t, deviceState, "-1")

	if ts := poolPumpScheduleTimespec(schedules, "handleMorningStart()"); ts != poolPumpTimespec(1, 57) {
		t.Fatalf("expected morning start rewritten to 01:57, got %q", ts)
	}
	if got != "-1" {
		t.Fatalf("rewrite moved the window off now: "+
			"expected the running pump to stop (active-output=-1), got %q", got)
	}
}

// Regression guard: a rewrite that leaves the pump correctly off — "now" is
// outside both the old and the new window — must not start it.
func TestPoolPump_ScheduleRewriteOutsideWindowDoesNotStartPump(t *testing.T) {
	skipIfNowInside(t, poolPumpNarrowStartMin, poolPumpNarrowStopMin)

	srv := poolPumpForecastServer(t, poolPumpNarrowPeakHour, "00:00", "23:59")
	now := time.Now()

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpNarrowRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: pro1ComponentStatus(), // pump off
		Schedules:       poolPumpSummerSchedules(now.Add(-5*time.Hour), now.Add(-4*time.Hour)),
	}

	// Ask for "0" so the helper spends its full budget looking for a start
	// that must never come.
	got, _ := poolPumpRewriteResult(t, deviceState, "0")
	if got != "-1" {
		t.Fatalf("now is outside the rewritten window: "+
			"expected the pump to stay off (active-output=-1), got %q", got)
	}
}

// #480 regression guards: everything above this point seeds Schedule.List
// with bare handler-name code strings. A live device running the wrapped
// createSchedules() never has that — every code field is
// wrapScheduleCall()'s try/catch boilerplate around the handler call. These
// two tests are TestPoolPump_ScheduleRewriteIntoPastStartsPump and
// TestPoolPump_ScheduleRewriteAwayStopsPump verbatim, except the fixture is
// poolPumpSummerSchedulesWrapped instead of poolPumpSummerSchedules — proving
// isWithinRunWindow() (the #441 predicate) and updateScheduleMode()'s job
// matching both still resolve a real answer, not null, once matching moved
// from exact/prefix equality to substring containment. If either regresses
// back to exact-match, isWithinRunWindow() silently returns null ("don't
// know") for every job and these tests fail by leaving the pump in the wrong
// state instead of erroring loudly — exactly the #441 failure shape.
func TestPoolPump_WrappedScheduleCode_IsWithinRunWindowStartsPump(t *testing.T) {
	skipUnlessNowInside(t, poolPumpWideStartMin, poolPumpWideStopMin)

	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	now := time.Now()

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: pro1ComponentStatus(), // pump off
		// Old window: the start is still ahead of us, so nothing was due yet.
		Schedules: poolPumpSummerSchedulesWrapped(now.Add(2*time.Hour), now.Add(4*time.Hour)),
	}

	got, schedules := poolPumpRewriteResult(t, deviceState, "0")

	if ts := poolPumpScheduleTimespec(schedules, "handleMorningStart()"); ts != poolPumpTimespec(1, 0) {
		t.Fatalf("updateScheduleMode() failed to find/rewrite the wrapped morning-start job: "+
			"expected 01:00, got %q", ts)
	}
	if got != "0" {
		t.Fatalf("isWithinRunWindow() failed to resolve a wrapped-code schedule: "+
			"expected the pump to start (active-output=0), got %q", got)
	}
}

func TestPoolPump_WrappedScheduleCode_IsWithinRunWindowStopsPump(t *testing.T) {
	skipIfNowInside(t, poolPumpNarrowStartMin, poolPumpNarrowStopMin)

	srv := poolPumpForecastServer(t, poolPumpNarrowPeakHour, "00:00", "23:59")
	now := time.Now()

	cs := pro1ComponentStatus()
	cs["switch:0"] = map[string]interface{}{"id": 0, "output": true} // running

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpNarrowRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: cs,
		// Old window contains "now", which is why the pump is running.
		Schedules: poolPumpSummerSchedulesWrapped(now.Add(-1*time.Hour), now.Add(1*time.Hour)),
	}

	got, schedules := poolPumpRewriteResult(t, deviceState, "-1")

	if ts := poolPumpScheduleTimespec(schedules, "handleMorningStart()"); ts != poolPumpTimespec(1, 57) {
		t.Fatalf("updateScheduleMode() failed to find/rewrite the wrapped morning-start job: "+
			"expected 01:57, got %q", ts)
	}
	if got != "-1" {
		t.Fatalf("isWithinRunWindow() failed to resolve a wrapped-code schedule: "+
			"expected the running pump to stop (active-output=-1), got %q", got)
	}
}

// === #509: updateScheduleMode() derives the post-update window from the
// jobs it already has in hand, instead of paying for a second Schedule.List
// (queueTask(readWindow)) on top of the sunrise forecast parse. That second
// read was measured to cost ~670 bytes of mem_peak on `mezzanine`, and on
// `filtration-hiver` on 2026-08-17 — with only ~1.2 KB of heap headroom left
// — it reclaimed the whole script with no trace, mid schedule-rewrite.
//
// These four tests pin the invariant #509 requires: the derived window fact
// must equal whatever a fresh Schedule.List taken after the updates would
// have produced. TestPoolPump_ScheduleRewriteIntoPastStartsPump and its
// siblings above already cover "summer window moved" end to end (a rewrite
// that changes the active-output outcome); these four cover the remaining
// arms explicitly required by #509's done-criteria.

// poolPumpWinterForecastServer serves an Open-Meteo response whose every
// hourly temperature sits under poolPumpWindowKVS()'s temp-threshold (20),
// so decideModeFromForecast() selects 'winter' and calls
// updateScheduleMode('winter', null, null) — the no-timings arm that #509's
// invariant list calls out by name, since a summer-only derivation would not
// cover it.
func poolPumpWinterForecastServer(t *testing.T) *httptest.Server {
	t.Helper()

	temps := make([]float64, 24)
	for i := range temps {
		temps[i] = 10
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestPoolPump_DailyCheckNoOpRewriteLeavesWindowAndScheduleUnchanged is the
// "windowChanged false" arm: the schedule already carries exactly the
// window the forecast recomputes, so updateScheduleMode() writes nothing at
// all and the derivation path in updateNext()'s completion must not run
// either. The pump keeps running off the window resolved at init.
func TestPoolPump_DailyCheckNoOpRewriteLeavesWindowAndScheduleUnchanged(t *testing.T) {
	skipUnlessNowInside(t, poolPumpWideStartMin, poolPumpWideStopMin)

	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	now := time.Now()

	// poolPumpWideStartMin/StopMin (01:00 / 23:29) is exactly what this
	// forecast+KVS combination recomputes — see the comment on those
	// constants — so seeding the schedule with those same times means
	// moves() finds nothing to write.
	start := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, now.Location())
	stop := time.Date(now.Year(), now.Month(), now.Day(), 23, 29, 0, 0, now.Location())

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: pro1ComponentStatus(), // pump off at boot
		Schedules:       poolPumpSummerSchedules(start, stop),
	}

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	buf := readPoolPumpScript(t)
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	// The window is already correct, so init's own onWindowJobs() call (not
	// the daily check) should start the pump.
	started := waitFor(initTimeout, 100*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	})

	// Give the daily check its own window to run to completion and (not)
	// rewrite anything, same budget poolPumpRewriteResult gives a genuine
	// rewrite to land.
	waitFor(8*time.Second, 100*time.Millisecond, func() bool { return false })

	got := kvsValue(deviceState, "script/pool-pump/active-output")
	schedules := deviceState.ScheduleJobs()
	cancel()
	<-done

	if !started {
		t.Fatalf("pump never started even though the pre-existing window already contains now")
	}
	if got != "0" {
		t.Fatalf("a no-op daily check (windowChanged false) must not disturb the window "+
			"the pump is already running from: active-output=%v", got)
	}
	if ts := poolPumpScheduleTimespec(schedules, "handleMorningStart()"); ts != poolPumpTimespec(1, 0) {
		t.Fatalf("windowChanged should have stayed false: morning-start timespec changed to %q", ts)
	}
	if ts := poolPumpScheduleTimespec(schedules, "handleEveningStop()"); ts != poolPumpTimespec(23, 29) {
		t.Fatalf("windowChanged should have stayed false: evening-stop timespec changed to %q", ts)
	}
}

// TestPoolPump_DailyCheckWinterSwitchDerivesNightWindow is the "winter mode
// switch" arm. decideModeFromForecast() calls
// updateScheduleMode('winter', null, null) — no timings — so the derivation
// path must resolve the night window from enable flags alone, which a
// summer-only derivation would not exercise.
func TestPoolPump_DailyCheckWinterSwitchDerivesNightWindow(t *testing.T) {
	srv := poolPumpWinterForecastServer(t)
	now := time.Now()

	job := func(id int, code, timespec string, enable bool) map[string]interface{} {
		return map[string]interface{}{
			"id": id, "enable": enable, "timespec": timespec,
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": code},
			}},
		}
	}

	// Night jobs already bracket "now" but start disabled, matching a real
	// summer-mode device. The winter switch must enable them at their
	// existing timespec — updateScheduleMode never touches night timespecs —
	// and the resulting window must resolve without a second Schedule.List.
	nightStart := now.Add(-1 * time.Hour)
	nightStop := now.Add(1 * time.Hour)

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: pro1ComponentStatus(), // pump off
		Schedules: []map[string]interface{}{
			job(1, "handleDailyCheck()", "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
			job(2, "handleMorningStart()", poolPumpTimespec(now.Add(2*time.Hour).Hour(), now.Add(2*time.Hour).Minute()), true),
			job(3, "handleEveningStop()", poolPumpTimespec(now.Add(4*time.Hour).Hour(), now.Add(4*time.Hour).Minute()), true),
			job(4, "handleNightStart()", poolPumpTimespec(nightStart.Hour(), nightStart.Minute()), false),
			job(5, "handleNightStop()", poolPumpTimespec(nightStop.Hour(), nightStop.Minute()), false),
		},
	}

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	buf := readPoolPumpScript(t)
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	modeOK := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		v, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists && v == "winter"
	})
	if !modeOK {
		cancel()
		<-done
		t.Fatalf("schedule-mode never became 'winter' for a below-threshold forecast; got %v",
			kvsValue(deviceState, "script/pool-pump/schedule-mode"))
	}

	started := waitFor(eventTimeout, 100*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	})
	got := kvsValue(deviceState, "script/pool-pump/active-output")
	cancel()
	<-done

	if !started {
		t.Fatalf("winter switch enabled the night jobs bracketing now, but the pump never "+
			"started (active-output=%v) — the derived night window did not resolve", got)
	}
}

// TestPoolPump_DailyCheckMissingEveningJobLeavesWindowUnresolved is the
// "device whose morning/evening jobs are absent" arm. onWindowJobs()'s own
// "still symbolic" guard must fire here exactly as it would off a fresh
// Schedule.List: with no handleEveningStop() job to match at all, the
// derived window can never resolve, so the fact must stay whatever it
// already was — NOT get forced to "off" — even though updateScheduleMode()
// did write a change (the morning job moved) and windowChanged is true.
func TestPoolPump_DailyCheckMissingEveningJobLeavesWindowUnresolved(t *testing.T) {
	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	now := time.Now()

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
	cs["switch:0"] = map[string]interface{}{"id": 0, "output": true} // already running

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			"schedule-mode": "summer",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: cs,
		// No handleEveningStop() job at all, at init or after the rewrite --
		// the window can never resolve, so it must never have resolved, and
		// the pump the fixture starts already running must be left alone.
		Schedules: []map[string]interface{}{
			job(1, "handleDailyCheck()", "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
			job(2, "handleMorningStart()", poolPumpTimespec(now.Add(2*time.Hour).Hour(), now.Add(2*time.Hour).Minute()), true),
			job(4, "handleNightStart()", poolPumpTimespec(23, 15), false),
			job(5, "handleNightStop()", poolPumpTimespec(0, 15), false),
		},
	}

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	buf := readPoolPumpScript(t)
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initial := poolPumpScheduleTimespec(deviceState.ScheduleJobs(), "handleMorningStart()")
	rewritten := waitFor(initTimeout, 100*time.Millisecond, func() bool {
		return poolPumpScheduleTimespec(deviceState.ScheduleJobs(), "handleMorningStart()") != initial
	})
	if !rewritten {
		cancel()
		<-done
		t.Fatalf("morning-start job was never rewritten (still %q) — test didn't exercise windowChanged", initial)
	}

	// Give the reconciler its own window to (not) act on the unresolved window.
	waitFor(8*time.Second, 100*time.Millisecond, func() bool { return false })

	got := kvsValue(deviceState, "script/pool-pump/active-output")
	cancel()
	<-done

	if got != "0" {
		t.Fatalf("missing evening job must leave the window unresolved (-2 'no opinion'), "+
			"not force the already-running pump off: active-output=%v", got)
	}
}

// TestPoolPump_DailyCheckRunsDuringWaterSupplyProtection is the aftermath
// defect noted alongside the #450 crash: after the live crash-restart on
// 2026-08-11, the script logged 'Current mode: winter', then 'Daily check
// event' -> 'Water supply protection active, ignoring event' — the daily
// check that would have restored 'summer' mode was skipped outright because
// handleDailyCheck() bailed out whenever input:0 was active, leaving the
// device on a winter schedule on an August evening until a LATER restart
// happened to find protection cleared.
//
// handleDailyCheck() runs automatically once, at the very end of init()
// (continueInit()'s queueTask(handleDailyCheck)), so booting this device
// with water-supply protection ALREADY active (input:0.state=true in the
// very first ComponentStatus read) reproduces exactly that shape without
// needing a live crash: if the guard removed by this fix were still in
// place, performDailyModeCheck() would never run and schedule-mode would
// stay 'winter' forever despite a forecast hot enough for 'summer'.
func TestPoolPump_DailyCheckRunsDuringWaterSupplyProtection(t *testing.T) {
	srv := poolPumpForecastServer(t, poolPumpWidePeakHour, "00:00", "23:59")
	now := time.Now()

	cs := pro1ComponentStatus()
	cs["input:0"] = map[string]interface{}{"id": 0, "state": true} // protected from boot

	deviceState := &script.DeviceState{
		KVS: poolPumpWindowKVS(poolPumpWideRunHours),
		Storage: map[string]interface{}{
			// No schedule-mode seeded: loadState() defaults to "winter",
			// matching the live device's post-crash state.
			"forecast-url": srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus: cs,
		Schedules:       poolPumpSummerSchedules(now.Add(2*time.Hour), now.Add(4*time.Hour)),
	}

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	buf := readPoolPumpScript(t)
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	ok := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		v, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists && v == "summer"
	})
	cancel()
	<-done

	if !ok {
		t.Fatalf("schedule-mode never became 'summer' while water-supply protection was active at boot "+
			"(hot forecast, threshold exceeded); got %v — handleDailyCheck() is (again) silently skipping "+
			"performDailyModeCheck() during protection instead of only refusing pump actuation",
			kvsValue(deviceState, "script/pool-pump/schedule-mode"))
	}

	// Protection must still be respected: with input:0 active throughout,
	// the pump itself must never have been started by the mode change.
	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "-1" {
		t.Errorf("pump was started (active-output=%v) while water-supply protection was active — "+
			"doStart()'s own live input:0 check should have refused this regardless of the daily-check fix", v)
	}
}

// Locked read helpers for DeviceState (#451).
//
// The emulator mutates DeviceState's maps from the goroutine running the
// script while these tests poll them from the test goroutine. Reading the maps
// directly is a data race, and an unsynchronised map read racing a write does
// not merely flake — Go aborts the whole test binary with "fatal error:
// concurrent map read and map write", losing every result in the package.
//
// These wrappers keep the call sites as terse as the direct indexing they
// replace, so a predicate reads much as it did before, but every read now goes
// through DeviceState's RWMutex.

func kvsValue(d *script.DeviceState, key string) interface{} {
	v, _ := d.KVSValue(key)
	return v
}

func storageValue(d *script.DeviceState, key string) interface{} {
	v, _ := d.StorageValue(key)
	return v
}

func componentStatusValue(d *script.DeviceState, key string) (interface{}, bool) {
	return d.ComponentStatusValue(key)
}

// === CALL-SLOT MACHINERY REGRESSION TESTS (#421, #450) ===
//
// pool-pump.js's CALL_SLOTS pool, acquireCallSlot(), sharedCallDone() and the
// MAX_CALL_DEPTH deferral (lines ~399-540 of pool-pump.js as of origin/main
// 84133f8) had no test coverage before this file: `go test -run
// 'CallSlot|TooMany|Queue|Catchup|Restart'` returned "no tests to run". That
// machinery is exactly where the still-open #450 live crash surfaces
// ("Too much recursion ... in function acquireCallSlot"), so it is the
// highest-value thing to pin down here.
//
// RunWithDeviceState does not expose the running goja VM to Go tests (see
// the comment on TestPoolPump_RuntimeAccounting_TracksElapsedRunAndTurnover
// above), so there is no way to call acquireCallSlot()/inspect CALL_SLOTS
// from Go directly. Instead, callSlotTestHarnessJS is appended (in Go, at
// test time, never on disk) to an unmodified copy of pool-pump.js's own
// source. Appending only adds new top-level functions/vars after
// pool-pump.js's closing `init();` call and registers one more
// Shelly.addEventHandler — pool-pump.js's own code, and the CALL_SLOTS /
// acquireCallSlot / sharedCallDone / queueTask machinery it installs, are
// completely unmodified. The harness drives that real, unmodified machinery
// through the same Shelly.call wrapper pool-pump.js installs, and publishes
// results via KVS.Set (fire-and-forget, so it never itself touches
// CALL_SLOTS) so the Go test can observe them the same way every other test
// in this file observes KVS.
//
// callSlotTestHarnessJS relies on two facts verified by reading run.go
// (pkg/shelly/script): every emulated Shelly.call method (KVS.Set,
// Schedule.Update, etc.) invokes its callback synchronously, inline, before
// RAW_CALL returns to the JS caller — there is no goroutine-backed
// concurrency in the emulator. That means CALL_SLOTS never holds more than
// one slot "in flight" per call chain UNLESS sharedCallDone defers a
// completion (CALL_DEPTH >= MAX_CALL_DEPTH == 3): a deferred completion's
// slot stays marked used until queueTask's 200ms timer drains it via
// runPendingCallback(), which is the only way multiple slots are held open
// at once in this emulator. callSlotTestHarnessJS exploits exactly that: a
// 4-level-deep nested Shelly.call chain (csChain) always defers its 4th
// completion, so firing N independent chains back-to-back leaves N slots
// "stuck" simultaneously — more than CALL_SLOTS' fixed pool of 6 once N > 6
// — without needing any real concurrency.
const callSlotTestHarnessJS = `
// === CALL-SLOT REGRESSION TEST HARNESS (test-only; appended by
// pool_pump_test.go; never written to pool-pump.js, never shipped) ===
var CS_COMPLETED = 0;
var CS_CHAIN_LOG = [];

function csCountUsedSlots() {
  var n = 0;
  for (var i = 0; i < CALL_SLOTS.length; i++) {
    if (CALL_SLOTS[i].used) n++;
  }
  return n;
}

function csPublish(key, value) {
  // Fire-and-forget (no callback): Shelly.call routes this through
  // decrementOnlyCallDone, not acquireCallSlot, so publishing a result never
  // perturbs the CALL_SLOTS state being measured.
  Shelly.call("KVS.Set", {key: "test/callslot/" + key, value: String(value)});
}

// One chain of 4 nested Shelly.call completions, using the exact same
// Shelly.call pool-pump.js reassigned at load time. MAX_CALL_DEPTH is 3, so
// the 4th nested completion is always deferred via PENDING_DONE/queueTask
// instead of being invoked from inside the 3rd completion's stack frame —
// that deferral is the #450 fix under test.
function csChain(tag, onDone) {
  Shelly.call("KVS.Set", {key: "test/callslot/d1", value: "1"}, function(r1, ec1) {
    if (ec1 && false) {}
    CS_CHAIN_LOG.push(tag + ":1");
    Shelly.call("KVS.Set", {key: "test/callslot/d2", value: "1"}, function(r2, ec2) {
      if (ec2 && false) {}
      CS_CHAIN_LOG.push(tag + ":2");
      Shelly.call("KVS.Set", {key: "test/callslot/d3", value: "1"}, function(r3, ec3) {
        if (ec3 && false) {}
        CS_CHAIN_LOG.push(tag + ":3");
        Shelly.call("KVS.Set", {key: "test/callslot/d4", value: "1"}, function(r4, ec4) {
          if (ec4 && false) {}
          CS_CHAIN_LOG.push(tag + ":4");
          CS_COMPLETED++;
          csPublish("completed", CS_COMPLETED);
          csPublish("used-now", csCountUsedSlots());
          csPublish("chain-log-len", CS_CHAIN_LOG.length);
          if (onDone) onDone();
        });
      });
    });
  });
}

// Fires N independent chains back-to-back, synchronously (a plain for loop —
// no queueTask between chains). Each chain leaves exactly one deferred
// (4th-level) completion in flight, so N > CALL_SLOTS.length (6) forces
// acquireCallSlot's pool-exhausted fallback path (a fresh unpooled record)
// for the overflow. Snapshots are published immediately after the
// synchronous burst, before any queueTask tick has run, so they capture true
// peak concurrency rather than whatever has already drained.
function csRunBurst(n) {
  csPublish("pool-size", CALL_SLOTS.length);
  for (var i = 0; i < n; i++) {
    csChain("burst" + i, null);
  }
  csPublish("used-after-burst", csCountUsedSlots());
  csPublish("pending-after-burst", PENDING_DONE.length - PENDING_DONE_HEAD);
  csPublish("burst-fired", n);
}

function csRunSingleChain() {
  csChain("solo", null);
  // Published synchronously, in the same stack frame that fired the chain:
  // if the #450 fix were reverted (defer removed), the 4th level would
  // recurse inline and this would already read 4, not 3.
  csPublish("solo-log-len-immediate", CS_CHAIN_LOG.length);
}

// Schedule.Update on a non-existent schedule id always errors (-105),
// synchronously, at CALL_DEPTH 0 (not deferred) — exercises sharedCallDone's
// non-deferred branch specifically on the error path.
function csRunErrorCheck() {
  var before = csCountUsedSlots();
  Shelly.call("Schedule.Update", {id: 999999, enable: true}, function(res, ec, em) {
    if (res && false) {}
    if (em && false) {}
    csPublish("error-code", ec);
    csPublish("slots-before", before);
    csPublish("slots-after", csCountUsedSlots());
    csPublish("error-check-done", 1);
  });
}

Shelly.addEventHandler(function(event) {
  if (!event || !event.info) return;
  var info = event.info;
  if (info.event === "test.callslot.run-burst") {
    csRunBurst(info.n || 10);
  } else if (info.event === "test.callslot.run-single-chain") {
    csRunSingleChain();
  } else if (info.event === "test.callslot.run-error-check") {
    csRunErrorCheck();
  }
});
`

// callSlotEvent builds a synthetic device event routed to every registered
// Shelly.addEventHandler callback — pool-pump.js's own (which ignores it,
// same as any event it doesn't recognise) and callSlotTestHarnessJS's
// (which dispatches on info.event).
func callSlotEvent(action string, n int) []byte {
	event := map[string]interface{}{
		"info": map[string]interface{}{
			"event": action,
			"n":     n,
		},
	}
	data, _ := json.Marshal(event)
	return data
}

func newCallSlotDeviceState(injector chan []byte) *script.DeviceState {
	return &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedules(),
		EventInjector:   injector,
	}
}

func waitPoolPumpInit(t *testing.T, deviceState *script.DeviceState) bool {
	t.Helper()
	return waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVSValue("script/pool-pump/schedule-mode")
		return exists
	})
}

// TestPoolPump_CallSlotBurstExhaustsPoolButSurvives pins down #421/#450
// item 1 ("slot exhaustion is survivable"): firing more concurrent deferred
// completions (10) than CALL_SLOTS holds (6) must not crash the script or
// silently drop work. Before the #450 fix (deferral removed, or the
// pool-exhausted fallback in acquireCallSlot removed), this either recurses
// past MAX_CALL_DEPTH's guard or loses every call past the 6th.
func TestPoolPump_CallSlotBurstExhaustsPoolButSurvives(t *testing.T) {
	buf := append(readPoolPumpScript(t), []byte(callSlotTestHarnessJS)...)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)
	deviceState := newCallSlotDeviceState(injector)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	if !waitPoolPumpInit(t, deviceState) {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	const n = 10 // > CALL_SLOTS' fixed pool of 6
	injector <- callSlotEvent("test.callslot.run-burst", n)

	if !waitFor(eventTimeout, 20*time.Millisecond, func() bool {
		_, ok := deviceState.KVSValue("test/callslot/used-after-burst")
		return ok
	}) {
		cancel()
		<-done
		t.Fatalf("burst never ran (harness event handler not wired up)")
	}

	poolSize := parseKVSInt(t, deviceState, "test/callslot/pool-size")
	usedAfterBurst := parseKVSInt(t, deviceState, "test/callslot/used-after-burst")
	pendingAfterBurst := parseKVSInt(t, deviceState, "test/callslot/pending-after-burst")

	if poolSize != 6 {
		t.Fatalf("CALL_SLOTS pool size = %d, want 6 (pool-pump.js line ~407)", poolSize)
	}
	if usedAfterBurst > poolSize {
		t.Fatalf("CALL_SLOTS reports %d used slots but the pool only holds %d — pool grew unbounded", usedAfterBurst, poolSize)
	}
	if usedAfterBurst != poolSize {
		t.Fatalf("expected the pool fully exhausted (%d used) after a %d-chain burst, got %d used", poolSize, n, usedAfterBurst)
	}
	if pendingAfterBurst != n {
		t.Fatalf("expected all %d deferred completions still pending immediately after the burst, got %d — work was dropped instead of overflowing to the fallback path", n, pendingAfterBurst)
	}

	// The whole point of the fallback path: work beyond the 6-slot pool must
	// still complete, just via unpooled records instead of CALL_SLOTS.
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		return kvsValue(deviceState, "test/callslot/completed") == strconv.Itoa(n)
	}) {
		cancel()
		<-done
		t.Fatalf("burst did not fully drain; completed = %v, want %d — queued work was silently dropped",
			kvsValue(deviceState, "test/callslot/completed"), n)
	}

	cancel()
	<-done
}

// TestPoolPump_CallSlotsReleaseAfterBurst pins down #421/#450 item 2 ("slots
// are released"): once a burst that exhausted CALL_SLOTS has fully drained,
// every slot must be back to used:false — no permanent leak — and the pool
// must still be healthy enough for pool-pump.js's own, unrelated business
// logic (a button press cycling the pump speed) to work normally afterwards.
func TestPoolPump_CallSlotsReleaseAfterBurst(t *testing.T) {
	buf := append(readPoolPumpScript(t), []byte(callSlotTestHarnessJS)...)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)
	deviceState := newCallSlotDeviceState(injector)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	if !waitPoolPumpInit(t, deviceState) {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	const n = 10
	injector <- callSlotEvent("test.callslot.run-burst", n)

	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		return kvsValue(deviceState, "test/callslot/completed") == strconv.Itoa(n)
	}) {
		cancel()
		<-done
		t.Fatalf("burst did not fully drain; completed = %v, want %d", kvsValue(deviceState, "test/callslot/completed"), n)
	}

	// "used-now" is republished by every one of the n completions; the last
	// one published is from the n-th (final) drain, i.e. the pool's
	// steady-state after every deferred completion has run.
	if v := parseKVSInt(t, deviceState, "test/callslot/used-now"); v != 0 {
		t.Fatalf("CALL_SLOTS still reports %d used slots after the burst fully drained — leak", v)
	}

	// Prove the pool is not just numerically empty but actually usable:
	// pool-pump.js's own button handler (unrelated to the harness) must
	// still work, exactly as in TestPoolPump_ButtonCyclesPro3.
	if v := kvsValue(deviceState, "script/pool-pump/active-output"); v != "-1" {
		t.Fatalf("expected active-output=-1 before button press, got %v", v)
	}
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVSValue("script/pool-pump/active-output")
		return ok && v == "0"
	}) {
		t.Fatalf("button press after burst: expected active-output=0, got %v — pool left unusable after exhaustion",
			kvsValue(deviceState, "script/pool-pump/active-output"))
	}

	cancel()
	<-done
}

// TestPoolPump_CallSlotDeepChainDefersInsteadOfRecursing pins down #421/#450
// item 3 ("deep call chains defer rather than recurse"): a chain nested
// deeper than MAX_CALL_DEPTH (3) must not invoke its next completion inline
// — that inline invocation is exactly the unbounded stack growth #450's live
// crash trace shows ("Too much recursion ... in function acquireCallSlot").
// The deferred completion must still run, just later, off the task queue.
func TestPoolPump_CallSlotDeepChainDefersInsteadOfRecursing(t *testing.T) {
	buf := append(readPoolPumpScript(t), []byte(callSlotTestHarnessJS)...)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)
	deviceState := newCallSlotDeviceState(injector)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	if !waitPoolPumpInit(t, deviceState) {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	injector <- callSlotEvent("test.callslot.run-single-chain", 0)

	// Published synchronously, in the same call stack that fired the chain.
	if !waitFor(eventTimeout, 20*time.Millisecond, func() bool {
		_, ok := deviceState.KVSValue("test/callslot/solo-log-len-immediate")
		return ok
	}) {
		cancel()
		<-done
		t.Fatalf("single chain never ran (harness event handler not wired up)")
	}
	if v := parseKVSInt(t, deviceState, "test/callslot/solo-log-len-immediate"); v != 3 {
		t.Fatalf("expected only 3 nested completions to run inline before the 4th is deferred (MAX_CALL_DEPTH=3), got %d — "+
			"the 4th level recursed synchronously instead of deferring, which is the #450 stack-overflow crash", v)
	}

	// The deferred 4th completion must still run — later, off the task queue.
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		return kvsValue(deviceState, "test/callslot/chain-log-len") == "4"
	}) {
		t.Fatalf("deferred completion never ran; chain-log-len = %v, want 4 — deferred work was dropped, not just delayed",
			kvsValue(deviceState, "test/callslot/chain-log-len"))
	}

	cancel()
	<-done
}

// TestPoolPump_CallSlotErrorPathReleasesSlot pins down #421/#450 item 4
// ("error paths release their slot"): sharedCallDone's non-deferred branch
// releases a call's slot unconditionally, before invoking the caller's
// callback, regardless of whether the RPC succeeded or errored. This drives
// a call (Schedule.Update on a non-existent schedule id) that always returns
// error -105, at CALL_DEPTH 0 so it takes the non-deferred branch, and
// checks the slot count is identical immediately before and after.
func TestPoolPump_CallSlotErrorPathReleasesSlot(t *testing.T) {
	buf := append(readPoolPumpScript(t), []byte(callSlotTestHarnessJS)...)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)
	deviceState := newCallSlotDeviceState(injector)

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	if !waitPoolPumpInit(t, deviceState) {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	injector <- callSlotEvent("test.callslot.run-error-check", 0)

	if !waitFor(eventTimeout, 20*time.Millisecond, func() bool {
		return kvsValue(deviceState, "test/callslot/error-check-done") == "1"
	}) {
		cancel()
		<-done
		t.Fatalf("error-path check never completed")
	}

	errorCode := parseKVSInt(t, deviceState, "test/callslot/error-code")
	if errorCode == 0 {
		t.Fatalf("expected a non-zero error code from Schedule.Update on an unknown id, got %d — test didn't exercise the error path", errorCode)
	}
	before := parseKVSInt(t, deviceState, "test/callslot/slots-before")
	after := parseKVSInt(t, deviceState, "test/callslot/slots-after")
	if after != before {
		t.Fatalf("used-slot count changed from %d to %d across an errored call — slot leaked on the error path", before, after)
	}

	cancel()
	<-done
}
