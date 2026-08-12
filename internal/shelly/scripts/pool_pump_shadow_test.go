package scripts

import (
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
)

// === Step 3 of #476: shadow-mode observation ===
//
// TEMPORARY, deleted together with the shadow scaffolding in step 4.
//
// pool-pump.js currently computes desiredOutput() on every reconcile() and
// appends the result to Script.storage["shadow"] without acting on it, as
// "<reason>=<want>|<have>;", with a trailing "!" on any entry where the
// policy disagreed with the settled relay state. This test drives the
// scenarios the live logic already has coverage for and prints the whole
// trail, so every divergence can be explained BEFORE the reconciler is
// allowed to drive the relay. It asserts nothing: it is an observation
// harness, and a silent pass here would be worthless.
func TestPoolPump_ShadowModeTrail(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name  string
		state *script.DeviceState
		drive func(injector chan []byte)
	}{
		{
			name: "pro1-button-then-water-flap",
			state: &script.DeviceState{
				KVS: pro1KVS(), Storage: map[string]interface{}{},
				ComponentStatus: pro1ComponentStatus(), Schedules: pro1Schedules(),
			},
			drive: func(inj chan []byte) {
				inj <- shellyButtonEvent()
				time.Sleep(1500 * time.Millisecond)
				inj <- shellyInputEvent(0, true)
				time.Sleep(1500 * time.Millisecond)
				inj <- shellyInputEvent(0, false)
				time.Sleep(1500 * time.Millisecond)
			},
		},
		{
			name: "pro3-running-at-boot-then-water",
			state: func() *script.DeviceState {
				cs := pro3ComponentStatus()
				cs["switch:2"] = map[string]interface{}{"id": 2, "output": true}
				return &script.DeviceState{
					KVS: controllerKVS(), Storage: map[string]interface{}{},
					ComponentStatus: cs, Schedules: poolPumpSchedules(),
				}
			}(),
			drive: func(inj chan []byte) {
				time.Sleep(1500 * time.Millisecond)
				inj <- shellyInputEvent(0, true)
				time.Sleep(1500 * time.Millisecond)
			},
		},
		{
			name: "pro1-summer-window-contains-now",
			state: &script.DeviceState{
				KVS:             poolPumpWindowKVS(poolPumpWideRunHours),
				Storage:         map[string]interface{}{"schedule-mode": "summer"},
				ComponentStatus: pro1ComponentStatus(),
				Schedules:       poolPumpSummerSchedules(now.Add(-1*time.Hour), now.Add(1*time.Hour)),
			},
			drive: func(inj chan []byte) { time.Sleep(2500 * time.Millisecond) },
		},
		{
			name: "pro1-summer-window-excludes-now-pump-running",
			state: func() *script.DeviceState {
				cs := pro1ComponentStatus()
				cs["switch:0"] = map[string]interface{}{"id": 0, "output": true}
				return &script.DeviceState{
					KVS:             poolPumpWindowKVS(poolPumpWideRunHours),
					Storage:         map[string]interface{}{"schedule-mode": "summer"},
					ComponentStatus: cs,
					Schedules:       poolPumpSummerSchedules(now.Add(-5*time.Hour), now.Add(-4*time.Hour)),
				}
			}(),
			drive: func(inj chan []byte) { time.Sleep(2500 * time.Millisecond) },
		},
	}

	buf := readPoolPumpScript(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mqtt.ResetClient()
			mqtt.SetClient(mqtt.NewMockClient())
			t.Cleanup(mqtt.ResetClient)

			injector := make(chan []byte, 8)
			tc.state.EventInjector = injector

			ctx, cancel := poolPumpRunContext(t)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, tc.state)
			}()

			if !waitFor(initTimeout, 200*time.Millisecond, func() bool {
				_, ok := tc.state.KVSValue("script/pool-pump/schedule-mode")
				return ok
			}) {
				cancel()
				<-done
				t.Fatalf("init timeout")
			}

			tc.drive(injector)

			trail, _ := tc.state.StorageValue("shadow")
			settled := kvsValue(tc.state, "script/pool-pump/active-output")
			cancel()
			<-done

			t.Logf("SHADOW settled active-output=%v\n  trail=%v", settled, trail)
		})
	}
}
