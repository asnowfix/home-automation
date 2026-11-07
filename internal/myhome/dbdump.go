package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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

// DatabaseExport is the export side: devices stay opaque because they are
// marshalled as the daemon sent them.
type DatabaseExport struct {
	Devices          any                               `json:"devices,omitempty"`
	TemperatureRooms map[string]*TemperatureRoomConfig `json:"rooms,omitempty"`
	WeekdayDefaults  map[int]DayType                   `json:"weekday_defaults,omitempty"`
	KindSchedules    []TemperatureKindSchedule         `json:"kind_schedules,omitempty"`
	Vacuum           *VacuumDump                       `json:"vacuum,omitempty"`
}

// DatabaseImport is the import side.
type DatabaseImport struct {
	Devices          []Device                          `json:"devices,omitempty"`
	TemperatureRooms map[string]*TemperatureRoomConfig `json:"rooms,omitempty"`
	WeekdayDefaults  map[int]DayType                   `json:"weekday_defaults,omitempty"`
	KindSchedules    []TemperatureKindSchedule         `json:"kind_schedules,omitempty"`
	Vacuum           *VacuumDump                       `json:"vacuum,omitempty"`
}

// ImportCounts is what ImportDatabase stored.
type ImportCounts struct {
	Devices, Rooms, WeekdayDefaults, KindSchedules, VacuumRobots, VacuumRegions int
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

// ExportDatabase collects the document over RPC. Sections whose service is
// not running (an error from the daemon) are left out, not fatal.
func ExportDatabase(ctx context.Context, c Client, pattern string, devicesOnly bool) (*DatabaseExport, error) {
	out := &DatabaseExport{}
	found, err := c.LookupDevices(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup devices: %w", err)
	}
	if found != nil && len(*found) > 0 {
		out.Devices = found
	}
	if devicesOnly {
		return out, nil
	}
	if res, err := c.CallE(ctx, TemperatureList, nil); err == nil {
		if rooms, ok := res.(*TemperatureRoomList); ok && len(*rooms) > 0 {
			out.TemperatureRooms = *rooms
		}
	}
	if res, err := c.CallE(ctx, TemperatureGetWeekdayDefaults, &TemperatureGetWeekdayDefaultsParams{}); err == nil {
		if d, ok := res.(*TemperatureWeekdayDefaults); ok && len(d.Defaults) > 0 {
			out.WeekdayDefaults = d.Defaults
		}
	}
	if res, err := c.CallE(ctx, TemperatureGetKindSchedules, &TemperatureGetKindSchedulesParams{}); err == nil {
		if k, ok := res.(*TemperatureKindScheduleList); ok && len(*k) > 0 {
			out.KindSchedules = *k
		}
	}
	if res, err := c.CallE(ctx, VacuumExport, nil); err == nil {
		if v, ok := res.(*VacuumDump); ok && (len(v.Robots) > 0 || len(v.Regions) > 0) {
			out.Vacuum = v
		}
	}
	return out, nil
}

// Count returns how many items the export holds, per section.
func (e *DatabaseExport) Count() ImportCounts {
	n := ImportCounts{
		Rooms: len(e.TemperatureRooms), WeekdayDefaults: len(e.WeekdayDefaults), KindSchedules: len(e.KindSchedules),
	}
	switch d := e.Devices.(type) {
	case *[]devices.Device:
		n.Devices = len(*d)
	}
	if e.Vacuum != nil {
		n.VacuumRobots, n.VacuumRegions = len(e.Vacuum.Robots), len(e.Vacuum.Regions)
	}
	return n
}

// ImportDatabase writes the document through RPC, reporting per-item progress
// and non-fatal failures to progress. Failures of single items do not stop the
// import; a cancelled context does.
func ImportDatabase(ctx context.Context, c Client, in *DatabaseImport, devicesOnly bool, progress io.Writer) (ImportCounts, error) {
	var n ImportCounts
	for i := range in.Devices {
		device := in.Devices[i]
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if _, err := c.CallE(ctx, DeviceUpdate, &device); err != nil {
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			fmt.Fprintf(progress, "⚠ Failed to import device %s: %v\n", device.Id(), err)
			continue
		}
		n.Devices++
		fmt.Fprintf(progress, "✓ Imported device %s (%s)\n", device.Name(), device.Id())
	}
	if devicesOnly {
		return n, nil
	}

	for roomID, room := range in.TemperatureRooms {
		ical := room.ICalURL
		params := &TemperatureSetParams{RoomID: roomID, Name: room.Name, Kinds: room.Kinds, Levels: room.Levels, ICalURL: &ical}
		if _, err := c.CallE(ctx, TemperatureSet, params); err != nil {
			fmt.Fprintf(progress, "⚠ Failed to import room %s: %v\n", roomID, err)
			continue
		}
		n.Rooms++
		fmt.Fprintf(progress, "✓ Imported room %s (%s)\n", room.Name, roomID)
	}
	for weekday, dayType := range in.WeekdayDefaults {
		if _, err := c.CallE(ctx, TemperatureSetWeekdayDefault, &TemperatureSetWeekdayDefaultParams{Weekday: weekday, DayType: dayType}); err != nil {
			fmt.Fprintf(progress, "⚠ Failed to import weekday default %d: %v\n", weekday, err)
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
			fmt.Fprintf(progress, "⚠ Failed to import kind schedule %s/%s: %v\n", schedule.Kind, schedule.DayType, err)
			continue
		}
		n.KindSchedules++
	}
	if in.Vacuum != nil && (len(in.Vacuum.Robots) > 0 || len(in.Vacuum.Regions) > 0) {
		res, err := c.CallE(ctx, VacuumImport, in.Vacuum)
		if err != nil {
			fmt.Fprintf(progress, "⚠ Failed to import vacuum robots and regions: %v\n", err)
		} else if r, ok := res.(*VacuumImportResult); ok {
			n.VacuumRobots, n.VacuumRegions = r.Robots, r.Regions
			fmt.Fprintf(progress, "✓ Imported %d vacuum robots, %d vacuum regions\n", r.Robots, r.Regions)
		}
	}
	return n, nil
}
