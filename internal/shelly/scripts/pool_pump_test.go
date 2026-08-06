package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
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

// activeNightWindowSchedules returns poolPumpSchedules() with the night-run
// window (handleNightStart/handleNightStop) widened to bracket time.Now()
// instead of the fixed 23:15/00:15 literals. Tests that leave the pump
// already "on" in ComponentStatus at boot (simulating a legitimate
// in-progress run carried across a restart) need this: pool-pump.js's #421
// Bug B restart catch-up compares "now" against the currently active mode's
// schedule window and calls doStop() if they disagree. scheduleMode defaults
// to "winter" when Storage doesn't carry a saved mode (as in these
// fixtures), so catch-up looks at the night-start/night-stop jobs — and the
// fixed 23:15-00:15 band essentially never contains the real time these
// tests run at, which would otherwise make catch-up immediately stop the
// pump these tests are trying to exercise.
func activeNightWindowSchedules() []map[string]interface{} {
	schedules := poolPumpSchedules()
	now := time.Now()
	start := now.Add(-30 * time.Minute)
	stop := now.Add(3 * time.Hour)
	startSpec := fmt.Sprintf("0 %d %d * * SUN,MON,TUE,WED,THU,FRI,SAT", start.Minute(), start.Hour())
	stopSpec := fmt.Sprintf("0 %d %d * * SUN,MON,TUE,WED,THU,FRI,SAT", stop.Minute(), stop.Hour())
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
		switch params["code"] {
		case "handleNightStart()":
			job["timespec"] = startSpec
		case "handleNightStop()":
			job["timespec"] = stopSpec
		}
	}
	return schedules
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

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		10*time.Second,
	)
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

	ok := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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
		Schedules:       activeNightWindowSchedules(),
		EventInjector:   injector,
	}

	// 20s ceiling: init (up to 9s) + two water-supply transitions (up to 5s
	// each, see below) — generous headroom, not the expected runtime.
	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		20*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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
	stopped := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})
	if !stopped {
		t.Fatalf("pump did not stop after water supply ON; active-output = %v",
			deviceState.KVS["script/pool-pump/active-output"])
	}

	injector <- shellyInputEvent(0, false)
	restored := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
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

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		15*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	// Wait for init.
	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("button press 1: expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Press 2: 0 → 1
	injector <- shellyButtonEvent()
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "1"
	}) {
		t.Fatalf("button press 2: expected active-output=1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Press 3: 1 → 2
	injector <- shellyButtonEvent()
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "2"
	}) {
		t.Fatalf("button press 3: expected active-output=2, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Press 4: 2 → off (exercises turnOffAllSwitches)
	injector <- shellyButtonEvent()
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
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
	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		25*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	// Wait for init.
	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("Pro1 toggle on: expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Button press: toggle OFF (exercises turnOffAllSwitches on Pro1)
	injector <- shellyButtonEvent()
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	}) {
		t.Fatalf("Pro1 toggle off: expected active-output=-1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Toggle ON again for water supply test.
	injector <- shellyButtonEvent()
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
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
	if !waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	}) {
		t.Fatalf("Pro1 water supply ON: expected active-output=-1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Water supply OFF → should restore switch:0.
	injector <- shellyInputEvent(0, false)
	if !waitFor(5*time.Second, 50*time.Millisecond, func() bool {
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
		Schedules:       activeNightWindowSchedules(),
		EventInjector:   injector,
	}

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		15*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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
	stopped := waitFor(3*time.Second, 50*time.Millisecond, func() bool {
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
		Schedules:       activeNightWindowSchedules(),
		EventInjector:   injector,
	}

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		15*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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
	stopped := waitFor(3*time.Second, 50*time.Millisecond, func() bool {
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

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		10*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		20*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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

	started := waitFor(3*time.Second, 50*time.Millisecond, func() bool {
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

	stopped := waitFor(3*time.Second, 50*time.Millisecond, func() bool {
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

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		15*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		15*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init timeout")
	}

	injector <- shellyButtonEvent()
	started := waitFor(2*time.Second, 50*time.Millisecond, func() bool {
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

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		15*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
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

// === #421 Bug A: "Too many calls in progress" concurrent-call ceiling ===
//
// Reproduces the live crash from issue #421 by recreating its actual trigger:
// activateOutput()'s runtime-accounting hooks (stopRuntimeAccounting ->
// persistRuntimeState, and setOutput's saveState() callback) queue
// storeValue() fire-and-forget KVS writes via queueTask() — the same two
// call sites the live crash dumps named (persistRuntimeState's turnover
// mirror, saveState's active-output mirror). processTaskQueue's 200ms tick
// only serializes *when* those queued task functions run — it has no idea
// how many of the underlying async Shelly.call RPCs are still in flight.
//
// This drives four rapid, alternating water-supply toggle events at a pump
// that's already running. Because each toggle's own Switch.Set call is
// deferred (DeviceState.CallDelay simulates a real RPC's async round-trip),
// STATE.activeOutput stays stale across the whole burst, so every "water
// supply ON" toggle sees a spurious wasRunning->false transition and queues
// two more persistRuntimeState writes each time — on top of the four
// directly-dispatched Switch.Set calls themselves. That combination
// reliably pushes concurrent in-flight Shelly.call RPCs past the real
// device's 5-call ceiling, recreating "Too many calls in progress" (see the
// two crash dumps quoted in issue #421).
func TestPoolPump_ConcurrentCallCeiling_WaterSupplyBurstDoesNotCrash(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	// Generous: boot alone (26 sequential config keys, each round-tripping
	// through CallDelay) takes on the order of 15-20s with this delay, plus
	// a 10s settle buffer (see below) and the burst/drain phase.
	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		90*time.Second,
	)
	defer cancel()

	injector := make(chan []byte, 8)

	deviceState := &script.DeviceState{
		KVS:             pro1KVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro1ComponentStatus(),
		Schedules:       pro1Schedules(),
		EventInjector:   injector,
		Mode:            script.DeviceTestMode,
		// Long enough that four rapid-fire Switch.Set dispatches (injected
		// within microseconds of each other) all stay "in flight"
		// simultaneously across several 200ms task-queue ticks — recreating
		// the real overlap between the task queue's fixed cadence and an
		// RPC's actual completion time. Short enough that, once the fix
		// throttles processTaskQueue's own dispatch against real in-flight
		// count, the whole burst still drains within this test's deadline.
		CallDelay: 500 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	initDone := waitFor(60*time.Second, 200*time.Millisecond, func() bool {
		_, exists := deviceState.KVS["script/pool-pump/schedule-mode"]
		return exists
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("init did not complete within timeout")
	}

	// schedule-mode appears early (right after STATE.initializing flips to
	// false) — well before the 4 sequential initSteps (Sys.SetConfig,
	// applyComponentNames, verifySchedules, ...), restartCatchUp(), and
	// handleDailyCheck()'s forecast fetch finish draining, each of which
	// dispatches its own CallDelay-deferred Shelly.call. Give that leftover
	// init activity time to fully settle so the burst below is the only
	// concurrent-call activity in play — otherwise it can trip the ceiling
	// (or fail to, post-fix) for reasons unrelated to what this test targets.
	time.Sleep(10 * time.Second)

	// Turn the pump on first (button press bypasses activateOutput() per
	// existing test comments, so it doesn't itself contribute to the
	// concurrent-call count) and wait for it to actually land, so the burst
	// below starts from a known, settled "running" state.
	injector <- shellyButtonEvent()
	if !waitFor(3*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("pump did not turn on before the concurrent-call burst")
	}

	// Fire the burst: four alternating water-supply transitions with no
	// delay in between (see the doc comment above).
	injector <- shellyInputEvent(0, true)
	injector <- shellyInputEvent(0, false)
	injector <- shellyInputEvent(0, true)
	injector <- shellyInputEvent(0, false)

	deadline := time.After(20 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var runErr error
settleLoop:
	for {
		select {
		case runErr = <-done:
			break settleLoop
		case <-deadline:
			break settleLoop
		case <-ticker.C:
			// No specific end state to poll for (the deliberately-raced
			// toggles make the final active-output unpredictable) — just
			// give queued/deferred work time to drain without crashing.
		}
	}

	cancel()
	if runErr == nil {
		runErr = <-done
	}

	if runErr != nil && strings.Contains(runErr.Error(), "Too many calls in progress") {
		t.Fatalf("script crashed on the concurrent-call ceiling (issue #421 Bug A): %v", runErr)
	}
}

// === #421 Bug B: restart catch-up against the active schedule window ===
//
// enforceOutputState() only mirrors whatever physical switch state it finds
// at boot — it never asks "should I actually be running right now, given
// today's schedule window?". This simulates a restart landing well outside
// the active summer-mode window (morning-start/evening-stop, computed here
// relative to time.Now() so the test is deterministic regardless of when it
// runs) with the pump physically left "on", and asserts the fixed
// restartCatchUp() path calls doStop() and the switch ends up off — without
// waiting for any schedule tick (there is no cron emulation in this
// harness).
func TestPoolPump_RestartCatchUp_StopsOutsideScheduleWindow(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	now := time.Now()
	winStart := now.Add(3 * time.Hour)
	winStop := now.Add(4 * time.Hour)
	startSpec := fmt.Sprintf("0 %d %d * * SUN,MON,TUE,WED,THU,FRI,SAT", winStart.Minute(), winStart.Hour())
	stopSpec := fmt.Sprintf("0 %d %d * * SUN,MON,TUE,WED,THU,FRI,SAT", winStop.Minute(), winStop.Hour())

	schedules := []map[string]interface{}{
		{
			"id": 1, "enable": true,
			"timespec": "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": "handleDailyCheck()"},
			}},
		},
		{
			"id": 2, "enable": true,
			"timespec": startSpec, // 3h from now — "now" is outside this window
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": "handleMorningStart()"},
			}},
		},
		{
			"id": 3, "enable": true,
			"timespec": stopSpec,
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": "handleEveningStop()"},
			}},
		},
		{
			"id": 4, "enable": false, // disabled: summer mode, night schedules off
			"timespec": "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": "handleNightStart()"},
			}},
		},
		{
			"id": 5, "enable": false,
			"timespec": "0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT",
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": "handleNightStop()"},
			}},
		},
	}

	cs := pro1ComponentStatus()
	cs["switch:0"] = map[string]interface{}{"id": 0, "output": true} // physically on at "restart"

	deviceState := &script.DeviceState{
		KVS:             pro1KVS(),
		Storage:         map[string]interface{}{"schedule-mode": "summer"},
		ComponentStatus: cs,
		Schedules:       schedules,
	}

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		15*time.Second,
	)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	stopped := waitFor(10*time.Second, 100*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})
	cancel()
	<-done

	if !stopped {
		t.Fatalf("restart outside scheduled window: expected catch-up to stop the pump (active-output=-1), got %v",
			deviceState.KVS["script/pool-pump/active-output"])
	}

	entry, ok := deviceState.ComponentStatus["switch:0"].(map[string]interface{})
	if !ok {
		t.Fatalf("switch:0 component status missing or malformed: %v", deviceState.ComponentStatus["switch:0"])
	}
	if on, _ := entry["output"].(bool); on {
		t.Errorf("restart outside scheduled window: switch:0 still physically on after catch-up")
	}
}
