package script

import (
	"reflect"
	"testing"
)

func TestDeviceState_KVSAccessors(t *testing.T) {
	d := &DeviceState{} // nil maps: setters must allocate lazily

	if _, ok := d.KVSValue("k"); ok {
		t.Error("KVSValue on an empty state must report absent")
	}
	d.SetKVSValue("k", "v")
	d.SetKVSValue("n", 3)

	if s, ok := d.KVSString("k"); !ok || s != "v" {
		t.Errorf("KVSString(k) = %q,%v", s, ok)
	}
	if _, ok := d.KVSString("n"); ok {
		t.Error("KVSString of a non-string must report !ok")
	}
	if _, ok := d.KVSString("missing"); ok {
		t.Error("KVSString of a missing key must report !ok")
	}

	snap := d.KVSSnapshot()
	snap["k"] = "mutated"
	if got, _ := d.KVSString("k"); got != "v" {
		t.Errorf("snapshot must be a copy; live value became %q", got)
	}
	if !reflect.DeepEqual(d.GetKVS(), map[string]interface{}{"k": "v", "n": 3}) {
		t.Errorf("GetKVS = %v", d.GetKVS())
	}

	d.DeleteKVSValue("k")
	if _, ok := d.KVSValue("k"); ok {
		t.Error("key must be gone after DeleteKVSValue")
	}
	d.DeleteKVSValue("never-existed") // must not panic
}

func TestDeviceState_StorageAccessors(t *testing.T) {
	d := &DeviceState{}
	d.DeleteStorageValue("x") // nil map delete must not panic
	if d.StorageLen() != 0 {
		t.Fatal("empty state must have no storage")
	}
	d.SetStorageValue("a", "1")
	d.SetStorageValue("b", 2)
	if v, ok := d.StorageValue("a"); !ok || v != "1" {
		t.Errorf("StorageValue(a) = %v,%v", v, ok)
	}
	if d.StorageLen() != 2 {
		t.Errorf("StorageLen = %d, want 2", d.StorageLen())
	}
	d.DeleteStorageValue("a")
	if _, ok := d.StorageValue("a"); ok || d.StorageLen() != 1 {
		t.Error("a must be deleted")
	}
}

func TestDeviceState_ComponentStatus(t *testing.T) {
	d := &DeviceState{}
	if _, ok := d.ComponentStatusValue("switch:0"); ok {
		t.Error("absent component must report !ok")
	}
	if d.SetComponentStatusField("switch:0", "output", true) {
		t.Error("SetComponentStatusField on an absent component must return false")
	}

	d.SetComponentStatusValue("switch:0", map[string]interface{}{"output": false})
	d.SetComponentStatusValue("sys", "not-a-map")

	if d.SetComponentStatusField("sys", "f", 1) {
		t.Error("SetComponentStatusField on a non-map status must return false")
	}
	if !d.SetComponentStatusField("switch:0", "output", true) {
		t.Fatal("SetComponentStatusField on a map status must return true")
	}

	v, ok := d.ComponentStatusValue("switch:0")
	if !ok {
		t.Fatal("switch:0 must exist")
	}
	m := v.(map[string]interface{})
	if m["output"] != true {
		t.Errorf("output = %v, want true", m["output"])
	}
	// The returned map is a copy: mutating it must not touch the live state.
	m["output"] = "tampered"
	again, _ := d.ComponentStatusValue("switch:0")
	if again.(map[string]interface{})["output"] != true {
		t.Error("ComponentStatusValue must return a copy")
	}
	if v, ok := d.ComponentStatusValue("sys"); !ok || v != "not-a-map" {
		t.Errorf("non-map status must pass through, got %v,%v", v, ok)
	}

	d.DeleteComponentStatusValue("switch:0")
	if _, ok := d.ComponentStatusValue("switch:0"); ok {
		t.Error("switch:0 must be deleted")
	}
}

func TestDeviceState_Schedules(t *testing.T) {
	d := &DeviceState{Schedules: []map[string]interface{}{
		{"id": 1, "enable": true, "timespec": "0 0 * * *"},
		{"id": float64(2), "enable": false}, // JSON-decoded ids are float64
	}}

	jobs := d.ScheduleJobs()
	if len(jobs) != 2 {
		t.Fatalf("ScheduleJobs = %d jobs, want 2", len(jobs))
	}
	jobs[0]["enable"] = "tampered"
	if d.Schedules[0]["enable"] != true {
		t.Error("ScheduleJobs must return copies of the job maps")
	}

	got, ok := d.UpdateSchedule(2, map[string]interface{}{"enable": true, "id": 99})
	if !ok {
		t.Fatal("UpdateSchedule(2) must find the job")
	}
	if got["enable"] != true {
		t.Errorf("updated job = %v", got)
	}
	if got["id"] == 99 {
		t.Error("UpdateSchedule must not let an update rewrite the id")
	}
	if _, ok := d.UpdateSchedule(42, nil); ok {
		t.Error("UpdateSchedule of an unknown id must return false")
	}
	if n := len((&DeviceState{}).ScheduleJobs()); n != 0 {
		t.Errorf("an empty state has %d schedule jobs, want 0", n)
	}
}

func TestDeviceState_PendingNestedEventIsSingleUse(t *testing.T) {
	d := &DeviceState{}
	if _, ok := d.TakePendingNestedEvent(); ok {
		t.Error("nothing pending on a fresh state")
	}
	d.SetPendingNestedEvent([]byte(`{"info":{"event":"x"}}`))
	ev, ok := d.TakePendingNestedEvent()
	if !ok || string(ev) != `{"info":{"event":"x"}}` {
		t.Errorf("Take = %q,%v", ev, ok)
	}
	if _, ok := d.TakePendingNestedEvent(); ok {
		t.Error("the pending event must be consumed exactly once")
	}
}
