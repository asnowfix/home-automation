package scripts

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

const bluListenerScriptPath = "blu-listener.js"

func readBluListenerScript(t *testing.T) []byte {
	t.Helper()
	buf, err := os.ReadFile(bluListenerScriptPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", bluListenerScriptPath, err)
	}
	return buf
}

func bluFollowKVS(mac, switchID string, autoOff float64) map[string]interface{} {
	v, _ := json.Marshal(map[string]interface{}{
		"switch_id": switchID,
		"auto_off":  autoOff,
	})
	return map[string]interface{}{
		"follow/shelly-blu/" + mac: string(v),
	}
}

func bluFollowKVSWithBounds(mac, switchID string, autoOff float64, illumMin, illumMax interface{}) map[string]interface{} {
	cfg := map[string]interface{}{
		"switch_id": switchID,
		"auto_off":  autoOff,
	}
	if illumMin != nil {
		cfg["illuminance_min"] = illumMin
	}
	if illumMax != nil {
		cfg["illuminance_max"] = illumMax
	}
	v, _ := json.Marshal(cfg)
	return map[string]interface{}{
		"follow/shelly-blu/" + mac: string(v),
	}
}

// injectBluEvent sends a shelly-blu event via the EventInjector channel.
// The script's onEventData handler routes these to handleBluEvent.
func injectBluEvent(t *testing.T, injector chan []byte, mac string, illuminance int, motion int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]interface{}{
		"address":     mac,
		"illuminance": illuminance,
		"motion":      motion,
		"battery":     90,
	})
	event, _ := json.Marshal(map[string]interface{}{
		"info": map[string]interface{}{
			"event":   "shelly-blu",
			"address": "shelly-blu/events/" + mac,
			"data":    string(payload),
		},
	})
	injector <- event
}

func switchIsOn(deviceState *script.DeviceState, switchKey string) bool {
	if deviceState.ComponentStatus == nil {
		return false
	}
	v, ok := deviceState.ComponentStatus[switchKey]
	if !ok {
		return false
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	on, ok := m["output"].(bool)
	return ok && on
}

func newBluListenerState(kvs map[string]interface{}) *script.DeviceState {
	return &script.DeviceState{
		KVS:     kvs,
		Storage: make(map[string]interface{}),
		ComponentStatus: map[string]interface{}{
			"switch:0": map[string]interface{}{"id": 0, "output": false},
			"switch:1": map[string]interface{}{"id": 1, "output": false},
		},
		EventInjector: make(chan []byte, 8),
	}
}

func runBluListener(ctx context.Context, t *testing.T, buf []byte, state *script.DeviceState) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- script.RunWithDeviceState(ctx, bluListenerScriptPath, buf, false, state)
	}()
	// Allow script to initialize: KVS.List + KVS.Get chain + onLoadFollowsComplete
	time.Sleep(300 * time.Millisecond)
	return done
}

// TestBluListener_MotionTurnsOnSwitch verifies that a BLU motion event from a
// followed MAC turns on the configured switch.
func TestBluListener_MotionTurnsOnSwitch(t *testing.T) {
	buf := readBluListenerScript(t)
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	mac := "aa:bb:cc:dd:ee:ff"
	state := newBluListenerState(bluFollowKVS(mac, "switch:0", 0))

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		10*time.Second,
	)
	defer cancel()

	done := runBluListener(ctx, t, buf, state)

	injectBluEvent(t, state.EventInjector, mac, 50, 1)

	ok := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		return switchIsOn(state, "switch:0")
	})
	cancel()
	<-done

	if !ok {
		t.Fatal("switch:0 was not turned on after BLU motion event")
	}
}

// TestBluListener_NoMotionNoAction verifies that a BLU event with motion=0
// does not turn on the switch.
func TestBluListener_NoMotionNoAction(t *testing.T) {
	buf := readBluListenerScript(t)
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	mac := "aa:bb:cc:dd:ee:01"
	state := newBluListenerState(bluFollowKVS(mac, "switch:0", 0))

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		5*time.Second,
	)
	defer cancel()

	done := runBluListener(ctx, t, buf, state)

	injectBluEvent(t, state.EventInjector, mac, 50, 0) // motion=0

	time.Sleep(400 * time.Millisecond)
	cancel()
	<-done

	if switchIsOn(state, "switch:0") {
		t.Fatal("switch:0 was turned on despite motion=0")
	}
}

// TestBluListener_AutoOffTurnsOffSwitch verifies that the auto-off timer fires
// and turns the switch off after the configured duration.
func TestBluListener_AutoOffTurnsOffSwitch(t *testing.T) {
	buf := readBluListenerScript(t)
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	mac := "11:22:33:44:55:66"
	// 0.3 s auto-off to keep the test fast
	state := newBluListenerState(bluFollowKVS(mac, "switch:0", 0.3))

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		10*time.Second,
	)
	defer cancel()

	done := runBluListener(ctx, t, buf, state)

	injectBluEvent(t, state.EventInjector, mac, 50, 1)

	onOk := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		return switchIsOn(state, "switch:0")
	})
	if !onOk {
		cancel()
		<-done
		t.Fatal("switch:0 was not turned on")
	}

	offOk := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		return !switchIsOn(state, "switch:0")
	})
	cancel()
	<-done

	if !offOk {
		t.Fatal("switch:0 was not turned off by auto-off timer")
	}
}

// TestBluListener_IlluminanceTooHigh verifies that when illuminance >= max,
// the switch is NOT turned on.
func TestBluListener_IlluminanceTooHigh(t *testing.T) {
	buf := readBluListenerScript(t)
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	mac := "aa:11:bb:22:cc:33"
	state := newBluListenerState(bluFollowKVSWithBounds(mac, "switch:0", 0, nil, 100))

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		5*time.Second,
	)
	defer cancel()

	done := runBluListener(ctx, t, buf, state)

	// illuminance == max (100) → blocked by strict < check
	injectBluEvent(t, state.EventInjector, mac, 100, 1)

	time.Sleep(400 * time.Millisecond)
	cancel()
	<-done

	if switchIsOn(state, "switch:0") {
		t.Fatal("switch:0 was turned on despite illuminance >= illuminance_max")
	}
}

// TestBluListener_IlluminanceTooLow verifies that when illuminance <= min,
// the switch is NOT turned on.
func TestBluListener_IlluminanceTooLow(t *testing.T) {
	buf := readBluListenerScript(t)
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	mac := "aa:11:bb:22:cc:44"
	state := newBluListenerState(bluFollowKVSWithBounds(mac, "switch:0", 0, 50, nil))

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		5*time.Second,
	)
	defer cancel()

	done := runBluListener(ctx, t, buf, state)

	// illuminance == min (50) → blocked by strict > check
	injectBluEvent(t, state.EventInjector, mac, 50, 1)

	time.Sleep(400 * time.Millisecond)
	cancel()
	<-done

	if switchIsOn(state, "switch:0") {
		t.Fatal("switch:0 was turned on despite illuminance <= illuminance_min")
	}
}

// TestBluListener_IlluminanceWithinBoundsTurnsOn verifies that an event within
// configured lux bounds does trigger the switch.
func TestBluListener_IlluminanceWithinBoundsTurnsOn(t *testing.T) {
	buf := readBluListenerScript(t)
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	mac := "aa:11:bb:22:cc:55"
	// min=20, max=100 → illuminance=60 should pass
	state := newBluListenerState(bluFollowKVSWithBounds(mac, "switch:0", 0, 20, 100))

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		10*time.Second,
	)
	defer cancel()

	done := runBluListener(ctx, t, buf, state)

	injectBluEvent(t, state.EventInjector, mac, 60, 1)

	ok := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		return switchIsOn(state, "switch:0")
	})
	cancel()
	<-done

	if !ok {
		t.Fatal("switch:0 was not turned on for illuminance within bounds")
	}
}

// TestBluListener_KVSReloadPicksUpNewFollow verifies that after a KVS change
// event, a newly configured follow MAC becomes active.
func TestBluListener_KVSReloadPicksUpNewFollow(t *testing.T) {
	buf := readBluListenerScript(t)
	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	// Start with no follows
	state := newBluListenerState(map[string]interface{}{})

	ctx, cancel := context.WithTimeout(
		logr.NewContext(context.Background(), testr.New(t)),
		10*time.Second,
	)
	defer cancel()

	done := runBluListener(ctx, t, buf, state)

	// Add follow config to KVS while script is running
	mac := "ff:ee:dd:cc:bb:aa"
	v, _ := json.Marshal(map[string]interface{}{
		"switch_id": "switch:0",
		"auto_off":  0,
	})
	state.KVS["follow/shelly-blu/"+mac] = string(v)

	// Inject KVS change event to trigger reload
	kvsEvent, _ := json.Marshal(map[string]interface{}{
		"info": map[string]interface{}{
			"event":  "kvs",
			"key":    "follow/shelly-blu/" + mac,
			"action": "set",
		},
	})
	state.EventInjector <- kvsEvent

	// Allow reload chain to complete
	time.Sleep(400 * time.Millisecond)

	injectBluEvent(t, state.EventInjector, mac, 50, 1)

	ok := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		return switchIsOn(state, "switch:0")
	})
	cancel()
	<-done

	if !ok {
		t.Fatal("switch:0 not turned on after KVS reload added new follow")
	}
}
