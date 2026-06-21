package scripts

import (
	"encoding/json"
	"fmt"
	"os"
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
// behaviour added for differentiated cadence: lawn zones (0 and 2, group
// "lawn") water together as soon as either crosses its trigger, while massifs
// (zone 1, group "beds") is gated independently by its own trigger.
func TestGarden_LawnFiresTogetherBedsIndependent(t *testing.T) {
	vm := gardenVM(t)

	// Zone 0 above its 12mm trigger, zone 2 below it (but in the same "lawn"
	// group as zone 0), zone 1 (massifs) below its 8mm trigger.
	mustEval(t, vm, `storeStorageValue(deficitKey(0), 15);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(1), 3);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(2), 9);`)

	raw := mustEval(t, vm, `JSON.stringify(computeZonePlan())`).String()
	plan := zonePlanIDs(t, raw)

	if _, ok := plan[0]; !ok {
		t.Errorf("expected zone 0 (over trigger) in plan, got %v", plan)
	}
	if _, ok := plan[2]; !ok {
		t.Errorf("expected lawn zone 2 to fire together with zone 0, got %v", plan)
	}
	if _, ok := plan[1]; ok {
		t.Errorf("expected massifs (zone 1, below its trigger) to stay excluded, got %v", plan)
	}
}

// TestGarden_GroupCadenceGate verifies that a group already watered within
// its intervalDays window is excluded regardless of deficit, and becomes
// eligible again once enough days have passed. It reads bedsInterval from the
// live ZONES config rather than hardcoding it, so the test tracks whatever
// the script's current default is instead of silently going stale.
func TestGarden_GroupCadenceGate(t *testing.T) {
	vm := gardenVM(t)

	// All zones comfortably over trigger so deficit never gates the result —
	// only the group-cadence check should determine inclusion/exclusion.
	mustEval(t, vm, `storeStorageValue(deficitKey(0), 25);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(1), 25);`)
	mustEval(t, vm, `storeStorageValue(deficitKey(2), 25);`)

	bedsInterval := mustEval(t, vm, `ZONES[1].intervalDays`).ToInteger()
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
	if _, ok := plan[2]; ok {
		t.Errorf("lawn zone 2 should still be gated (watered today), got %v", plan)
	}
	if _, ok := plan[1]; !ok {
		t.Errorf("expected massifs (zone 1) to be due again, got %v", plan)
	}
}
