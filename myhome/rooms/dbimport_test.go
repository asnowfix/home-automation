package rooms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	mhd "github.com/asnowfix/home-automation/myhome/devices"
	"github.com/asnowfix/home-automation/myhome/storage"
	pkgdevices "github.com/asnowfix/home-automation/pkg/devices"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// instance is a daemon in miniature: the real rooms service and the real
// device cache over in-memory SQLite, reached through a Client that calls the
// handlers directly (no global verb registry, no MQTT).
type instance struct {
	t     *testing.T
	rooms *Service
	devs  *mhd.Cache
	// unreachable lists device ids whose room-id KVS write fails.
	unreachable map[string]bool
	// offline lists verbs answered like a daemon that does not serve them.
	offline map[myhome.Verb]bool
}

func newInstance(t *testing.T) *instance {
	t.Helper()
	svc, _ := newTestService(t)
	ds, err := storage.NewDeviceStorage(testr.New(t), ":memory:")
	if err != nil {
		t.Fatalf("NewDeviceStorage: %v", err)
	}
	t.Cleanup(ds.Close)
	ctx := logr.NewContext(context.Background(), logr.Discard())
	return &instance{t: t, rooms: svc, devs: mhd.NewCache(ctx, ds),
		unreachable: map[string]bool{}, offline: map[myhome.Verb]bool{}}
}

func (i *instance) LookupDevices(context.Context, string) (*[]pkgdevices.Device, error) {
	return nil, nil
}
func (i *instance) ForgetDevices(context.Context, string) error { return nil }

func (i *instance) CallE(ctx context.Context, v myhome.Verb, p any) (any, error) {
	if i.offline[v] {
		return nil, fmt.Errorf("no handler (code:1): unknown or unregistered method %s", v)
	}
	switch v {
	case myhome.DeviceExport:
		ds, err := i.devs.ExportDevices(ctx, p.(string))
		return &ds, err
	case myhome.DeviceUpdate:
		_, err := i.devs.SetDevice(ctx, p.(*myhome.Device), true)
		return nil, err
	case myhome.DeviceSetRoom:
		sp := p.(*myhome.DeviceSetRoomParams)
		if _, err := i.devs.SetDeviceRoom(ctx, sp.Identifier, sp.RoomId); err != nil {
			return nil, err
		}
		if i.unreachable[sp.Identifier] {
			return &myhome.DeviceSetRoomResult{KVS: myhome.KVSUnreachable, KVSError: "timeout"}, nil
		}
		return &myhome.DeviceSetRoomResult{KVS: myhome.KVSSet}, nil
	case myhome.TemperatureList:
		return i.rooms.HandleList(ctx)
	case myhome.RoomCreate:
		return i.rooms.HandleRoomCreate(ctx, p.(*myhome.RoomCreateParams))
	case myhome.RoomEdit:
		return i.rooms.HandleRoomEdit(ctx, p.(*myhome.RoomEditParams))
	case myhome.TemperatureGetWeekdayDefaults:
		return i.rooms.HandleGetWeekdayDefaults(ctx, p.(*myhome.TemperatureGetWeekdayDefaultsParams))
	case myhome.TemperatureSetWeekdayDefault:
		return i.rooms.HandleSetWeekdayDefault(ctx, p.(*myhome.TemperatureSetWeekdayDefaultParams))
	case myhome.TemperatureGetKindSchedules:
		return i.rooms.HandleGetKindSchedules(ctx, p.(*myhome.TemperatureGetKindSchedulesParams))
	case myhome.TemperatureSetKindSchedule:
		return i.rooms.HandleSetKindSchedule(ctx, p.(*myhome.TemperatureSetKindScheduleParams))
	}
	return nil, fmt.Errorf("instance: unexpected verb %s", v)
}

// export mirrors what `ctl db export` collects.
func (i *instance) export() *myhome.DatabaseDump {
	i.t.Helper()
	ctx := context.Background()
	d := &myhome.DatabaseDump{}
	out, err := i.CallE(ctx, myhome.DeviceExport, "*")
	if err != nil {
		i.t.Fatal(err)
	}
	d.Devices = *out.(*[]*myhome.Device)
	out, _ = i.CallE(ctx, myhome.TemperatureList, nil)
	if l := out.(*myhome.TemperatureRoomList); len(*l) > 0 {
		d.TemperatureRooms = *l
	}
	out, _ = i.CallE(ctx, myhome.TemperatureGetWeekdayDefaults, &myhome.TemperatureGetWeekdayDefaultsParams{})
	d.WeekdayDefaults = out.(*myhome.TemperatureWeekdayDefaults).Defaults
	out, _ = i.CallE(ctx, myhome.TemperatureGetKindSchedules, &myhome.TemperatureGetKindSchedulesParams{})
	d.KindSchedules = *out.(*myhome.TemperatureKindScheduleList)
	return d
}

func js(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mkDevice(id, mac, name, room string) *myhome.Device {
	d := &myhome.Device{}
	d.Manufacturer_ = "Shelly"
	d.Id_ = id
	d.MAC = mac
	d.Name_ = name
	d.RoomId = room
	return d
}

func seed(t *testing.T, in *instance) {
	t.Helper()
	ctx := context.Background()
	for _, d := range []*myhome.Device{
		mkDevice("shellyplus1-aaa", "aa:bb:cc:dd:ee:01", "hall-light", "hall"),
		mkDevice("shellyblu-112233445566", "", "salon-temperature", "hall"),
		mkDevice("shellyplus1-bbb", "aa:bb:cc:dd:ee:02", "garage", ""),
	} {
		if _, err := in.devs.SetDevice(ctx, d, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := in.CallE(ctx, myhome.RoomCreate, &myhome.RoomCreateParams{ID: "hall", Name: "Hall"}); err != nil {
		t.Fatal(err)
	}
	url := "https://calendar.example.org/hall.ics"
	name := "Hall way"
	if _, err := in.CallE(ctx, myhome.RoomEdit, &myhome.RoomEditParams{
		ID: "hall", Name: &name, ICalURL: &url,
		Kinds:  []myhome.RoomKind{myhome.RoomKindOffice},
		Levels: map[string]float64{"eco": 17, "comfort": 21},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := in.CallE(ctx, myhome.TemperatureSetWeekdayDefault, &myhome.TemperatureSetWeekdayDefaultParams{Weekday: 3, DayType: myhome.DayTypeDayOff}); err != nil {
		t.Fatal(err)
	}
	if _, err := in.CallE(ctx, myhome.TemperatureSetKindSchedule, &myhome.TemperatureSetKindScheduleParams{
		Kind: myhome.RoomKindOffice, DayType: myhome.DayTypeWorkDay, Ranges: []string{"08:00-12:30"},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDatabaseImport_RoundTrip: export one instance, import into an empty one,
// export again; the two dumps must be identical for every section.
func TestDatabaseImport_RoundTrip(t *testing.T) {
	ctx := context.Background()
	src, dst := newInstance(t), newInstance(t)
	seed(t, src)
	want := src.export()
	if len(want.Devices) != 3 || len(want.TemperatureRooms) != 1 || len(want.WeekdayDefaults) != 1 || len(want.KindSchedules) != 1 {
		t.Fatalf("seed incomplete: %s", js(t, want))
	}
	if want.TemperatureRooms["hall"].ICalURL == "" {
		t.Fatal("seed lost ical_url")
	}
	// The dump file is what crosses the wire.
	dump, err := myhome.ParseDatabaseDump([]byte(js(t, want)))
	if err != nil {
		t.Fatal(err)
	}

	// Default: devices unknown to the target are not recreated.
	r, err := myhome.ImportDatabase(ctx, dst, dump, myhome.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Devices.Skipped != 3 || r.Devices.Created != 0 {
		t.Errorf("devices: want 3 skipped 0 created, got %+v", r.Devices)
	}
	if r.Rooms.Created != 1 || r.WeekdayDefaults.Created != 1 || r.KindSchedules.Created != 1 {
		t.Errorf("sections not created: %+v", r)
	}
	if got := dst.export().TemperatureRooms["hall"].ICalURL; got != want.TemperatureRooms["hall"].ICalURL {
		t.Errorf("ical_url not restored: %q", got)
	}
	if n := len(dst.export().Devices); n != 0 {
		t.Fatalf("devices were resurrected: %d", n)
	}

	// --create-missing: full restore. One device cannot be reached.
	dst.unreachable["shellyplus1-aaa"] = true
	r, err = myhome.ImportDatabase(ctx, dst, dump, myhome.ImportOptions{CreateMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Devices.Created != 3 || r.Rooms.Unchanged != 1 || r.Incomplete() {
		t.Errorf("unexpected report: %+v", r)
	}
	if r.RoomKVS.Set != 1 || len(r.RoomKVS.Unreachable) != 1 || !strings.Contains(r.RoomKVS.Unreachable[0], "shellyplus1-aaa") {
		t.Errorf("KVS report: %+v", r.RoomKVS)
	}
	if a, b := js(t, want), js(t, dst.export()); a != b {
		t.Errorf("round trip changed the dump:\nwant %s\ngot  %s", a, b)
	}

	// Idempotent: a second import changes nothing and writes no KVS.
	r, err = myhome.ImportDatabase(ctx, dst, dump, myhome.ImportOptions{CreateMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Devices.Unchanged != 3 || r.Devices.Created+r.Devices.Updated != 0 || r.Rooms.Unchanged != 1 ||
		r.WeekdayDefaults.Unchanged != 1 || r.KindSchedules.Unchanged != 1 || r.RoomKVS.Set != 0 {
		t.Errorf("second import not a no-op: %+v", r)
	}
}

// A device forgotten after the backup is not brought back; a known device
// only gets its room restored.
func TestDatabaseImport_ForgottenAndUpdated(t *testing.T) {
	ctx := context.Background()
	in := newInstance(t)
	seed(t, in)
	dump := in.export()
	if err := in.devs.ForgetDevice(ctx, "shellyplus1-bbb"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.devs.SetDeviceRoom(ctx, "shellyplus1-aaa", "kitchen"); err != nil {
		t.Fatal(err)
	}
	r, err := myhome.ImportDatabase(ctx, in, dump, myhome.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Devices.Skipped != 1 || r.Devices.Updated != 1 || r.Devices.Unchanged != 1 || r.RoomKVS.Set != 1 {
		t.Errorf("unexpected report: %+v", r)
	}
	for _, d := range in.export().Devices {
		if d.Id() == "shellyplus1-bbb" {
			t.Error("forgotten device was resurrected")
		}
		if d.Id() == "shellyplus1-aaa" && d.RoomId != "hall" {
			t.Errorf("room not restored: %q", d.RoomId)
		}
	}
}

// A daemon without a section reports it instead of failing or faking a count.
func TestDatabaseImport_SectionNotServed(t *testing.T) {
	ctx := context.Background()
	src, dst := newInstance(t), newInstance(t)
	seed(t, src)
	dst.offline[myhome.TemperatureGetWeekdayDefaults] = true
	r, err := myhome.ImportDatabase(ctx, dst, src.export(), myhome.ImportOptions{CreateMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.WeekdayDefaults.Unavailable == "" || !r.Incomplete() {
		t.Errorf("weekday defaults should be unavailable: %+v", r.WeekdayDefaults)
	}
	if r.Rooms.Created != 1 || r.Devices.Created != 3 || r.KindSchedules.Created != 1 {
		t.Errorf("other sections must still import: %+v", r)
	}
}
