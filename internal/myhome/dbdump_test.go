package myhome

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/pkg/devices"
)

// recClient records the RPCs and answers from canned results.
type recClient struct {
	found   *[]devices.Device
	results map[Verb]any
	calls   []Verb
	params  map[Verb][]any
}

func (c *recClient) LookupDevices(context.Context, string) (*[]devices.Device, error) {
	return c.found, nil
}
func (c *recClient) ForgetDevices(context.Context, string) error { return nil }
func (c *recClient) CallE(_ context.Context, v Verb, in any) (any, error) {
	c.calls = append(c.calls, v)
	if c.params == nil {
		c.params = map[Verb][]any{}
	}
	c.params[v] = append(c.params[v], in)
	if r, ok := c.results[v]; ok {
		if err, isErr := r.(error); isErr {
			return nil, err
		}
		return r, nil
	}
	return nil, nil
}

func sampleVacuum() *VacuumDump {
	return &VacuumDump{
		Robots:  []VacuumRow{{"blid": "B1", "name": "Roomy", "host": "10.0.0.5"}},
		Regions: []VacuumRow{{"blid": "B1", "pmap_id": "p", "region_id": "7", "name": "Kitchen", "room_id": "kitchen"}},
	}
}

func TestExportIncludesVacuumAndSurvivesJSON(t *testing.T) {
	list := TemperatureRoomList{"kitchen": {RoomID: "kitchen", Name: "Kitchen", ICalURL: "https://example.invalid/c.ics"}}
	c := &recClient{results: map[Verb]any{TemperatureList: &list, VacuumExport: sampleVacuum()}}
	exp, err := ExportDatabase(context.Background(), c, "*", false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(exp)
	in, err := ParseDatabaseImport(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.Vacuum, sampleVacuum()) {
		t.Errorf("vacuum section changed in the file: %+v", in.Vacuum)
	}
	if in.TemperatureRooms["kitchen"].ICalURL == "" {
		t.Error("ical_url lost in export")
	}
	// devices-only skips everything else
	c = &recClient{}
	if _, err := ExportDatabase(context.Background(), c, "*", true); err != nil || len(c.calls) != 0 {
		t.Errorf("devices-only made calls %v (%v)", c.calls, err)
	}
	// a daemon without the vacuum service just leaves the section out
	c = &recClient{results: map[Verb]any{VacuumExport: errors.New("unknown method")}}
	exp, err = ExportDatabase(context.Background(), c, "*", false)
	if err != nil || exp.Vacuum != nil || len(exp.Skipped) == 0 || exp.Skipped[len(exp.Skipped)-1].Section != "vacuum" {
		t.Errorf("missing vacuum service: %+v, %v", exp.Vacuum, err)
	}
}

func TestImportSendsEverySectionWithICalAndVacuum(t *testing.T) {
	file := `{
	  "devices": [{"manufacturer":"Shelly","id":"shellyplus1-abc","name":"Lamp","room_id":"kitchen"}],
	  "rooms": {"kitchen": {"room_id":"kitchen","name":"Kitchen","kinds":["kitchen"],"levels":{"eco":17},"ical_url":"https://example.invalid/c.ics"}},
	  "weekday_defaults": {"6":"day-off"},
	  "kind_schedules": [{"kind":"kitchen","day_type":"work-day","ranges":[{"start":390,"end":480}]}],
	  "vacuum": {"robots":[{"blid":"B1"}],"regions":[{"blid":"B1","pmap_id":"p","region_id":"7","room_id":"kitchen"}]}
	}`
	in, err := ParseDatabaseImport([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	c := &recClient{results: map[Verb]any{VacuumImport: &VacuumImportResult{Robots: 1, Regions: 1}}}
	n, err := ImportDatabase(context.Background(), c, in, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := ImportCounts{Devices: 1, Rooms: 1, WeekdayDefaults: 1, KindSchedules: 1, VacuumRobots: 1, VacuumRegions: 1}
	if n != want {
		t.Errorf("counts = %+v, want %+v", n, want)
	}
	dev := c.params[DeviceUpdate][0].(*Device)
	if dev.RoomId != "kitchen" {
		t.Errorf("device.update carries room_id %q, want kitchen", dev.RoomId)
	}
	set := c.params[TemperatureSet][0].(*TemperatureSetParams)
	if set.ICalURL == nil || *set.ICalURL != "https://example.invalid/c.ics" {
		t.Errorf("temperature.set ical_url = %v", set.ICalURL)
	}
	if v := c.params[VacuumImport][0].(*VacuumDump); v.Regions[0]["room_id"] != "kitchen" {
		t.Errorf("vacuum.import regions = %+v", v.Regions)
	}
	if sched := c.params[TemperatureSetKindSchedule][0].(*TemperatureSetKindScheduleParams); sched.Ranges[0] != "06:30-08:00" {
		t.Errorf("kind schedule ranges = %v", sched.Ranges)
	}
}

// Files written before vacuum (and before any new section) still import, and the
// legacy bare device array still parses; none of them calls vacuum.import.
func TestOldFormatsStillImport(t *testing.T) {
	old := `{"devices":[{"manufacturer":"Shelly","id":"a","name":"A"}],
	         "rooms":{"r":{"room_id":"r","name":"R","kinds":["other"],"levels":{"eco":17}}},
	         "weekday_defaults":{"0":"day-off"}}`
	legacy := `[{"manufacturer":"Shelly","id":"a","name":"A"}]`
	for name, file := range map[string]string{"unified, no vacuum": old, "legacy array": legacy} {
		in, err := ParseDatabaseImport([]byte(file))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		c := &recClient{}
		n, err := ImportDatabase(context.Background(), c, in, false, io.Discard)
		if err != nil || n.Devices != 1 {
			t.Errorf("%s: %+v, %v", name, n, err)
		}
		for _, v := range c.calls {
			if v == VacuumImport {
				t.Errorf("%s: vacuum.import called for a file without a vacuum section", name)
			}
		}
	}
	if _, err := ParseDatabaseImport([]byte("not json")); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("garbage accepted: %v", err)
	}
	// a failing vacuum.import is reported (progress and error), never "complete"
	in, _ := ParseDatabaseImport([]byte(`{"vacuum":{"robots":[{"blid":"B"}]}}`))
	c := &recClient{results: map[Verb]any{VacuumImport: errors.New("boom")}}
	var out strings.Builder
	if _, err := ImportDatabase(context.Background(), c, in, false, &out); err == nil || !strings.Contains(out.String(), "boom") {
		t.Errorf("vacuum failure: %v / %q", err, out.String())
	}
}

// One temperature sensor per room (#752): a file with a two-sensor room is
// rejected before any write; a clean file imports.
func TestImportRejectsRoomWithSeveralSensors(t *testing.T) {
	parse := func(devs string) *DatabaseImport {
		in, err := ParseDatabaseImport([]byte(`{"devices":[` + devs + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	dev := func(id, room string) string {
		return `{"manufacturer":"Shelly","id":"` + id + `","name":"` + id + `","room_id":"` + room + `"}`
	}

	c := &recClient{}
	var out strings.Builder
	_, err := ImportDatabase(context.Background(), c,
		parse(dev("shellyht-a", "salon")+","+dev("shellyht-b", "salon")+","+dev("shellyplus1-x", "salon")), false, &out)
	if err == nil || len(c.calls) != 0 {
		t.Fatalf("err = %v, calls = %v (want an error and no write)", err, c.calls)
	}
	if !strings.Contains(out.String(), `room "salon" has 2 temperature sensors (shellyht-a, shellyht-b)`) {
		t.Errorf("progress = %q", out.String())
	}

	c = &recClient{}
	if _, err := ImportDatabase(context.Background(), c,
		parse(dev("shellyht-a", "salon")+","+dev("shellyht-b", "cuisine")), false, &out); err != nil {
		t.Fatalf("clean file rejected: %v", err)
	}
	if len(c.calls) != 2 {
		t.Errorf("calls = %v", c.calls)
	}
}
