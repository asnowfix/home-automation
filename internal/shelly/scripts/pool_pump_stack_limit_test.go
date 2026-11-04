package scripts

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
)

// Stack-depth sweep for #661 / #496. Opt-in (POOLPUMP_STACK_SWEEP=1): each
// scenario is binary-searched on goja's call-stack limit, which costs minutes.
//
// For a scenario, the "minimal surviving limit" is the smallest
// DeviceState.MaxCallStackSize at which pool-pump.js init completes AND no
// goja stack overflow is recorded anywhere (script.StackOverflows(), which
// also sees overflows inside Go-invoked callbacks that the emulator
// otherwise swallows) AND the later phases (forced day-rollover flush,
// injected handleNightStop()) behave. That minimum is the scenario's maximum
// goja call-frame depth. goja counts JS call frames; Espruino is bounded by C
// stack consumed by expression nesting (#496), so this is a weak proxy for
// the device. See docs/pool-pump-winter-crash-progress.md for results.

type stackScenario struct {
	name      string
	relayOn   bool // switch:0 ON when the script starts
	inWindow  bool // winter night window contains "now"
	yesterday bool // runtime record (and run-start, if relay ON) dated yesterday
	logging   bool // KVS logging=true (production setting)
}

type stackPhaseResult struct {
	initOK     bool
	flushOK    bool
	nightOK    bool
	overflowsI int64 // overflows seen by the end of init + settle
	overflowsF int64 // ... after the forced rollover flush
	overflowsN int64 // ... after handleNightStop
}

func (r stackPhaseResult) survived() bool {
	return r.initOK && r.flushOK && r.nightOK && r.overflowsN == 0
}

func (r stackPhaseResult) String() string {
	return fmt.Sprintf("init=%v(ovf %d) flush=%v(ovf %d) night=%v(ovf %d)",
		r.initOK, r.overflowsI, r.flushOK, r.overflowsF, r.nightOK, r.overflowsN)
}

func winterJobs(nowMin int, inWindow bool) []map[string]interface{} {
	job := func(id int, code, timespec string, enable bool) map[string]interface{} {
		return map[string]interface{}{
			"id": id, "enable": enable, "timespec": timespec,
			"calls": []interface{}{map[string]interface{}{
				"method": "script.eval",
				"params": map[string]interface{}{"id": 1, "code": code},
			}},
		}
	}
	at := func(deltaMin int) string {
		m := ((nowMin+deltaMin)%1440 + 1440) % 1440
		return poolPumpTimespec(m/60, m%60)
	}
	startD, stopD := -30, 30
	if !inWindow {
		startD, stopD = 120, 180
	}
	return []map[string]interface{}{
		job(1, "handleDailyCheck()", "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", true),
		job(2, "handleMorningStart()", poolPumpTimespec(11, 9), false),
		job(3, "handleEveningStop()", poolPumpTimespec(14, 51), false),
		job(4, "handleNightStart()", at(startD), true),
		job(5, "handleNightStop()", at(stopD), true),
	}
}

func runStackScenario(t *testing.T, sc stackScenario, limit int) stackPhaseResult {
	t.Helper()
	var res stackPhaseResult
	srv := poolPumpWinterForecastServer(t)
	now := time.Now()
	nowMin := now.Hour()*60 + now.Minute()

	kvs := pro1KVS()
	kvs["script/pool-pump/logging"] = strconv.FormatBool(sc.logging)
	cs := pro1ComponentStatus()
	cs["switch:0"] = map[string]interface{}{"id": 0, "output": sc.relayOn}

	injector := make(chan []byte, 4)
	ds := &script.DeviceState{
		KVS: kvs,
		Storage: map[string]interface{}{
			"schedule-mode": "winter",
			"forecast-url":  srv.URL + "?daily=sunrise,sunset",
		},
		ComponentStatus:      cs,
		Schedules:            winterJobs(nowMin, sc.inWindow),
		ScheduleEvalInjector: injector,
		MaxCallStackSize:     limit,
	}
	stamp := now
	if sc.yesterday {
		stamp = now.AddDate(0, 0, -1)
	}
	seedRuntimeStorage(ds, 1234, stamp.Unix())
	if sc.relayOn {
		ds.SetStorageValue(runStartStorageKey, strconv.FormatInt(stamp.UnixMilli(), 10))
	}

	script.ResetStackOverflows()
	ctx, cancel := poolPumpRunContext(t, mqtt.NewMockClient())
	defer cancel()
	buf := readPoolPumpScript(t)
	done := make(chan error, 1)
	go func() { done <- script.RunWithDeviceState(ctx, "pool-pump.js", buf, false, ds) }()
	defer func() { cancel(); <-done }()

	runWindowSeen := func() bool {
		for _, e := range ds.EmittedEvents() {
			if e.Name == "pool.run_window" {
				return true
			}
		}
		return false
	}
	// Completion of init: schedule-mode written (saveState), the daily check's
	// forecast parse reached its pool.run_window emit, or an overflow stopped
	// the show (fail fast).
	res.initOK = waitFor(8*time.Second, 50*time.Millisecond, func() bool {
		if script.StackOverflows() > 0 {
			return true
		}
		_, exists := ds.KVSValue("script/pool-pump/schedule-mode")
		return exists && runWindowSeen()
	})
	if script.StackOverflows() > 0 {
		res.initOK = false
	}
	time.Sleep(1200 * time.Millisecond) // let queued runtime closures / Step 4 drain
	res.overflowsI = script.StackOverflows()
	if res.overflowsI > 0 {
		res.overflowsF, res.overflowsN = res.overflowsI, res.overflowsI
		return res
	}

	// Forced day rollover at the 60 s runtime checkpoint (only meaningful
	// while a run is open, i.e. relay ON at this point).
	yday := now.AddDate(0, 0, -1).Unix()
	injector <- mustJSON(map[string]string{"code": fmt.Sprintf(
		"STATE.runtimeTs = %d; runtimeFlushFire();", yday)})
	time.Sleep(1200 * time.Millisecond)
	res.flushOK = true
	res.overflowsF = script.StackOverflows()

	// Night stop with the window moved away from "now": relay must end OFF.
	a, b := (nowMin+120)%1440, (nowMin+180)%1440
	injector <- mustJSON(map[string]string{"code": fmt.Sprintf(
		"F_WIN_START = %d; F_WIN_STOP = %d; handleNightStop();", a, b)})
	res.nightOK = waitFor(5*time.Second, 50*time.Millisecond, func() bool {
		v, ok := ds.ComponentStatusValue("switch:0")
		m, _ := v.(map[string]interface{})
		on, _ := m["output"].(bool)
		return ok && !on
	})
	time.Sleep(500 * time.Millisecond)
	res.overflowsN = script.StackOverflows()
	return res
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// minSurvivingLimit binary-searches the smallest limit in [1, hi] at which the
// scenario survives. Returns 0 if even hi does not survive. Assumes
// monotonicity, then confirms by also running min-1 (must fail).
func minSurvivingLimit(t *testing.T, sc stackScenario, hi int) (int, stackPhaseResult) {
	t.Helper()
	if r := runStackScenario(t, sc, hi); !r.survived() {
		t.Logf("%s: does not survive even limit %d: %s", sc.name, hi, r)
		return 0, r
	}
	lo := 1 // lo-1 is known failing (limit 0 = unlimited is not usable here)
	for lo < hi {
		mid := (lo + hi) / 2
		if runStackScenario(t, sc, mid).survived() {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, runStackScenario(t, sc, lo-1)
}

func TestPoolPump_StackDepthSweep(t *testing.T) {
	if os.Getenv("POOLPUMP_STACK_SWEEP") != "1" {
		t.Skip("set POOLPUMP_STACK_SWEEP=1 to run the goja call-stack-depth sweep (#661/#496)")
	}
	scenarios := []stackScenario{
		{name: "OFF/outside/today (baseline)", relayOn: false, inWindow: false, yesterday: false, logging: true},
		{name: "OFF/outside/yesterday", relayOn: false, inWindow: false, yesterday: true, logging: true},
		{name: "OFF/inside/today", relayOn: false, inWindow: true, yesterday: false, logging: true},
		{name: "OFF/inside/yesterday", relayOn: false, inWindow: true, yesterday: true, logging: true},
		{name: "ON/outside/today", relayOn: true, inWindow: false, yesterday: false, logging: true},
		{name: "ON/outside/yesterday", relayOn: true, inWindow: false, yesterday: true, logging: true},
		{name: "ON/inside/today", relayOn: true, inWindow: true, yesterday: false, logging: true},
		{name: "ON/inside/yesterday (prod failure shape)", relayOn: true, inWindow: true, yesterday: true, logging: true},
		{name: "ON/inside/yesterday, logging off", relayOn: true, inWindow: true, yesterday: true, logging: false},
	}
	for _, sc := range scenarios {
		if r := runStackScenario(t, sc, 0); !r.survived() {
			t.Errorf("%s: harness broken, unlimited run does not survive: %s", sc.name, r)
			continue
		}
		min, below := minSurvivingLimit(t, sc, 40)
		t.Logf("SWEEP %-45s min surviving limit = %d ; at limit-1: %s", sc.name, min, below)
	}
}
