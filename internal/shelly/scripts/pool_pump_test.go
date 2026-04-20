package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
		Schedules:       poolPumpSchedules(),
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
