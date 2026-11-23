package scripts

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
)

// Behaviour pins for the #864 init-path allocation arms (loadConfig's
// per-key KVS.Get callback, finishContinueInit's initSteps closures, the
// Schedule.List continuation). Those refactors replace per-call closures with
// named functions; nothing about WHAT init reads, writes or in which order is
// allowed to change, and the heap effect itself is invisible to the emulator.
// These tests pin the observable results: the CONFIG values loadConfig
// produces for every parse/default path, and the facts init's four steps set.

const initDumpKey = "test/dump"

// initDumpCode makes the running script write the globals the pins read into
// a KVS key, which the test then reads back (the emulator offers no direct
// way to evaluate-and-return).
const initDumpCode = `Shelly.call("KVS.Set", {key: "` + initDumpKey + `", value: JSON.stringify({` +
	`cfg: CONFIG, water: F_WATER, ws: F_WIN_START, we: F_WIN_STOP, init: STATE.initializing})});`

type initDump struct {
	Cfg   map[string]interface{} `json:"cfg"`
	Water bool                   `json:"water"`
	Ws    float64                `json:"ws"`
	We    float64                `json:"we"`
	Init  bool                   `json:"init"`
}

// runInitAndDump boots pool-pump.js with ds, then repeatedly dumps the script
// globals until done(dump) is true or the timeout elapses. It returns the last
// dump seen and whether done was satisfied.
func runInitAndDump(t *testing.T, ds *script.DeviceState, timeout time.Duration, done func(initDump) bool) (initDump, bool) {
	t.Helper()
	buf := readPoolPumpScript(t)
	mc := mqtt.NewMockClient()
	ctx, cancel := poolPumpRunContext(t, mc)
	defer cancel()

	evalCh := make(chan []byte, 64)
	ds.ScheduleEvalInjector = evalCh

	exited := make(chan error, 1)
	go func() {
		exited <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, ds)
	}()

	var last initDump
	ok := waitFor(timeout, 150*time.Millisecond, func() bool {
		if err := evalPoolPumpSchedule(ds, initDumpCode); err != nil {
			t.Fatalf("eval: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		raw, found := ds.KVSString(initDumpKey)
		if !found {
			return false
		}
		var d initDump
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatalf("decode dump %q: %v", raw, err)
		}
		last = d
		return done(d)
	})
	cancel()
	<-exited
	return last, ok
}

// TestPoolPump_LoadConfigValueParsing pins loadConfig's per-key handling for
// every branch: a valid number, a valid boolean, a non-"true" boolean, a
// non-numeric number (falls back to the default), an empty value (default), a
// missing key (the emulator answers KVS.Get with error -114: default), a
// string, and a key never mentioned (default). A cliOnly key is never read.
func TestPoolPump_LoadConfigValueParsing(t *testing.T) {
	t.Parallel()

	kvs := controllerKVS()
	delete(kvs, "script/pool-pump/eco-speed")            // missing key -> error path -> default 2
	kvs["script/pool-pump/pool-volume"] = "abc"          // invalid number -> default 46
	kvs["script/pool-pump/turnover"] = ""                // empty -> default 5
	kvs["script/pool-pump/max-rpm"] = "3000"             // valid number
	kvs["script/pool-pump/max-temp"] = "0"               // zero is a value, not "unset"
	kvs["script/pool-pump/solar-enabled"] = "true"       // boolean true
	kvs["script/pool-pump/logging"] = "false"            // boolean false (default is true)
	kvs["script/pool-pump/speed"] = "day"                // string
	kvs["script/pool-pump/solar-stop-w"] = "TRUE"        // number key: Number("TRUE") is NaN -> default 200
	kvs["script/pool-pump/night-duration"] = "123"       // cliOnly: must stay at its default
	kvs["script/pool-pump/mqtt-topic"] = "should/ignore" // cliOnly: must stay at its default
	// every other key is absent -> default

	ds := &script.DeviceState{
		KVS:             kvs,
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       poolPumpSchedules(),
	}
	d, ok := runInitAndDump(t, ds, initTimeout, func(d initDump) bool { return len(d.Cfg) > 0 && !d.Init })
	if !ok {
		t.Fatalf("init did not complete; last dump %+v", d)
	}

	want := map[string]interface{}{
		"ecoSpeed":             float64(2),
		"poolVolume":           float64(46),
		"turnover":             float64(5),
		"maxRpm":               float64(3000),
		"maxTemp":              float64(0),
		"solarEnabled":         true,
		"enableLogging":        false,
		"preferredSpeed":       "day",
		"solarStopThresholdW":  float64(200),
		"nightRunDurationMs":   float64(3600000), // default: cliOnly keys are not loaded
		"mqttTopicPrefix":      "pool/pump",      // default: cliOnly keys are not loaded
		"daySpeed":             float64(1),       // absent -> default
		"maxSpeed":             float64(0),       // absent -> default
		"temperatureThreshold": float64(20),      // present, valid
		"solarStartThresholdW": float64(500),
		"solarStaleMs":         float64(300000),
		"overrideMs":           float64(7200000),
	}
	for k, w := range want {
		if got, in := d.Cfg[k]; !in || !reflect.DeepEqual(got, w) {
			t.Errorf("CONFIG.%s = %v (present=%v), want %v", k, got, in, w)
		}
	}
}

// TestPoolPump_InitStepsSetFacts pins what finishContinueInit's four steps
// leave behind: step 2 reads input:0 and asserts the water fact, step 4 reads
// Schedule.List and seeds the run window from it (winter mode at boot: the
// night start/stop jobs, 23:15 and 00:15).
func TestPoolPump_InitStepsSetFacts(t *testing.T) {
	t.Parallel()

	cs := pro3ComponentStatus()
	cs["input:0"] = map[string]interface{}{"id": 0, "state": true}
	ds := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: cs,
		Schedules:       poolPumpSchedules(),
	}
	d, ok := runInitAndDump(t, ds, initTimeout, func(d initDump) bool { return d.Ws >= 0 })
	if !ok {
		t.Fatalf("run window was never seeded from Schedule.List; last dump %+v", d)
	}
	if !d.Water {
		t.Errorf("F_WATER = false, want true (step 2 reads input:0 state=true)")
	}
	if d.Ws != 23*60+15 || d.We != 15 {
		t.Errorf("run window = %v-%v, want %v-%v (night start/stop jobs)", d.Ws, d.We, 23*60+15, 15)
	}
}

// TestPoolPump_InitStepsNoSchedulesStopsBeforeWindow pins step 4's fatal
// path: with no pool schedule job the chain stops (the window is never seeded)
// and the script does not crash. The negative is observed over a bounded
// settle period: the four steps take well under it.
func TestPoolPump_InitStepsNoSchedulesStopsBeforeWindow(t *testing.T) {
	t.Parallel()

	ds := &script.DeviceState{
		KVS:             controllerKVS(),
		Storage:         make(map[string]interface{}),
		ComponentStatus: pro3ComponentStatus(),
		Schedules:       nil,
	}
	d, ok := runInitAndDump(t, ds, 6*time.Second, func(d initDump) bool { return d.Ws >= 0 })
	if ok {
		t.Fatalf("run window seeded without any pool schedule: %+v", d)
	}
	if d.Cfg == nil || d.Init {
		t.Errorf("script did not reach post-init state (or died): %+v", d)
	}
	if d.Ws != -1 || d.We != -1 {
		t.Errorf("run window = %v-%v, want unset -1--1", d.Ws, d.We)
	}
}
