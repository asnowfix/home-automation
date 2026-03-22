// pool_pump_test.go — integration tests for pool-pump.js using the Goja runtime.
//
// These tests run the real pool-pump.js script inside the Shelly mock runtime
// (createShellyRuntime) and verify behaviour end-to-end:
//
//  1. TestPoolPump_ControllerCreates4Schedules — verifies that a controller
//     device initialises and registers all four expected schedules.
//  2. TestPoolPump_NightStopEventStopsPump — injects a pool-pump/night-stop
//     device event and confirms that all pump switches are turned off.
package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"pkg/shelly/mqtt"
	"pkg/shelly/script"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// poolPumpScript is the relative path from this package to pool-pump.js.
const poolPumpScriptPath = "pool-pump.js"

// readPoolPumpScript reads pool-pump.js from the source tree.
// Tests run with the working directory set to the package directory, so the
// relative path above resolves correctly.
func readPoolPumpScript(t *testing.T) []byte {
	t.Helper()
	buf, err := os.ReadFile(poolPumpScriptPath)
	if err != nil {
		t.Fatalf("failed to read pool-pump.js: %v", err)
	}
	return buf
}

// controllerKVS returns a KVS map pre-seeded with all required configuration
// keys for a controller device (Pro3).
func controllerKVS() map[string]interface{} {
	return map[string]interface{}{
		"script/pool-pump/device-role":    "controller",
		"script/pool-pump/controller-id":  "shellypro3-aabbcc112233",
		"script/pool-pump/bootstrap-id":   "shellypro1-ddeeff445566",
		"script/pool-pump/mqtt-topic":     "pool/pump",
		"script/pool-pump/logging":        "false", // keep test output quiet
		"script/pool-pump/eco-speed":      "0",
		"script/pool-pump/mid-speed":      "1",
		"script/pool-pump/high-speed":     "2",
		"script/pool-pump/boot-duration":  "120000",
		"script/pool-pump/night-duration": "3600000",
		"script/pool-pump/boot-delay":     "500",
		"script/pool-pump/boot-hours":     "6",
	}
}

// pro3ComponentStatus returns component statuses that make the script detect a
// Pro3 (3-switch) device with all switches off and no water-supply active.
func pro3ComponentStatus() map[string]interface{} {
	return map[string]interface{}{
		"switch:0": map[string]interface{}{"id": 0, "output": false},
		"switch:1": map[string]interface{}{"id": 1, "output": false},
		"switch:2": map[string]interface{}{"id": 2, "output": false},
		"input:0":  map[string]interface{}{"id": 0, "state": false},
		"input:1":  map[string]interface{}{"id": 1, "state": false},
		"input:2":  map[string]interface{}{"id": 2, "state": false},
		"mqtt":     map[string]interface{}{"connected": true},
		"sys":      map[string]interface{}{"device_id": "shellypro3-aabbcc112233"},
	}
}

// waitFor polls pred every pollInterval until it returns true or the deadline
// is reached. Returns true if pred eventually returned true.
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

// shellyEvent builds a JSON-encoded Shelly device event that the script's
// Shelly.addEventHandler callback understands.
func shellyEvent(eventName string) []byte {
	event := map[string]interface{}{
		"component": "schedule:1",
		"info": map[string]interface{}{
			"component": "schedule:1",
			"event":     eventName,
		},
	}
	data, _ := json.Marshal(event)
	return data
}

// TestPoolPump_ControllerCreates5Schedules runs pool-pump.js as a controller
// and verifies that all five expected schedules are registered after init.
//
// Schedule timespecs expected:
//
//	daily-check   : @sunrise * * SUN,...
//	morning-start : @sunrise+3h * * SUN,...
//	evening-stop  : @sunset * * SUN,...
//	night-start   : 0 15 23 * * SUN,...
//	night-stop    : 0 15 0 * * SUN,...
func TestPoolPump_ControllerCreates5Schedules(t *testing.T) {
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
	}

	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, deviceState)
	}()

	wantTimespecs := []string{
		"@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT",
		"@sunrise+3h * * SUN,MON,TUE,WED,THU,FRI,SAT",
		"@sunset * * SUN,MON,TUE,WED,THU,FRI,SAT",
		"0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT",
		"0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT",
	}

	// Poll until all 5 schedules are created (or timeout).
	ok := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
		return len(deviceState.Schedules) >= len(wantTimespecs)
	})
	cancel() // stop the script
	<-done

	if !ok {
		t.Fatalf("timed out waiting for schedules: got %d, want %d\nSchedules: %+v",
			len(deviceState.Schedules), len(wantTimespecs), deviceState.Schedules)
	}

	// Build a set of actual timespecs.
	got := make(map[string]bool)
	for _, s := range deviceState.Schedules {
		if ts, ok := s["timespec"].(string); ok {
			got[ts] = true
		}
	}

	for _, want := range wantTimespecs {
		if !got[want] {
			t.Errorf("missing schedule timespec %q\ngot: %v", want, deviceState.Schedules)
		}
	}
}

// TestPoolPump_NightStopEventStopsPump runs pool-pump.js as a controller with
// switch 2 (high speed) initially on, waits for initialisation, injects a
// pool-pump/night-stop device event, and then verifies that all switches are
// turned off (ComponentStatus) and that the active-output KVS key is "-1".
func TestPoolPump_NightStopEventStopsPump(t *testing.T) {
	buf := readPoolPumpScript(t)

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	injector := make(chan []byte, 4)

	// Start with switch 2 on so the script knows the pump is running.
	cs := pro3ComponentStatus()
	cs["switch:2"] = map[string]interface{}{"id": 2, "output": true}

	deviceState := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: cs,
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

	// Wait for all 4 schedules to appear — that signals init is complete.
	initDone := waitFor(9*time.Second, 200*time.Millisecond, func() bool {
		return len(deviceState.Schedules) >= 4
	})
	if !initDone {
		cancel()
		<-done
		t.Fatalf("script did not complete init within timeout")
	}

	// Inject night-stop event.
	injector <- shellyEvent("pool-pump/night-stop")

	// Wait for the pump to be switched off.  activateOutput(-1) sets a 200 ms
	// timer before flipping switches, then saveState() writes KVS — allow 1 s.
	stopped := waitFor(1500*time.Millisecond, 50*time.Millisecond, func() bool {
		v, ok := deviceState.KVS["script/pool-pump/active-output"]
		return ok && v == "-1"
	})

	cancel()
	<-done

	if !stopped {
		t.Fatalf("pump did not stop after night-stop event; KVS active-output = %v",
			deviceState.KVS["script/pool-pump/active-output"])
	}

	// Confirm all switches are off in ComponentStatus.
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("switch:%d", i)
		if entry, ok := deviceState.ComponentStatus[key]; ok {
			if m, ok := entry.(map[string]interface{}); ok {
				if on, _ := m["output"].(bool); on {
					t.Errorf("switch %d still on after night-stop", i)
				}
			}
		}
	}
}
