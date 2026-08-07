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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	cancel()
	<-done

	if !ok {
		t.Fatalf("timed out waiting for init to complete")
	}
}

func TestPoolPump_WaterSupplyRestoresSpeed(t *testing.T) {
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

	// 20s ceiling: init (up to 9s) + two water-supply transitions (up to 5s
	// each, see below) — generous headroom, not the expected runtime.
	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("script did not complete init within timeout")
	}

	if v := deviceState.KVS["script/pool-pump/active-output"]; v != "2" {
		t.Fatalf("expected active-output=2 after init, got %v", v)
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
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})
	if !stopped {
		t.Fatalf("pump did not stop after water supply ON; active-output = %v",
			deviceState.KVS["script/pool-pump/active-output"])
	}

	injector <- shellyInputEvent(0, false)
	restored := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "2"
	})

	cancel()
	<-done

	if !restored {
		t.Fatalf("pump speed not restored after water supply OFF; active-output = %v",
			deviceState.KVS["script/pool-pump/active-output"])
	}
}

// TestPoolPump_ButtonCyclesPro3 verifies that sys_btn_push events cycle
// through speeds: off → 0 → 1 → 2 → off (the last transition exercises turnOffAllSwitches).
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Start from off.
	if v := deviceState.KVS["script/pool-pump/active-output"]; v != "-1" {
		t.Fatalf("expected active-output=-1 before button presses, got %v", v)
	}

	// Press 1: off → 0
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("button press 1: expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Press 2: 0 → 1
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "1"
	}) {
		t.Fatalf("button press 2: expected active-output=1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Press 3: 1 → 2
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "2"
	}) {
		t.Fatalf("button press 3: expected active-output=2, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Press 4: 2 → off (exercises turnOffAllSwitches)
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	}) {
		t.Fatalf("button press 4: expected active-output=-1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Verify all switches are off.
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("switch:%d", i)
		if entry, ok := deviceState.ComponentStatus[key]; ok {
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("Pro1 init timeout")
	}

	// Should start off.
	if v := deviceState.KVS["script/pool-pump/active-output"]; v != "-1" {
		t.Fatalf("Pro1: expected active-output=-1 after init, got %v", v)
	}

	// Button press: toggle ON
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("Pro1 toggle on: expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Button press: toggle OFF (exercises turnOffAllSwitches on Pro1)
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	}) {
		t.Fatalf("Pro1 toggle off: expected active-output=-1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Toggle ON again for water supply test.
	injector <- shellyButtonEvent()
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("Pro1 toggle on (2): expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Water supply ON → should turn off. 5s (not 2s): activateOutput() now
	// also queues the #402 runtime/turnover KVS mirror writes ahead of this
	// transition's own active-output write — see the comment in
	// TestPoolPump_WaterSupplyRestoresSpeed.
	injector <- shellyInputEvent(0, true)
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	}) {
		t.Fatalf("Pro1 water supply ON: expected active-output=-1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Water supply OFF → should restore switch:0.
	injector <- shellyInputEvent(0, false)
	if !waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("Pro1 water supply OFF: expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	cancel()
	<-done
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
	raw, ok := deviceState.KVS[key]
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
	raw, ok := deviceState.KVS[key]
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
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
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})
	elapsed := time.Since(runStart)
	cancel()
	<-done

	if !stopped {
		t.Fatalf("pump did not stop after water supply ON; active-output = %v", deviceState.KVS["script/pool-pump/active-output"])
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
func TestPoolPump_RuntimeAccounting_ContinuesAfterReboot(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)

	cs := pro3ComponentStatus()
	cs["switch:2"] = map[string]interface{}{"id": 2, "output": true} // pump left "on" across the simulated reboot

	const baselineSec = 3600 // 1h of runtime accrued before the "reboot"

	deviceState := &script.DeviceState{
		KVS: controllerKVS(),
		Storage: map[string]interface{}{
			"runtime-sec":  fmt.Sprintf("%d", baselineSec),
			"runtime-date": dateString(time.Now()),
		},
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	// Short real wait, then stop, to confirm accumulation continues from the
	// restored baseline rather than resetting to 0.
	time.Sleep(1 * time.Second)

	injector <- shellyInputEvent(0, true)
	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})
	cancel()
	<-done

	if !stopped {
		t.Fatalf("pump did not stop after water supply ON; active-output = %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	runtimeSec := parseKVSInt(t, deviceState, "script/pool-pump/runtime-sec")
	if runtimeSec <= baselineSec {
		t.Fatalf("runtime-sec %d did not increase past persisted baseline %d — accounting reset instead of continuing",
			runtimeSec, baselineSec)
	}

	turnoverToday := parseKVSFloat(t, deviceState, "script/pool-pump/turnover-today")
	wantTurnover := expectedTurnover(float64(runtimeSec))
	if math.Abs(turnoverToday-wantTurnover) > 0.01 {
		t.Errorf("turnover-today = %v, want ~%v (runtime_sec=%d)", turnoverToday, wantTurnover, runtimeSec)
	}
}

// TestPoolPump_RuntimeAccounting_StaleDateResetsOnBoot verifies
// ensureRuntimeDay()'s day-rollover reset: Script.storage is pre-seeded with
// a runtime-date from "yesterday" plus a large leftover runtime-sec — as if
// the device was left running overnight and only rebooted the next day (or
// the daemon never stopped it before midnight). At boot, loadState() must
// discard the stale total (reset to 0) rather than carry it into today,
// which is the same reset path exercised — for an in-progress run instead of
// at boot — by the flushRuntimeCheckpoint()/handleNightStop() mid-run
// rollover fix (ensureRuntimeDay() pulling STATE.runStartTs forward to
// Date.now() so a still-open run doesn't re-credit its pre-midnight time to
// the new day). That exact mid-run interleaving isn't independently
// reproducible from this Go harness: Date.now()/Timer.set are tied to the
// real system clock with no injectable virtual clock, so there's no
// deterministic way to force STATE.runtimeDate stale while STATE.runStartTs
// is non-null without a real device restart in between (which always goes
// through this same boot-time reset path).
func TestPoolPump_RuntimeAccounting_StaleDateResetsOnBoot(t *testing.T) {
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

	ctx, cancel := poolPumpRunContext(t)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(initTimeout, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	cancel()
	<-done

	if !initDone {
		t.Fatalf("init timeout")
	}

	if got := deviceState.Storage["runtime-date"]; got != dateString(time.Now()) {
		t.Errorf("Storage runtime-date = %v, want today (%s)", got, dateString(time.Now()))
	}
	if got := deviceState.Storage["runtime-sec"]; got != "0" {
		t.Errorf("Storage runtime-sec = %v, want \"0\" (stale total must not carry over)", got)
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
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
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0" // eco switch, per controllerKVS()'s eco-speed=0
	})
	if !started {
		cancel()
		<-done
		t.Fatalf("solar start: expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Fresh (ts = now), below the 200W stop threshold.
	if err := mc.Publish(ctx, "myhome/energy/solar/available", solarPayload(100, time.Now().Unix()), 0, true, "test"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	stopped := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})

	cancel()
	<-done

	if !stopped {
		t.Fatalf("solar stop: expected active-output=-1, got %v", deviceState.KVS["script/pool-pump/active-output"])
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
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

	if v := deviceState.KVS["script/pool-pump/active-output"]; v != "-1" {
		t.Fatalf("solar hard ceiling: expected pump to stay off, got active-output=%v", v)
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	injector <- shellyButtonEvent()
	started := waitFor(eventTimeout, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	})

	cancel()
	<-done

	if !started {
		t.Fatalf("expected button press to start pump despite solar enabled/never-received, got active-output=%v",
			deviceState.KVS["script/pool-pump/active-output"])
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
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
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

	if v := deviceState.KVS["script/pool-pump/active-output"]; v != "-1" {
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
// script.eval code matches, so a test can prove the rewrite actually landed
// before asserting on what the rewrite should have caused.
func poolPumpScheduleTimespec(schedules []map[string]interface{}, code string) string {
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
		if !ok || params["code"] != code {
			continue
		}
		if ts, ok := job["timespec"].(string); ok {
			return ts
		}
	}
	return ""
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
	initial := poolPumpScheduleTimespec(deviceState.Schedules, "handleMorningStart()")
	if !waitFor(15*time.Second, 100*time.Millisecond, func() bool {
		return poolPumpScheduleTimespec(deviceState.Schedules, "handleMorningStart()") != initial
	}) {
		cancel()
		<-done
		t.Fatalf("schedule rewrite never happened (morning start still %q)", initial)
	}

	// Then give the reconciliation its own window to act — or to provably not.
	waitFor(8*time.Second, 100*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == wantOutput
	})

	got, _ := deviceState.KVS["script/pool-pump/active-output"].(string)
	schedules := deviceState.Schedules
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
