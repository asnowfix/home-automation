package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// unreachableDevices installs a registrar with every real method handler
// registered (shelly.Init), so a call to ANY method gets past Device.CallE's
// handler lookup and reaches the counting caller; each call is counted and then
// hangs for 3 s, like a device that does not answer (#732). It returns the counter.
// The registrar is reset when the test ends.
func unreachableDevices(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	shelly.Init(logr.Discard(), nil, time.Second, 0)
	r := shelly.GetRegistrar()
	r.RegisterDeviceCaller(types.ChannelDefault, func(ctx context.Context, _ types.Device, mh types.MethodHandler, _ any, _ any) (any, error) {
		calls.Add(1)
		t.Logf("device call attempted: %s", mh.Method)
		time.Sleep(3 * time.Second)
		return nil, context.DeadlineExceeded
	})
	t.Cleanup(func() { r.Init(logr.Discard()) })
	return &calls
}

func switchDevice(t *testing.T, id string, output bool) *myhome.Device {
	t.Helper()
	d := myhome.NewDevice(logr.Discard(), myhome.SHELLY, id).WithName(id)
	impl, err := shelly.NewDeviceFromSummary(context.Background(), logr.Discard(), d)
	if err != nil {
		t.Fatal(err)
	}
	sd := impl.(*shelly.Device)
	sd.SetStatus(&shellyapi.Status{Switch0: &sswitch.Status{Id: 0, Output: output}})
	d.WithImpl(sd)
	d.Config = &shellyapi.Config{Switch0: &sswitch.Config{Id: 0}}
	return d
}

func renderCtx() context.Context { return logr.NewContext(context.Background(), logr.Discard()) }

func TestRenderMakesNoDeviceCall(t *testing.T) {
	calls := unreachableDevices(t)

	// polarity resolved earlier by a refresh: normally-closed, so output=true means "off"
	fake := types.NewFakeDevice()
	fake.IdValue = "shellyplus1-aabbccddee01"
	fake.SetResult(string(kvs.Get), &kvs.GetResponse{Value: "true"})
	if _, err := kvs.GetValue(context.Background(), logr.Discard(), types.ChannelDefault, fake, "normally-closed"); err != nil {
		t.Fatal(err)
	}

	known := switchDevice(t, "shellyplus1-aabbccddee01", true) // cached polarity
	cold := switchDevice(t, "shellyplus1-aabbccddee02", true)  // never refreshed
	noCfg := switchDevice(t, "shellyplus1-aabbccddee03", true)
	noCfg.Config = nil

	start := time.Now()
	kv := DeviceToView(renderCtx(), known)
	cv := DeviceToView(renderCtx(), cold)
	nv := DeviceToView(renderCtx(), noCfg)
	elapsed := time.Since(start)

	if n := calls.Load(); n != 0 {
		t.Errorf("rendering made %d device call(s)", n)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("rendering three devices took %v with unreachable devices", elapsed)
	}
	t.Logf("rendered 3 devices with unreachable stubs in %v", elapsed)

	if sw, ok := kv.Switches[0]; !ok || sw.On || kv.SwitchUnknown[0] {
		t.Errorf("known normally-closed device: switch=%+v unknown=%v; want off and known", sw, kv.SwitchUnknown[0])
	}
	if _, ok := cv.Switches[0]; !ok || !cv.SwitchUnknown[0] {
		t.Errorf("cold cache: switches=%v unknown=%v; want the switch listed as unknown", cv.Switches, cv.SwitchUnknown)
	}
	if len(nv.Switches) != 0 {
		t.Errorf("device without config lists switches %v; want none", nv.Switches)
	}
}

func TestRenderEndpointsMakeNoDeviceCall(t *testing.T) {
	calls := unreachableDevices(t)
	devs := []*myhome.Device{switchDevice(t, "shellyplus1-aabbccddee04", true), switchDevice(t, "shellyplus1-aabbccddee05", false)}
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: devs}, nil, nil, "", nil, nil)

	start := time.Now()
	for _, p := range []string{"/htmx/devices", "/htmx/device/shellyplus1-aabbccddee04"} {
		if rec := get(h, p); rec.Code != 200 {
			t.Fatalf("%s = %d", p, rec.Code)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("render endpoints made %d device call(s)", n)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("render endpoints took %v with unreachable devices", d)
	} else {
		t.Logf("/htmx/devices + /htmx/device/{id} with unreachable devices: %v", d)
	}
}

// TestRoomsFragmentGroupsEveryDeviceOnce pins #744: the Rooms fragment lists each
// device exactly once, under its room, or under "Unassigned" when it has no
// room_id or names a room that does not exist, and makes no device call.
func TestRoomsFragmentGroupsEveryDeviceOnce(t *testing.T) {
	calls := unreachableDevices(t)
	// Counting fakes for the pool verbs: pool.getstatus reaches the pool device
	// (3 KVS reads + Input.GetStatus) and must never be called from a render;
	// pool.snapshot answers from daemon memory and is applied once on the flat
	// list, not once per room (#744 review).
	live := stubPoolVerbs(t, &myhome.PoolSnapshotResult{DeviceID: "shellyplus1-aabbccddee21"})
	var snapshots atomic.Int32
	myhome.RegisterMethodHandler(myhome.PoolSnapshot, func(context.Context, any) (any, error) {
		snapshots.Add(1)
		return &myhome.PoolSnapshotResult{DeviceID: "shellyplus1-aabbccddee21"}, nil
	})

	assigned := switchDevice(t, "shellyplus1-aabbccddee21", false)
	assigned.RoomId = "study"
	none := switchDevice(t, "shellyplus1-aabbccddee22", false)
	dangling := switchDevice(t, "shellyplus1-aabbccddee23", false)
	dangling.RoomId = "demolished"

	if prev, err := myhome.Methods(myhome.TemperatureList); err == nil {
		t.Cleanup(func() { myhome.RegisterMethodHandler(myhome.TemperatureList, prev.ActionE) })
	} else {
		t.Cleanup(func() { myhome.UnregisterMethodHandler(myhome.TemperatureList) })
	}
	myhome.RegisterMethodHandler(myhome.TemperatureList, func(context.Context, any) (any, error) {
		return &myhome.TemperatureRoomList{
			"study": {RoomID: "study", Name: "Study Room", Kinds: []myhome.RoomKind{}},
			"empty": {RoomID: "empty", Name: "Empty Room", Kinds: []myhome.RoomKind{}},
		}, nil
	})

	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: []*myhome.Device{assigned, none, dangling}}, nil, nil, "", nil, nil)
	rec := get(h, "/htmx/rooms")
	if rec.Code != 200 {
		t.Fatalf("/htmx/rooms = %d", rec.Code)
	}
	body := rec.Body.String()

	for _, id := range []string{assigned.Id(), none.Id(), dangling.Id()} {
		if n := strings.Count(body, `id="device-`+id+`"`); n != 1 {
			t.Errorf("device %s rendered %d times, want exactly 1", id, n)
		}
	}
	room := strings.Index(body, "Study Room")
	unassigned := strings.Index(body, "Unassigned")
	if room < 0 || unassigned < 0 || room > unassigned {
		t.Fatalf("want the Study Room card before the Unassigned group (room=%d unassigned=%d)", room, unassigned)
	}
	if !strings.Contains(body, "Empty Room") {
		t.Error("a room without devices must still be listed")
	}
	pos := func(id string) int { return strings.Index(body, `id="device-`+id+`"`) }
	if p := pos(assigned.Id()); p < room || p > unassigned {
		t.Errorf("assigned device at %d, want inside the room card (%d..%d)", p, room, unassigned)
	}
	for _, d := range []*myhome.Device{none, dangling} {
		if pos(d.Id()) < unassigned {
			t.Errorf("device %s must be under Unassigned", d.Id())
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("rooms render made %d device call(s)", n)
	}
	if n := live.Load(); n != 0 {
		t.Errorf("rooms render called pool.getstatus %d time(s), want 0", n)
	}
	if n := snapshots.Load(); n != 1 {
		t.Errorf("rooms render called pool.snapshot %d time(s), want exactly 1 (2 rooms must not multiply it)", n)
	}
	if !strings.Contains(body, `id="water-supply-`+assigned.Id()+`"`) {
		t.Errorf("pool pill missing for the pool device, which sits in a room:\n%s", body)
	}
}

// registerRooms installs a temperature.list answering rooms (nil unregisters it
// = temperature service disabled), restored when the test ends.
func registerRooms(t *testing.T, rooms myhome.TemperatureRoomList) {
	t.Helper()
	prev, err := myhome.Methods(myhome.TemperatureList)
	t.Cleanup(func() {
		if err == nil {
			myhome.RegisterMethodHandler(myhome.TemperatureList, prev.ActionE)
		} else {
			myhome.UnregisterMethodHandler(myhome.TemperatureList)
		}
	})
	if rooms == nil {
		myhome.UnregisterMethodHandler(myhome.TemperatureList)
		return
	}
	myhome.RegisterMethodHandler(myhome.TemperatureList, func(context.Context, any) (any, error) { return &rooms, nil })
}

// Temperature service disabled (#744 review): the "not enabled" note, every
// device once under Unassigned, no "No rooms configured" text.
func TestRoomsFragmentTemperatureDisabled(t *testing.T) {
	calls := unreachableDevices(t)
	registerRooms(t, nil)
	a := switchDevice(t, "shellyplus1-aabbccddee31", false)
	b := switchDevice(t, "shellyplus1-aabbccddee32", false)
	b.RoomId = "study" // names a room nobody can resolve
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: []*myhome.Device{a, b}}, nil, nil, "", nil, nil)
	rec := get(h, "/htmx/rooms")
	if rec.Code != 200 {
		t.Fatalf("/htmx/rooms = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, d := range []*myhome.Device{a, b} {
		if n := strings.Count(body, `id="device-`+d.Id()+`"`); n != 1 {
			t.Errorf("device %s rendered %d times, want 1", d.Id(), n)
		}
	}
	u := strings.Index(body, "data-unassigned")
	if u < 0 || strings.Index(body, `id="device-`+a.Id()+`"`) < u || strings.Index(body, `id="device-`+b.Id()+`"`) < u {
		t.Errorf("every device must sit under Unassigned:\n%s", body)
	}
	if !strings.Contains(body, "data-rooms-disabled") || !strings.Contains(body, "not enabled") {
		t.Error("the not-enabled note is missing")
	}
	if strings.Contains(body, "No rooms configured") {
		t.Error("must not suggest adding rooms when the service is disabled")
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("render made %d device call(s)", n)
	}
}

// Owner's main instance today: rooms exist but no device has one. Every device
// is in one long Unassigned group, each room is listed and says it has no
// devices, and nothing is rendered twice.
func TestRoomsFragmentAllUnassigned(t *testing.T) {
	calls := unreachableDevices(t)
	registerRooms(t, myhome.TemperatureRoomList{
		"kitchen": {RoomID: "kitchen", Name: "Kitchen", Kinds: []myhome.RoomKind{}},
		"garage":  {RoomID: "garage", Name: "Garage", Kinds: []myhome.RoomKind{}},
	})
	var devs []*myhome.Device
	for i := 0; i < 60; i++ {
		devs = append(devs, switchDevice(t, fmt.Sprintf("shellyplus1-aabbccdd%04x", i), false))
	}
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: devs}, nil, nil, "", nil, nil)
	rec := get(h, "/htmx/rooms")
	if rec.Code != 200 {
		t.Fatalf("/htmx/rooms = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, d := range devs {
		if n := strings.Count(body, `id="device-`+d.Id()+`"`); n != 1 {
			t.Errorf("device %s rendered %d times, want 1", d.Id(), n)
		}
	}
	if n := strings.Count(body, "No devices"); n != 2 {
		t.Errorf("%d empty-room notes, want 2", n)
	}
	if n := strings.Count(body, "data-unassigned"); n != 1 {
		t.Errorf("%d Unassigned groups, want 1", n)
	}
	if !strings.Contains(body, "Kitchen") || !strings.Contains(body, "Garage") {
		t.Error("empty rooms must still be listed")
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("render made %d device call(s)", n)
	}
}

// A failed device-list read is a 500 (htmx keeps the previous DOM), not an
// empty page that looks valid.
func TestRoomsFragmentDeviceListErrorIs500(t *testing.T) {
	registerRooms(t, myhome.TemperatureRoomList{"a": {RoomID: "a", Name: "A", Kinds: []myhome.RoomKind{}}})
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{err: errors.New("db closed")}, nil, nil, "", nil, nil)
	if rec := get(h, "/htmx/rooms"); rec.Code != 500 {
		t.Errorf("/htmx/rooms = %d, want 500", rec.Code)
	}
}
