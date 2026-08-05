package scripts

import (
	"context"
	"encoding/json"
	"fmt"
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
// Bug B restart catch-up (restartCatchUp(), wired into continueInit())
// compares "now" against the currently active mode's schedule window and
// calls doStop() if they disagree. scheduleMode defaults to "winter" when
// Storage doesn't carry a saved mode (as in these fixtures), so catch-up
// looks at the night-start/night-stop jobs — and the fixed 23:15-00:15 band
// essentially never contains the real time these tests run at, which would
// otherwise make catch-up immediately stop the pump these tests are trying
// to exercise.
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

// shellySwitchEvent simulates a raw device-level "switch:<id> state changed"
// event, the same shape Shelly.addEventHandler receives on real hardware
// when a switch physically flips. handleSwitchEvent's Pro1 branches call
// saveState() synchronously in response — no Shelly.call round trip needed
// to trigger it — which is what makes it useful for piling multiple
// queueTask() entries onto TASK_QUEUE back-to-back, faster than any single
// RPC's round trip.
func shellySwitchEvent(switchID int, state bool) []byte {
	event := map[string]interface{}{
		"info": map[string]interface{}{
			"component": fmt.Sprintf("switch:%d", switchID),
			"id":        switchID,
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
	if !initDone {
		cancel()
		<-done
		t.Fatalf("script did not complete init within timeout")
	}

	if v := deviceState.KVS["script/pool-pump/active-output"]; v != "2" {
		t.Fatalf("expected active-output=2 after init, got %v", v)
	}

	injector <- shellyInputEvent(0, true)
	stopped := waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})
	if !stopped {
		t.Fatalf("pump did not stop after water supply ON; active-output = %v",
			deviceState.KVS["script/pool-pump/active-output"])
	}

	injector <- shellyInputEvent(0, false)
	restored := waitFor(2*time.Second, 50*time.Millisecond, func() bool {
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

	// Water supply ON → should turn off.
	injector <- shellyInputEvent(0, true)
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	}) {
		t.Fatalf("Pro1 water supply ON: expected active-output=-1, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	// Water supply OFF → should restore switch:0.
	injector <- shellyInputEvent(0, false)
	if !waitFor(2*time.Second, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "0"
	}) {
		t.Fatalf("Pro1 water supply OFF: expected active-output=0, got %v", deviceState.KVS["script/pool-pump/active-output"])
	}

	cancel()
	<-done
}

// === #421 Bug A: "Too many calls in progress" concurrent-call ceiling ===
//
// Reproduces the live crash from issue #421: processTaskQueue's 200ms tick
// only serializes *when* queued task functions run — it has no idea how many
// of the underlying async Shelly.call RPCs those tasks fire are still in
// flight. handleSwitchEvent's Pro1 branches call saveState() synchronously
// in direct response to a raw switch-state event (no Shelly.call round trip
// needed to trigger it), and saveState() queues two fire-and-forget
// storeValue() writes (active-output, schedule-mode) per call — exactly the
// storeValue()/queueTask() call sites issue #421's live crash dumps named.
// Injecting a rapid burst of switch:0 events piles many queueTask() entries
// onto TASK_QUEUE almost instantly; a task queue that dispatches one new
// Shelly.call per fixed 200ms tick regardless of how many previous ones are
// still in flight (DeviceState.CallDelay simulates each RPC's real,
// slower-than-200ms round trip) reliably pushes concurrent in-flight calls
// past the real device's 5-call ceiling, recreating "Too many calls in
// progress" (see issue #421).
func TestPoolPump_ConcurrentCallCeiling_SwitchEventBurstDoesNotCrash(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	// Generous: boot alone (18 sequential config keys, each round-tripping
	// through CallDelay) takes on the order of 20-25s with this delay, plus
	// a settle buffer and the burst/drain phase.
	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		90*time.Second,
	)
	defer cancel()

	injector := make(chan []byte, 16)

	deviceState := &script.DeviceState{
		KVS:             pro1KVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro1ComponentStatus(),
		Schedules:       pro1Schedules(),
		EventInjector:   injector,
		Mode:            script.DeviceTestMode,
		// Long enough (relative to the fixed 200ms task-queue tick) that
		// several queued Shelly.call dispatches stay "in flight"
		// simultaneously — a ~1.2s/200ms = 6x pileup factor comfortably
		// exceeds the real device's 5-call ceiling once enough tasks are
		// queued, while still draining within this test's deadline once the
		// fix's MAX_CALLS_IN_FLIGHT=4 throttle caps concurrency.
		CallDelay: 1200 * time.Millisecond,
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
	time.Sleep(8 * time.Second)

	// Fire the burst: eight alternating raw switch:0 state events with no
	// delay in between. Each synchronously triggers saveState() (two
	// queueTask() calls each) — see the doc comment above.
	for i := 0; i < 8; i++ {
		injector <- shellySwitchEvent(0, i%2 == 0)
	}

	deadline := time.After(25 * time.Second)
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
