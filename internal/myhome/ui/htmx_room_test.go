package ui

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

var roomTestNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)

// setVerb installs a method handler (nil removes it) and restores the previous
// state when the test ends. Tests using it must not call t.Parallel().
func setVerb(t *testing.T, verb myhome.Verb, fn func(context.Context, any) (any, error)) {
	t.Helper()
	prev, err := myhome.Methods(verb)
	t.Cleanup(func() {
		if err == nil {
			myhome.RegisterMethodHandler(verb, prev.ActionE)
		} else {
			myhome.UnregisterMethodHandler(verb)
		}
	})
	if fn == nil {
		myhome.UnregisterMethodHandler(verb)
		return
	}
	myhome.RegisterMethodHandler(verb, fn)
}

// roomEnv registers one room "sejour" with levels and the 07:00-22:00 comfort range.
func roomEnv(t *testing.T, occupied *bool) {
	t.Helper()
	old := roomNow
	roomNow = func() time.Time { return roomTestNow }
	t.Cleanup(func() { roomNow = old })
	registerRooms(t, myhome.TemperatureRoomList{"sejour": {RoomID: "sejour", Name: "Séjour"}})
	setVerb(t, myhome.TemperatureGetSchedule, func(context.Context, any) (any, error) {
		return &myhome.TemperatureScheduleResult{RoomID: "sejour",
			Levels:        map[string]float64{"eco": 17, "comfort": 20, "away": 15},
			ComfortRanges: []myhome.TemperatureTimeRange{{Start: 7 * 60, End: 22 * 60}}}, nil
	})
	if occupied == nil {
		setVerb(t, myhome.OccupancyGetStatus, nil)
	} else {
		setVerb(t, myhome.OccupancyGetStatus, func(context.Context, any) (any, error) {
			return &myhome.OccupancyStatusResult{Occupied: *occupied}, nil
		})
	}
	setVerb(t, myhome.VacuumList, nil)
}

// bluSensor is a BLU H&T in room, reporting temp (nil = never) at updated (nil = unknown).
func bluSensor(id, room string, caps []string, temp *float64, updated *time.Time) *myhome.Device {
	d := myhome.NewDevice(logr.Discard(), myhome.SHELLY, id).WithName(id)
	d.RoomId = room
	d.Info = &shellyapi.DeviceInfo{BTHome: &shellyapi.BTHomeInfo{Capabilities: caps}}
	d.Status = &myhome.Status{Sensors: &myhome.Sensors{Temperature: temp, Humidity: fp(50)}, TemperatureUpdatedAt: updated}
	return d
}

func fp(f float64) *float64 { return &f }

func noHumidity(d *myhome.Device) *myhome.Device { d.Status.Sensors.Humidity = nil; return d }
func noBTHome(d *myhome.Device) *myhome.Device   { d.Info.BTHome = nil; return d }

func roomBody(t *testing.T, devs []*myhome.Device, path string) (int, string) {
	t.Helper()
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: devs}, nil, nil, "", nil, nil)
	rec := get(h, path)
	return rec.Code, rec.Body.String()
}

func TestRoomPageTemperature(t *testing.T) {
	roomEnv(t, nil)
	fresh, old := roomTestNow.Add(-5*time.Minute), roomTestNow.Add(-2*time.Hour)
	caps := []string{"temperature", "humidity", "battery"}
	cases := []struct {
		name string
		devs []*myhome.Device
		want []string
		deny []string
	}{
		{"no sensor", []*myhome.Device{bluSensor("shellyblu-a1", "", caps, fp(9), &fresh)},
			[]string{`id="room-temp">--<`}, []string{"ago", "°C</span>"}},
		{"sensor without value", []*myhome.Device{bluSensor("shellybluht3-01", "sejour", caps, nil, nil)},
			[]string{`id="room-temp">--<`}, []string{"ago"}},
		{"one fresh", []*myhome.Device{bluSensor("shellybluht3-01", "sejour", caps, fp(21.44), &fresh)},
			[]string{`id="room-temp">21.4 °C<`}, []string{"ago"}},
		{"one 2 h old", []*myhome.Device{bluSensor("shellybluht3-01", "sejour", caps, fp(21.44), &old)},
			[]string{`21.4 °C<`, "· 2 h ago"}, nil},
		{"value without update time", []*myhome.Device{bluSensor("shellybluht3-01", "sejour", caps, fp(21.4), nil)},
			[]string{`21.4 °C<`, "· age --"}, nil},
		{"BLU only, capabilities list battery only", []*myhome.Device{bluSensor("shellybluht3-01", "sejour", []string{"battery"}, fp(18.2), &fresh)},
			[]string{`18.2 °C<`}, nil},
		// #772: the generic twin carries no room and an older reading; it must not count.
		{"BLU with a generic twin without room", []*myhome.Device{
			bluSensor("shellybluht3-01", "sejour", caps, fp(21.4), &fresh),
			bluSensor("shellyblu-aabbccddeeff", "", caps, fp(3.0), &old)},
			[]string{`21.4 °C<`}, []string{"Configuration error", "ago", "3.0"}},
		// BLU TRV (no humidity) and Gen1 DW2 (no BTHome) report a temperature but are not room thermometers.
		{"BLU H&T with a TRV and a DW2 in the room", []*myhome.Device{
			bluSensor("shellybluht3-01", "sejour", caps, fp(21.4), &fresh),
			noHumidity(bluSensor("shellyblutrv-03", "sejour", []string{"battery"}, fp(23), &fresh)),
			noBTHome(bluSensor("shellydw2-04", "sejour", nil, fp(5), &fresh))},
			[]string{`21.4 °C<`}, []string{"Configuration error"}},
		{"two sensors", []*myhome.Device{
			bluSensor("shellybluht3-01", "sejour", caps, fp(21.4), &fresh),
			bluSensor("shellyht-02", "sejour", caps, fp(19.0), &fresh)},
			[]string{"Configuration error: 2 temperature sensors in this room; keep one"}, []string{`id="room-temp"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := roomBody(t, c.devs, "/htmx/room/sejour")
			if code != 200 {
				t.Fatalf("status %d: %s", code, body)
			}
			for _, w := range c.want {
				if !strings.Contains(body, w) {
					t.Errorf("missing %q in:\n%s", w, body)
				}
			}
			for _, w := range c.deny {
				if strings.Contains(body, w) {
					t.Errorf("unexpected %q in:\n%s", w, body)
				}
			}
		})
	}
}

func TestRoomPageTarget(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		occ  *bool
		want string
	}{
		{&yes, "Target 20.0 °C · comfort until 22:00"},
		{&no, "Target 15.0 °C · away (nobody home)"},
		{nil, "Target 15.0 °C · away (no presence data)"},
	} {
		roomEnv(t, c.occ)
		_, body := roomBody(t, nil, "/htmx/room/sejour")
		if !strings.Contains(body, c.want) {
			t.Errorf("want %q in:\n%s", c.want, body)
		}
	}
}

// Heater rows show state with no toggle and the caption; rendering the page
// makes no device call even though the devices are unreachable (the polarity
// cache is pre-filled for one heater: a cold cache would read the KVS, P6).
func TestRoomPageHeaterRowsAreReadOnlyAndMakeNoDeviceCall(t *testing.T) {
	calls := unreachableDevices(t)
	roomEnv(t, nil)

	fake := types.NewFakeDevice()
	fake.IdValue = "shellyplus1-aabbccddee61"
	fake.SetResult(string(kvs.Get), &kvs.GetResponse{Value: "true"})
	if _, err := kvs.GetValue(context.Background(), logr.Discard(), types.ChannelDefault, fake, "normally-closed"); err != nil {
		t.Fatal(err)
	}
	heater := func(id string) *myhome.Device {
		d := switchDevice(t, id, true)
		d.RoomId = "sejour"
		d.Config.Script1 = &shellyapi.ScriptInfo{Name: "heater.js"}
		return d
	}
	devs := []*myhome.Device{heater("shellyplus1-aabbccddee61"), heater("shellyplus1-aabbccddee62")}

	code, body := roomBody(t, devs, "/htmx/room/sejour")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if strings.Contains(body, "mh-power") || strings.Contains(body, "/htmx/switch/toggle") {
		t.Errorf("heater row has a toggle:\n%s", body)
	}
	if strings.Count(body, "set by heater script") != 2 {
		t.Errorf("want the caption on both heater rows:\n%s", body)
	}
	// normally-closed with output=true is off: Idle; the cold-cache heater is unknown: --
	for _, w := range []string{"shellyplus1-aabbccddee61: <strong>Idle<", "shellyplus1-aabbccddee62: <strong>--<"} {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q in:\n%s", w, body)
		}
	}
	if !strings.Contains(body, `$el.dataset.roomId`) {
		t.Errorf("Edit button must read the room id from a data attribute:\n%s", body)
	}
	if !strings.Contains(body, `data-devices="shellyplus1-aabbccddee61 shellyplus1-aabbccddee62"`) {
		t.Errorf("missing data-devices for the SSE refresh:\n%s", body)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("rendering made %d device call(s)", n)
	}
}

func TestRoomPageUnknownRoomIsNotFoundAndNotProxied(t *testing.T) {
	roomEnv(t, nil)
	for _, p := range []string{"/htmx/room/x", "/htmx/room/x/vacuum"} {
		code, body := roomBody(t, nil, p)
		if code != http.StatusNotFound || !strings.Contains(body, "data-room-not-found") || !strings.Contains(body, "Room not found") {
			t.Errorf("%s = %d %q", p, code, body)
		}
	}
	setVerb(t, myhome.TemperatureList, func(context.Context, any) (any, error) { return nil, errors.New("boom") })
	if code, body := roomBody(t, nil, "/htmx/room/sejour"); code != http.StatusInternalServerError || !strings.Contains(body, "data-room-load-error") || strings.Contains(body, "not found") {
		t.Errorf("temperature.list failing = %d %q, want a distinct load error", code, body)
	}
	registerRooms(t, nil)
	if code, body := roomBody(t, nil, "/htmx/room/sejour"); code != http.StatusNotFound || !strings.Contains(body, "not enabled") {
		t.Errorf("rooms disabled = %d %q", code, body)
	}
}

func TestRoomPageVacuumAreas(t *testing.T) {
	roomEnv(t, nil)
	robots := &myhome.VacuumListResult{Robots: []myhome.VacuumRobot{{ID: "r1", Name: "Robot"}}}
	setVerb(t, myhome.VacuumList, func(context.Context, any) (any, error) { return robots, nil })
	regions := []myhome.VacuumRegion{
		{ID: "1", Name: "Salon", RoomID: "sejour"},
		{ID: "2", Name: "Old map", RoomID: "sejour", Stale: true},
		{ID: "3", Name: "Cuisine", RoomID: "cuisine"},
	}
	setVerb(t, myhome.VacuumRooms, func(context.Context, any) (any, error) {
		return &myhome.VacuumRoomsResult{Regions: regions}, nil
	})
	_, body := roomBody(t, nil, "/htmx/room/sejour")
	if !strings.Contains(body, "Salon") || !strings.Contains(body, "Old map (robot map changed)") || strings.Contains(body, "Cuisine") {
		t.Errorf("want the mapped areas, the stale one labelled, none from another room:\n%s", body)
	}
	if !strings.Contains(body, "every 5s [kiosk.visible('rooms') && kiosk.roomOpen") {
		t.Errorf("vacuum block does not re-poll only while visible:\n%s", body)
	}
	if !strings.Contains(body, "Clean this room") || !strings.Contains(body, "Robot offline") {
		t.Errorf("want the disabled clean button with its reason (robot not connected):\n%s", body)
	}

	regions = nil
	_, body = roomBody(t, nil, "/htmx/room/sejour")
	if !strings.Contains(body, "No robot area assigned") || !strings.Contains(body, "myhome ctl vacuum rooms assign") {
		t.Errorf("want the CLI hint:\n%s", body)
	}
	_, frag := roomBody(t, nil, "/htmx/room/sejour/vacuum")
	if !strings.Contains(frag, "No robot area assigned") || strings.Contains(frag, "mh-room-head") {
		t.Errorf("vacuum fragment should be the block alone:\n%s", frag)
	}

	setVerb(t, myhome.VacuumList, nil) // no robot: block omitted
	_, body = roomBody(t, nil, "/htmx/room/sejour")
	if strings.Contains(body, "Vacuum</h3>") {
		t.Errorf("vacuum block shown without a robot:\n%s", body)
	}
}

func TestRoomsListHeadingOpensRoom(t *testing.T) {
	registerRooms(t, myhome.TemperatureRoomList{"sejour": {RoomID: "sejour", Name: "Séjour"}})
	_, body := roomBody(t, nil, "/htmx/rooms")
	if !strings.Contains(body, `data-room-id="sejour"`) || !strings.Contains(body, "showRoom(") {
		t.Errorf("room heading does not open the room page:\n%s", body)
	}
}

func TestTempAgeText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		29 * time.Minute: "", 45 * time.Minute: "· 45 min ago", 2*time.Hour + 5*time.Minute: "· 2 h ago", 72 * time.Hour: "· 3 d ago",
	} {
		if got := tempAgeText(d); got != want {
			t.Errorf("tempAgeText(%v) = %q, want %q", d, got, want)
		}
	}
}
