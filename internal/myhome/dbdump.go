package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/asnowfix/home-automation/pkg/devices"
)

// The unified `myhome ctl db export` / `db import` document. Every section is
// optional, so a file written before a section existed still imports, and a
// section an importer does not know is ignored by encoding/json.
//
// Included: devices (with room_id), rooms (with ical_url), weekday defaults,
// kind schedules, vacuum robots and regions. Deliberately excluded, see the
// PR that introduced vacuum/ical here: caches that rebuild themselves
// (room_agenda_cache, weather_cache), events.db (its own file and retention),
// and fetch_subscriptions (devices re-register them at boot and their headers
// can carry credentials).

// DatabaseExport is the export side. Devices are the full records (room
// assignment, info, config), not the DeviceSummary the lookup RPCs decode to.
type DatabaseExport struct {
	Devices          []*Device                         `json:"devices,omitempty"`
	TemperatureRooms map[string]*TemperatureRoomConfig `json:"rooms,omitempty"`
	WeekdayDefaults  map[int]DayType                   `json:"weekday_defaults,omitempty"`
	KindSchedules    []TemperatureKindSchedule         `json:"kind_schedules,omitempty"`
	Vacuum           *VacuumDump                       `json:"vacuum,omitempty"`

	// Skipped lists what could not be collected, with the reason. It is not
	// part of the file: it tells the caller which empty-looking sections are
	// really "unavailable", as opposed to genuinely empty.
	Skipped []SkippedSection `json:"-"`
}

// SkippedSection is a part of the export the source daemon could not serve.
type SkippedSection struct {
	Section string // e.g. "vacuum", "rooms", "device <id>"
	Err     error
}

func (e SkippedSection) String() string {
	return fmt.Sprintf("%s: not exported (%v)", e.Section, e.Err)
}

// DatabaseImport is the import side.
type DatabaseImport struct {
	Devices          []Device                          `json:"devices,omitempty"`
	TemperatureRooms map[string]*TemperatureRoomConfig `json:"rooms,omitempty"`
	WeekdayDefaults  map[int]DayType                   `json:"weekday_defaults,omitempty"`
	KindSchedules    []TemperatureKindSchedule         `json:"kind_schedules,omitempty"`
	Vacuum           *VacuumDump                       `json:"vacuum,omitempty"`
}

// ImportCounts is what ExportDatabase collected (Count) or ImportDatabase
// stored. Failed counts the items an import could not store.
type ImportCounts struct {
	Devices, Rooms, WeekdayDefaults, KindSchedules, VacuumRobots, VacuumRegions int
	Failed                                                                      int
}

// ParseDatabaseImport reads the unified format, or the legacy bare array of devices.
func ParseDatabaseImport(data []byte) (*DatabaseImport, error) {
	var in DatabaseImport
	if err := json.Unmarshal(data, &in); err != nil {
		var devices []Device
		if err2 := json.Unmarshal(data, &devices); err2 != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		in = DatabaseImport{Devices: devices}
	}
	return &in, nil
}

// ExportDatabase collects the document over RPC. A section the daemon cannot
// serve (service disabled, method unknown to an older daemon, timeout) is not
// fatal, but it is recorded in Skipped so that it is never mistaken for an
// empty one.
func ExportDatabase(ctx context.Context, c Client, pattern string, devicesOnly bool) (*DatabaseExport, error) {
	out := &DatabaseExport{}
	found, err := c.LookupDevices(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup devices: %w", err)
	}
	if found != nil {
		for _, d := range *found {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			out.Devices = append(out.Devices, exportDevice(ctx, c, d, out))
		}
	}
	if devicesOnly {
		return out, nil
	}

	skip := func(section string, err error) { out.Skipped = append(out.Skipped, SkippedSection{section, err}) }
	if res, err := c.CallE(ctx, TemperatureList, nil); err != nil {
		skip("rooms", err)
	} else if rooms, ok := res.(*TemperatureRoomList); !ok {
		skip("rooms", fmt.Errorf("unexpected reply %T", res))
	} else if len(*rooms) > 0 {
		out.TemperatureRooms = *rooms
	}
	if res, err := c.CallE(ctx, TemperatureGetWeekdayDefaults, &TemperatureGetWeekdayDefaultsParams{}); err != nil {
		skip("weekday defaults", err)
	} else if d, ok := res.(*TemperatureWeekdayDefaults); !ok {
		skip("weekday defaults", fmt.Errorf("unexpected reply %T", res))
	} else if len(d.Defaults) > 0 {
		out.WeekdayDefaults = d.Defaults
	}
	if res, err := c.CallE(ctx, TemperatureGetKindSchedules, &TemperatureGetKindSchedulesParams{}); err != nil {
		skip("kind schedules", err)
	} else if k, ok := res.(*TemperatureKindScheduleList); !ok {
		skip("kind schedules", fmt.Errorf("unexpected reply %T", res))
	} else if len(*k) > 0 {
		out.KindSchedules = *k
	}
	if res, err := c.CallE(ctx, VacuumExport, nil); err != nil {
		skip("vacuum", err)
	} else if v, ok := res.(*VacuumDump); !ok {
		skip("vacuum", fmt.Errorf("unexpected reply %T", res))
	} else if len(v.Robots) > 0 || len(v.Regions) > 0 {
		out.Vacuum = v
	}
	return out, ctx.Err()
}

// exportDevice fetches the full record of a device found by LookupDevices.
// The lookup RPCs decode their reply into DeviceSummary, which has no room
// assignment, info or config; device.show returns the whole Device. If it
// fails, the summary is exported (so the device is not lost) and the missing
// detail is recorded in out.Skipped.
func exportDevice(ctx context.Context, c Client, d devices.Device, out *DatabaseExport) *Device {
	res, err := c.CallE(ctx, DeviceShow, &DeviceShowParams{Identifier: d.Id()})
	if err == nil {
		if full, ok := res.(*Device); ok && full != nil {
			return full
		}
		err = fmt.Errorf("unexpected reply %T", res)
	}
	out.Skipped = append(out.Skipped, SkippedSection{"device " + d.Id() + " (room assignment, info, config)", err})
	fallback := &Device{DeviceSummary: DeviceSummary{
		DeviceIdentifier: DeviceIdentifier{Manufacturer_: d.Manufacturer(), Id_: d.Id()},
		Host_:            d.Host(),
		Name_:            d.Name(),
	}}
	if mac := d.Mac(); mac != nil {
		fallback.MAC = mac.String()
	}
	return fallback
}

// Count returns how many items the export holds, per section.
func (e *DatabaseExport) Count() ImportCounts {
	n := ImportCounts{
		Devices: len(e.Devices), Rooms: len(e.TemperatureRooms), WeekdayDefaults: len(e.WeekdayDefaults), KindSchedules: len(e.KindSchedules),
	}
	if e.Vacuum != nil {
		n.VacuumRobots, n.VacuumRegions = len(e.Vacuum.Robots), len(e.Vacuum.Regions)
	}
	return n
}

// ImportDatabase writes the document through RPC, reporting per-item progress
// to progress. A failing item does not stop the import, but it is counted in
// Failed and makes the call return an error once everything was attempted: a
// partial import must not look like a complete one. A cancelled context stops
// it at once.
func ImportDatabase(ctx context.Context, c Client, in *DatabaseImport, devicesOnly bool, progress io.Writer) (ImportCounts, error) {
	var n ImportCounts
	// fail records a failed item; on a cancelled context it returns that
	// error instead, so Ctrl-C does not print one warning per remaining item.
	fail := func(format string, args ...any) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n.Failed++
		fmt.Fprintf(progress, "⚠ "+format+"\n", args...)
		return nil
	}
	finish := func() (ImportCounts, error) {
		if n.Failed > 0 {
			return n, fmt.Errorf("%d item(s) could not be imported (see warnings above)", n.Failed)
		}
		return n, nil
	}

	// One temperature sensor per room (#752). Reject the whole file before
	// writing anything. Open question E3 on #684: to only warn instead, make
	// this block print the conflicts to progress and carry on.
	if conflicts := RoomSensorConflicts(in.Devices); len(conflicts) > 0 {
		for _, c := range conflicts {
			fmt.Fprintf(progress, "✗ %s\n", c)
		}
		return n, fmt.Errorf("import rejected, nothing written: %d room(s) with several temperature sensors", len(conflicts))
	}

	for i := range in.Devices {
		device := in.Devices[i]
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if _, err := c.CallE(ctx, DeviceUpdate, &device); err != nil {
			if cerr := fail("Failed to import device %s: %v", device.Id(), err); cerr != nil {
				return n, cerr
			}
			continue
		}
		n.Devices++
		fmt.Fprintf(progress, "✓ Imported device %s (%s)\n", device.Name(), device.Id())
	}
	if devicesOnly {
		return finish()
	}

	for roomID, room := range in.TemperatureRooms {
		ical := room.ICalURL
		params := &TemperatureSetParams{RoomID: roomID, Name: room.Name, Kinds: room.Kinds, Levels: room.Levels, ICalURL: &ical}
		if _, err := c.CallE(ctx, TemperatureSet, params); err != nil {
			if cerr := fail("Failed to import room %s: %v", roomID, err); cerr != nil {
				return n, cerr
			}
			continue
		}
		n.Rooms++
		fmt.Fprintf(progress, "✓ Imported room %s (%s)\n", room.Name, roomID)
	}
	for weekday, dayType := range in.WeekdayDefaults {
		if _, err := c.CallE(ctx, TemperatureSetWeekdayDefault, &TemperatureSetWeekdayDefaultParams{Weekday: weekday, DayType: dayType}); err != nil {
			if cerr := fail("Failed to import weekday default %d: %v", weekday, err); cerr != nil {
				return n, cerr
			}
			continue
		}
		n.WeekdayDefaults++
	}
	for _, schedule := range in.KindSchedules {
		ranges := make([]string, len(schedule.Ranges))
		for i, r := range schedule.Ranges {
			ranges[i] = fmt.Sprintf("%02d:%02d-%02d:%02d", r.Start/60, r.Start%60, r.End/60, r.End%60)
		}
		params := &TemperatureSetKindScheduleParams{Kind: schedule.Kind, DayType: schedule.DayType, Ranges: ranges}
		if _, err := c.CallE(ctx, TemperatureSetKindSchedule, params); err != nil {
			if cerr := fail("Failed to import kind schedule %s/%s: %v", schedule.Kind, schedule.DayType, err); cerr != nil {
				return n, cerr
			}
			continue
		}
		n.KindSchedules++
	}
	if in.Vacuum != nil && (len(in.Vacuum.Robots) > 0 || len(in.Vacuum.Regions) > 0) {
		res, err := c.CallE(ctx, VacuumImport, in.Vacuum)
		if err != nil {
			if cerr := fail("Failed to import vacuum robots and regions: %v", err); cerr != nil {
				return n, cerr
			}
		} else if r, ok := res.(*VacuumImportResult); ok {
			n.VacuumRobots, n.VacuumRegions = r.Robots, r.Regions
			fmt.Fprintf(progress, "✓ Imported %d vacuum robots, %d vacuum regions\n", r.Robots, r.Regions)
		} else if cerr := fail("Vacuum import returned an unexpected reply %T, result unknown", res); cerr != nil {
			return n, cerr
		}
	}
	return finish()
}

// RoomSensorConflicts returns one line per room holding two or more
// temperature sensors among devices, sorted by room id. Pure.
func RoomSensorConflicts(devices []Device) []string {
	byRoom := map[string][]string{}
	for i := range devices {
		d := &devices[i]
		if d.RoomId != "" && IsTemperatureSensor(d) {
			byRoom[d.RoomId] = append(byRoom[d.RoomId], d.Id())
		}
	}
	var out []string
	for room, ids := range byRoom {
		if len(ids) > 1 {
			sort.Strings(ids)
			out = append(out, fmt.Sprintf("configuration error: room %q has %d temperature sensors (%s); a room can have only one", room, len(ids), strings.Join(ids, ", ")))
		}
	}
	sort.Strings(out)
	return out
}
