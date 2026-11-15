package ui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/devices"
	"github.com/asnowfix/home-automation/myhome/storage"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/go-logr/logr"
)

// TestStartupRenderFromDBLoadedCache pins the kiosk startup promise (#736, design
// docs/design/ui-startup-from-db.md item 4): right after a daemon start, before any
// device has answered, the Home and Rooms fragments show every known device and room
// from the DB-loaded cache, with switches unknown, and call no device.
//
// The cache is built the way the daemon builds it: a real DeviceStorage on SQLite
// ":memory:", wrapped by devices.NewCache, then Load. No impl is attached to any
// device, exactly as after a cold start.
func TestStartupRenderFromDBLoadedCache(t *testing.T) {
	calls := unreachableDevices(t)
	ctx := renderCtx()

	store, err := storage.NewDeviceStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	mk := func(id, name, mac string, cfg *shellyapi.Config, room string) {
		d := myhome.NewDevice(logr.Discard(), myhome.SHELLY, id).WithName(name)
		d.MAC = mac
		d.Config = cfg
		d.RoomId = room
		if _, err := store.SetDevice(ctx, d, true); err != nil {
			t.Fatal(err)
		}
	}
	swCfg := func() *shellyapi.Config { return &shellyapi.Config{Switch0: &sswitch.Config{Id: 0}} }
	mk("shellyplus1-aabbccddee11", "Lamp Hall", "AABBCCDDEE11", swCfg(), "")
	mk("shellyplus1-aabbccddee12", "Lamp Study", "AABBCCDDEE12", swCfg(), "study")
	mk("shellyplus1-aabbccddee13", "Never Configured", "AABBCCDDEE13", nil, "")
	mk("shellypro1-aabbccddee14", "Pool Pump", "AABBCCDDEE14", nil, "") // pool tracking on, see below

	cache := devices.NewCache(ctx, store)
	if err := cache.Load(ctx); err != nil {
		t.Fatal(err)
	}

	// temperature.list is memory/SQLite only in the daemon, so rooms may be read at
	// render; a fake stands in for the temperature service and counts its calls.
	var roomCalls atomic.Int32
	if prev, err := myhome.Methods(myhome.TemperatureList); err == nil {
		t.Cleanup(func() { myhome.RegisterMethodHandler(myhome.TemperatureList, prev.ActionE) })
	} else {
		t.Cleanup(func() { myhome.UnregisterMethodHandler(myhome.TemperatureList) })
	}
	myhome.RegisterMethodHandler(myhome.TemperatureList, func(context.Context, any) (any, error) {
		roomCalls.Add(1)
		return &myhome.TemperatureRoomList{
			"study":  {RoomID: "study", Name: "Study Room", Kinds: []myhome.RoomKind{}},
			"garage": {RoomID: "garage", Name: "Garage Room", Kinds: []myhome.RoomKind{}},
		}, nil
	})

	// Pool tracking on, nothing known yet: pool.snapshot has no values, and a
	// counting pool.getstatus (the live, device-reading verb) must never be reached.
	liveCalls := stubPoolVerbs(t, &myhome.PoolSnapshotResult{DeviceID: "shellypro1-aabbccddee14"})

	h := newMux(ctx, logr.Discard(), nil, cache, nil, nil, "", nil, nil)

	home := get(h, "/htmx/devices")
	if home.Code != 200 {
		t.Fatalf("/htmx/devices = %d", home.Code)
	}
	body := home.Body.String()
	for _, name := range []string{"Lamp Hall", "Lamp Study", "Never Configured"} {
		if !strings.Contains(body, name) {
			t.Errorf("/htmx/devices lacks device %q", name)
		}
	}
	if n := strings.Count(body, `class="mh-power is-unknown"`); n != 2 {
		t.Errorf("/htmx/devices has %d unknown power buttons, want 2 (one per configured switch)", n)
	}
	if strings.Contains(body, "mh-power is-on") {
		t.Error("a switch renders on before any state was seen")
	}

	if !strings.Contains(body, "Water supply --") || strings.Contains(body, "Water supply OK") {
		t.Error("pool water-supply pill must render -- (not OK) with no snapshot")
	}
	if !strings.Contains(body, "Turnover 24 h --") {
		t.Error("pool turnover pill must render -- with no snapshot")
	}
	if n := liveCalls.Load(); n != 0 {
		t.Errorf("rendering called pool.getstatus %d time(s), want 0 (#731)", n)
	}

	rooms := get(h, "/htmx/rooms")
	if rooms.Code != 200 {
		t.Fatalf("/htmx/rooms = %d", rooms.Code)
	}
	rb := rooms.Body.String()
	for _, want := range []string{"Study Room", "Garage Room", "Lamp Study"} {
		if !strings.Contains(rb, want) {
			t.Errorf("/htmx/rooms lacks %q", want)
		}
	}
	if roomCalls.Load() == 0 {
		t.Error("rooms were not read through temperature.list")
	}

	if n := calls.Load(); n != 0 {
		t.Errorf("startup render made %d device call(s)", n)
	}
}
