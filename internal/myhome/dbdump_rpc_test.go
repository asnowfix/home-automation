package myhome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/pkg/devices"
)

// wireClient behaves like the real client as far as decoding is concerned: the
// daemon's replies are marshalled to JSON and decoded into the result type
// registered in the signatures map (DeviceSummary for the lookup verbs, Device
// for device.show), which is what drops room_id on the device list path.
type wireClient struct {
	daemon  []*Device
	results map[Verb]any // canned replies for other verbs; an error value fails the call
	failing map[string]error
}

func decodeLike(v Verb, reply any) (any, error) {
	b, err := json.Marshal(reply)
	if err != nil {
		return nil, err
	}
	out := signatures[v].NewResult()
	if err := json.Unmarshal(b, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *wireClient) LookupDevices(_ context.Context, _ string) (*[]devices.Device, error) {
	out, err := decodeLike(DevicesMatch, c.daemon)
	if err != nil {
		return nil, err
	}
	res := make([]devices.Device, 0)
	for _, d := range *out.(*[]DeviceSummary) {
		res = append(res, d)
	}
	return &res, nil
}
func (c *wireClient) ForgetDevices(context.Context, string) error { return nil }
func (c *wireClient) CallE(_ context.Context, v Verb, in any) (any, error) {
	if v == DeviceShow {
		id := in.(*DeviceShowParams).Identifier
		if err := c.failing[id]; err != nil {
			return nil, err
		}
		for _, d := range c.daemon {
			if d.Id() == id {
				return decodeLike(v, d)
			}
		}
		return nil, fmt.Errorf("device not found: %s", id)
	}
	if r, ok := c.results[v]; ok {
		if err, isErr := r.(error); isErr {
			return nil, err
		}
		return r, nil
	}
	return nil, errors.New("unknown or unregistered method " + string(v))
}

func kitchenLamp() *Device {
	return &Device{
		DeviceSummary: DeviceSummary{
			DeviceIdentifier: DeviceIdentifier{Manufacturer_: "Shelly", Id_: "shellyplus1-abc"},
			MAC:              "AA:BB:CC:DD:EE:01", Host_: "10.0.0.7", Name_: "Lamp",
		},
		ConfigRevision: 3,
		RoomId:         "kitchen",
	}
}

// Blocker 1 of the PR review: room_id used to be lost because the export took
// its devices from LookupDevices, whose reply decodes to DeviceSummary. This
// goes through the decoding path; it fails without the device.show fetch.
func TestExportKeepsRoomAssignmentThroughRPCDecoding(t *testing.T) {
	c := &wireClient{daemon: []*Device{kitchenLamp()}}
	exp, err := ExportDatabase(context.Background(), c, "*", true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(exp)
	if err != nil {
		t.Fatal(err)
	}
	in, err := ParseDatabaseImport(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Devices) != 1 || in.Devices[0].RoomId != "kitchen" || in.Devices[0].ConfigRevision != 3 {
		t.Fatalf("device after export/parse = %+v, want room_id kitchen and config_revision 3", in.Devices)
	}
	if len(exp.Skipped) != 0 {
		t.Errorf("unexpected skipped: %v", exp.Skipped)
	}
}

// If device.show fails for one device, it is still exported (summary only) and
// the lost detail is reported, not silent.
func TestExportReportsDeviceWithoutFullRecord(t *testing.T) {
	c := &wireClient{daemon: []*Device{kitchenLamp()}, failing: map[string]error{"shellyplus1-abc": errors.New("timeout")}}
	exp, err := ExportDatabase(context.Background(), c, "*", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(exp.Devices) != 1 || exp.Devices[0].Id() != "shellyplus1-abc" || exp.Devices[0].MAC != "aa:bb:cc:dd:ee:01" {
		t.Errorf("fallback device = %+v", exp.Devices)
	}
	if len(exp.Skipped) != 1 || !strings.Contains(exp.Skipped[0].String(), "shellyplus1-abc") || !strings.Contains(exp.Skipped[0].String(), "timeout") {
		t.Errorf("skipped = %v", exp.Skipped)
	}
}

// Blocker 2: a section the daemon cannot serve is reported as skipped with the
// reason, which is distinct from a section that answered with nothing.
func TestExportDistinguishesUnavailableFromEmpty(t *testing.T) {
	empty := TemperatureRoomList{}
	c := &wireClient{results: map[Verb]any{
		TemperatureList:               &empty, // answered, empty
		TemperatureGetWeekdayDefaults: &TemperatureWeekdayDefaults{},
		TemperatureGetKindSchedules:   &TemperatureKindScheduleList{},
		// vacuum.export: no reply registered -> "unknown or unregistered method"
	}}
	exp, err := ExportDatabase(context.Background(), c, "*", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(exp.Skipped) != 1 || exp.Skipped[0].Section != "vacuum" || !strings.Contains(exp.Skipped[0].String(), "unknown or unregistered method") {
		t.Fatalf("skipped = %v, want exactly vacuum", exp.Skipped)
	}

	// Every temperature section unavailable (e.g. service disabled): each is named.
	c = &wireClient{results: map[Verb]any{VacuumExport: &VacuumDump{}}}
	exp, _ = ExportDatabase(context.Background(), c, "*", false)
	var names []string
	for _, s := range exp.Skipped {
		names = append(names, s.Section)
	}
	if got := strings.Join(names, ","); got != "rooms,weekday defaults,kind schedules" {
		t.Errorf("skipped sections = %q", got)
	}
}

// Blocker 3: a partial import returns an error and counts the failures, while
// the items that can be stored still are.
func TestImportReportsPartialFailure(t *testing.T) {
	in, err := ParseDatabaseImport([]byte(`{
	  "devices":[{"manufacturer":"Shelly","id":"a","name":"A"}],
	  "rooms":{"r":{"room_id":"r","name":"R","kinds":["other"],"levels":{"eco":17}}},
	  "vacuum":{"robots":[{"blid":"B"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	c := &recClient{results: map[Verb]any{
		TemperatureSet: errors.New("unknown or unregistered method temperature.set"),
		VacuumImport:   errors.New("unknown or unregistered method vacuum.import"),
	}}
	var out strings.Builder
	n, err := ImportDatabase(context.Background(), c, in, false, &out)
	if err == nil {
		t.Fatal("partial import returned nil")
	}
	if n.Failed != 2 || n.Devices != 1 || n.Rooms != 0 {
		t.Errorf("counts = %+v", n)
	}
	if !strings.Contains(out.String(), "temperature.set") || !strings.Contains(out.String(), "vacuum.import") {
		t.Errorf("progress lacks the failures: %q", out.String())
	}
}

// A cancelled context stops the import without a warning per remaining item.
func TestImportCancelledPrintsNoWarningPerItem(t *testing.T) {
	in := &DatabaseImport{TemperatureRooms: map[string]*TemperatureRoomConfig{}}
	for i := 0; i < 5; i++ {
		in.TemperatureRooms[fmt.Sprintf("r%d", i)] = &TemperatureRoomConfig{Name: "R"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &recClient{results: map[Verb]any{TemperatureSet: errors.New("canceled")}}
	var out strings.Builder
	if _, err := ImportDatabase(ctx, c, in, false, &out); !errors.Is(err, context.Canceled) || strings.Contains(out.String(), "⚠") {
		t.Errorf("err = %v, output %q", err, out.String())
	}

}
