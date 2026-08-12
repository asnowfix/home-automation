package scripts

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

const gardenScriptPath = "garden.js"

// gardenVM loads garden.js into a fresh goja runtime with the minimal stubs
// needed for its top-level (synchronous) code to run to completion: a Shelly
// object whose call()/addEventHandler() are no-ops, so the async
// loadConfig()->loadZones()->continueInit()->handlePlan() chain kicked off by
// the trailing init() call fires exactly one Shelly.call and then halts
// (its callback is never invoked) instead of reaching the network — and a
// Script.storage backed by an in-memory map so loadDeficit/saveGroupLastDay
// etc. work when invoked directly by tests below.
//
// This intentionally bypasses init()'s async KVS/forecast machinery (already
// exercised end-to-end by TestSmokeAllScripts and by live device testing) in
// order to unit-test the synchronous group-cadence algorithm in
// computeZonePlan() in isolation, with deterministic inputs. Zone config
// comes from the script's own ZONE_DEFAULTS (loaded synchronously by
// initZones() at module load time), not from KVS.
func gardenVM(t *testing.T) *goja.Runtime {
	t.Helper()

	buf, err := os.ReadFile(gardenScriptPath)
	if err != nil {
		t.Fatalf("failed to read garden.js: %v", err)
	}

	vm := goja.New()

	shellyObj := vm.NewObject()
	shellyObj.Set("call", func(call goja.FunctionCall) goja.Value { return goja.Undefined() })
	shellyObj.Set("addEventHandler", func(call goja.FunctionCall) goja.Value { return goja.Undefined() })
	vm.Set("Shelly", shellyObj)

	vm.Set("print", func(call goja.FunctionCall) goja.Value { return goja.Undefined() })

	storage := make(map[string]string)
	storageObj := vm.NewObject()
	storageObj.Set("getItem", func(call goja.FunctionCall) goja.Value {
		if v, ok := storage[call.Argument(0).String()]; ok {
			return vm.ToValue(v)
		}
		return goja.Null()
	})
	storageObj.Set("setItem", func(call goja.FunctionCall) goja.Value {
		storage[call.Argument(0).String()] = call.Argument(1).String()
		return goja.Undefined()
	})
	scriptObj := vm.NewObject()
	scriptObj.Set("storage", storageObj)
	vm.Set("Script", scriptObj)

	if _, err := vm.RunString(string(buf)); err != nil {
		t.Fatalf("garden.js failed to load: %v", err)
	}

	return vm
}

// gardenVMWithScheduleList extends gardenVM's stub set with a Shelly.call
// that answers "Schedule.List" from a caller-supplied job list and records
// every "Schedule.Update" call, a Timer stub that runs queueTask()'s
// deferred callback synchronously (updatePlanSchedule() and verifySchedules()
// both finish through the task queue, and there is no real event loop here),
// and a print() stub that captures log lines so a test can distinguish
// "Garden schedules verified" from "FATAL: Garden schedules missing" without
// a live device.
//
// Used by the #480 regression tests below to prove updatePlanSchedule() and
// verifySchedules() still find their jobs once the `code` field carries
// wrapScheduleCall()'s wrapped source instead of the bare handler call.
func gardenVMWithScheduleList(t *testing.T) (vm *goja.Runtime, setJobs func([]map[string]interface{}), updates *[]map[string]interface{}, logLines *[]string) {
	t.Helper()

	buf, err := os.ReadFile(gardenScriptPath)
	if err != nil {
		t.Fatalf("failed to read garden.js: %v", err)
	}

	vm = goja.New()
	var jobs []map[string]interface{}
	var updatesSlice []map[string]interface{}
	var lines []string

	shellyObj := vm.NewObject()
	shellyObj.Set("call", func(call goja.FunctionCall) goja.Value {
		method := call.Argument(0).String()
		cb, _ := goja.AssertFunction(call.Argument(2))
		switch method {
		case "Schedule.List":
			result := map[string]interface{}{"jobs": jobs}
			if cb != nil {
				if _, err := cb(goja.Undefined(), vm.ToValue(result), goja.Undefined()); err != nil {
					t.Fatalf("Schedule.List callback: %v", err)
				}
			}
		case "Schedule.Update":
			params, _ := call.Argument(1).Export().(map[string]interface{})
			updatesSlice = append(updatesSlice, params)
			if cb != nil {
				if _, err := cb(goja.Undefined(), goja.Undefined(), goja.Undefined()); err != nil {
					t.Fatalf("Schedule.Update callback: %v", err)
				}
			}
		}
		return goja.Undefined()
	})
	shellyObj.Set("addEventHandler", func(call goja.FunctionCall) goja.Value { return goja.Undefined() })
	shellyObj.Set("getCurrentScriptId", func(call goja.FunctionCall) goja.Value { return vm.ToValue(1) })
	vm.Set("Shelly", shellyObj)

	vm.Set("print", func(call goja.FunctionCall) goja.Value {
		lines = append(lines, call.Argument(0).String())
		return goja.Undefined()
	})

	timerObj := vm.NewObject()
	timerObj.Set("set", func(call goja.FunctionCall) goja.Value {
		fn, _ := goja.AssertFunction(call.Argument(2))
		if fn != nil {
			if _, err := fn(goja.Undefined()); err != nil {
				t.Fatalf("Timer.set callback: %v", err)
			}
		}
		return vm.ToValue(1)
	})
	timerObj.Set("clear", func(call goja.FunctionCall) goja.Value { return goja.Undefined() })
	vm.Set("Timer", timerObj)

	storage := make(map[string]string)
	storageObj := vm.NewObject()
	storageObj.Set("getItem", func(call goja.FunctionCall) goja.Value {
		if v, ok := storage[call.Argument(0).String()]; ok {
			return vm.ToValue(v)
		}
		return goja.Null()
	})
	storageObj.Set("setItem", func(call goja.FunctionCall) goja.Value {
		storage[call.Argument(0).String()] = call.Argument(1).String()
		return goja.Undefined()
	})
	scriptObj := vm.NewObject()
	scriptObj.Set("storage", storageObj)
	vm.Set("Script", scriptObj)

	if _, err := vm.RunString(string(buf)); err != nil {
		t.Fatalf("garden.js failed to load: %v", err)
	}

	return vm, func(j []map[string]interface{}) { jobs = j }, &updatesSlice, &lines
}

// gardenWrappedJob builds a Schedule.List job entry whose script.eval code is
// wrapScheduleCall(handlerCall)'d, mirroring what a live device carries after
// #480's createSchedules() change.
func gardenWrappedJob(id int, handlerCall, timespec string) map[string]interface{} {
	wrapped := "(function(){try{" + handlerCall + "}catch(e){log('schedule handler error:',e)}})()"
	return map[string]interface{}{
		"id": id, "enable": true, "timespec": timespec,
		"calls": []interface{}{map[string]interface{}{
			"method": "script.eval",
			"params": map[string]interface{}{"id": 1, "code": wrapped},
		}},
	}
}

// TestGarden_UpdatePlanSchedule_MatchesWrappedCode is the #480 regression
// guard for updatePlanSchedule(): with the handleWateringStart() job's code
// field wrapped (as garden.js's own createSchedules() now writes it), the
// job must still be found by id and rewritten. Before the substring-match
// fix this silently no-ops (jobId stays -1, "WARNING: handleWateringStart()
// schedule not found", no Schedule.Update call) instead of erroring loudly.
func TestGarden_UpdatePlanSchedule_MatchesWrappedCode(t *testing.T) {
	vm, setJobs, updates, logLines := gardenVMWithScheduleList(t)

	setJobs([]map[string]interface{}{
		gardenWrappedJob(1, "handlePlan()", "0 30 0 * * SUN,MON,TUE,WED,THU,FRI,SAT"),
		gardenWrappedJob(7, "handleWateringStart()", "0 0 5 * * SUN,MON,TUE,WED,THU,FRI,SAT"),
	})

	mustEval(t, vm, `updatePlanSchedule(6)`)

	if len(*updates) != 1 {
		t.Fatalf("expected exactly 1 Schedule.Update call, got %d (log: %v)", len(*updates), *logLines)
	}
	got := (*updates)[0]
	if id, _ := got["id"].(int64); id != 7 {
		t.Errorf("expected Schedule.Update on job id 7 (the wrapped handleWateringStart() job), got id=%v", got["id"])
	}
	wantTs := "0 0 6 * * SUN,MON,TUE,WED,THU,FRI,SAT"
	if ts, _ := got["timespec"].(string); ts != wantTs {
		t.Errorf("expected timespec %q, got %q", wantTs, ts)
	}
}

// TestGarden_VerifySchedules_MatchesWrappedCode is the #480 regression guard
// for verifySchedules(): with both jobs' code fields wrapped, it must log
// success ("Garden schedules verified"), not the FATAL missing-schedules
// message, and cb must still run.
func TestGarden_VerifySchedules_MatchesWrappedCode(t *testing.T) {
	vm, setJobs, _, logLines := gardenVMWithScheduleList(t)

	setJobs([]map[string]interface{}{
		gardenWrappedJob(1, "handlePlan()", "0 30 0 * * SUN,MON,TUE,WED,THU,FRI,SAT"),
		gardenWrappedJob(2, "handleWateringStart()", "0 0 5 * * SUN,MON,TUE,WED,THU,FRI,SAT"),
	})

	mustEval(t, vm, `var __cbRan = false; verifySchedules(function() { __cbRan = true; });`)

	if ran := mustEval(t, vm, `__cbRan`).ToBoolean(); !ran {
		t.Fatalf("verifySchedules() callback never ran (log: %v)", *logLines)
	}

	joined := strings.Join(*logLines, "\n")
	if strings.Contains(joined, "FATAL") {
		t.Errorf("verifySchedules() reported schedules missing with wrapped code (log: %v)", *logLines)
	}
	if !strings.Contains(joined, "Garden schedules verified") {
		t.Errorf("expected \"Garden schedules verified\" in log, got: %v", *logLines)
	}
}

func mustEval(t *testing.T, vm *goja.Runtime, code string) goja.Value {
	t.Helper()
	v, err := vm.RunString(code)
	if err != nil {
		t.Fatalf("eval %q: %v", code, err)
	}
	return v
}

func zonePlanIDs(t *testing.T, raw string) map[int]int {
	t.Helper()
	var plan []struct {
		ID      int `json:"id"`
		Minutes int `json:"minutes"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatalf("unmarshal plan %q: %v", raw, err)
	}
	got := make(map[int]int, len(plan))
	for _, p := range plan {
		got[p.ID] = p.Minutes
	}
	return got
}

// TestGarden_LawnFiresTogetherBedsIndependent verifies the core grouping
// behaviour added for differentiated cadence: lawn zones (0 and 1, group
// "lawn") water together as soon as either crosses its trigger, while massifs
// (zone 2, group "beds") is gated independently by its own trigger.
func TestGarden_LawnFiresTogetherBedsIndependent(t *testing.T) {
	vm := gardenVM(t)

	// The lawn zones ship disabled (23e7307, "disable zone-0/1 by default, as
	// KVS is not read (yet)"), and computeZonePlan() skips disabled zones
	// before any grouping logic runs — so with the shipped defaults this test
	// would assert nothing at all. Enable them explicitly: what is under test
	// here is the grouping rule, not which zones happen to be switched on in
	// production. TestGarden_DisabledZoneStaysOutOfPlan below covers the
	// shipped default itself.
	mustEval(t, vm, `ZONES[0].enabled = true; ZONES[1].enabled = true;`)

	// Zone 0 above its 12mm trigger, zone 1 below it (but in the same "lawn"
	// group as zone 0), zone 2 (massifs) below its 8mm trigger.
	mustEval(t, vm, `storeStorageValue(deficitKey(0), 15);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(1), 9);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(2), 3);`)

	raw := mustEval(t, vm, `JSON.stringify(computeZonePlan())`).String()
	plan := zonePlanIDs(t, raw)

	if _, ok := plan[0]; !ok {
		t.Errorf("expected zone 0 (over trigger) in plan, got %v", plan)
	}
	if _, ok := plan[1]; !ok {
		t.Errorf("expected lawn zone 1 to fire together with zone 0, got %v", plan)
	}
	if _, ok := plan[2]; ok {
		t.Errorf("expected massifs (zone 2, below its trigger) to stay excluded, got %v", plan)
	}
}

// TestGarden_GroupCadenceGate verifies that a group already watered within
// its intervalDays window is excluded regardless of deficit, and becomes
// eligible again once enough days have passed. It reads bedsInterval from the
// live ZONES config rather than hardcoding it, so the test tracks whatever
// the script's current default is instead of silently going stale.
func TestGarden_GroupCadenceGate(t *testing.T) {
	vm := gardenVM(t)

	// Same reason as above: with the shipped defaults the lawn zones are
	// disabled, which would make this test's two "lawn stays gated"
	// assertions pass vacuously — they would hold even if cadence gating were
	// completely broken. Enable them so the gate is actually exercised.
	mustEval(t, vm, `ZONES[0].enabled = true; ZONES[1].enabled = true;`)

	// All zones comfortably over trigger so deficit never gates the result —
	// only the group-cadence check should determine inclusion/exclusion.
	mustEval(t, vm, `storeStorageValue(deficitKey(0), 25);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(1), 25);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(2), 25);`)

	bedsInterval := mustEval(t, vm, `ZONES[2].intervalDays`).ToInteger()
	if bedsInterval < 1 {
		t.Fatalf("unexpected beds intervalDays: %d", bedsInterval)
	}

	// Mark both groups watered "today" — both groups must be excluded.
	mustEval(t, vm, `saveGroupLastDay('lawn'); saveGroupLastDay('beds');`)
	if raw := mustEval(t, vm, `JSON.stringify(computeZonePlan())`).String(); raw != "[]" {
		t.Fatalf("expected empty plan right after both groups watered, got %s", raw)
	}

	// Roll beds back exactly far enough to become due again; lawn
	// (intervalDays=1) is still "watered today" so it must stay excluded even
	// though beds reappears in the plan.
	mustEval(t, vm, fmt.Sprintf(
		`storeStorageValue(groupLastKey('beds'), todayDayNumber() - %d);`, bedsInterval))

	raw := mustEval(t, vm, `JSON.stringify(computeZonePlan())`).String()
	plan := zonePlanIDs(t, raw)

	if _, ok := plan[0]; ok {
		t.Errorf("lawn zone 0 should still be gated (watered today), got %v", plan)
	}
	if _, ok := plan[1]; ok {
		t.Errorf("lawn zone 1 should still be gated (watered today), got %v", plan)
	}
	if _, ok := plan[2]; !ok {
		t.Errorf("expected massifs (zone 2) to be due again, got %v", plan)
	}
}

// TestGarden_DisabledZoneStaysOutOfPlan covers the shipped ZONE_DEFAULTS
// directly: the lawn zones are disabled (23e7307, "disable zone-0/1 by
// default, as KVS is not read (yet)"), so they must not appear in a plan even
// when their deficit is far over trigger, while the enabled beds zone still
// does.
//
// This exists so the default is asserted by a test whose subject it actually
// is. Before this, the only thing standing on it was
// TestGarden_LawnFiresTogetherBedsIndependent — a grouping test — which
// simply started failing when the default flipped, blocking every PR into
// main rather than reporting a garden regression.
func TestGarden_DisabledZoneStaysOutOfPlan(t *testing.T) {
	vm := gardenVM(t)

	if enabled := mustEval(t, vm, `ZONES[0].enabled || ZONES[1].enabled`).ToBoolean(); enabled {
		t.Fatalf("expected the shipped defaults to leave both lawn zones disabled; " +
			"if that changed deliberately, update this test and the two above it")
	}

	// Every zone far over its trigger: only the enabled/disabled flag can
	// decide who makes the plan.
	mustEval(t, vm, `storeStorageValue(deficitKey(0), 25);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(1), 25);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(2), 25);`)

	raw := mustEval(t, vm, `JSON.stringify(computeZonePlan())`).String()
	plan := zonePlanIDs(t, raw)

	if _, ok := plan[0]; ok {
		t.Errorf("disabled lawn zone 0 must stay out of the plan, got %v", plan)
	}
	if _, ok := plan[1]; ok {
		t.Errorf("disabled lawn zone 1 must stay out of the plan, got %v", plan)
	}
	if _, ok := plan[2]; !ok {
		t.Errorf("enabled beds zone 2 should still be planned, got %v", plan)
	}
}
