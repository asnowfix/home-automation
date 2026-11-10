package myhome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// DatabaseDump is the file format shared by `ctl db export` and `ctl db import`.
type DatabaseDump struct {
	Devices          []*Device                         `json:"devices,omitempty"`
	TemperatureRooms map[string]*TemperatureRoomConfig `json:"rooms,omitempty"`
	WeekdayDefaults  map[int]DayType                   `json:"weekday_defaults,omitempty"`
	KindSchedules    []TemperatureKindSchedule         `json:"kind_schedules,omitempty"`
}

// ParseDatabaseDump decodes the unified format, or the legacy devices-only
// format (a bare JSON array of devices).
func ParseDatabaseDump(data []byte) (*DatabaseDump, error) {
	var dump DatabaseDump
	if err := json.Unmarshal(data, &dump); err != nil {
		if err := json.Unmarshal(data, &dump.Devices); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
	}
	return &dump, nil
}

// ImportOptions tunes ImportDatabase.
type ImportOptions struct {
	DevicesOnly   bool // skip rooms, weekday defaults and kind schedules
	CreateMissing bool // also create devices the target database does not hold
}

// SectionReport counts what the import did to one section of the dump.
type SectionReport struct {
	Created   int
	Updated   int
	Unchanged int      // already identical in the target
	Skipped   int      // deliberately not applied (see Notes)
	Failed    int      // the daemon refused or failed the write (see Notes)
	Notes     []string // one line per skipped or failed item
	// Unavailable is non-empty when the target daemon does not serve the
	// section (verb not registered, e.g. an older release): nothing was written.
	Unavailable  string
	NotRequested bool // --devices-only
}

// KVSReport is the outcome of re-deriving the room-id KVS key on devices.
type KVSReport struct {
	Set           int
	NotApplicable int      // Gen1 / BLU devices have no KVS
	Unknown       int      // daemon too old to report the outcome
	Unreachable   []string // "<device id>: <reason>"; database updated, device not
}

// ImportReport is what ImportDatabase did, section by section.
type ImportReport struct {
	Devices         SectionReport
	Rooms           SectionReport
	WeekdayDefaults SectionReport
	KindSchedules   SectionReport
	// RoomKVS covers the device writes: room-id is written to the KVS of
	// every device whose room assignment the import creates or changes.
	RoomKVS KVSReport
}

// Incomplete reports whether something that was requested could not be
// applied: a section the daemon does not serve, or a refused write. Items
// skipped by policy and unreachable devices are reported but do not count.
func (r *ImportReport) Incomplete() bool {
	for _, s := range []*SectionReport{&r.Devices, &r.Rooms, &r.WeekdayDefaults, &r.KindSchedules} {
		if s.Unavailable != "" || s.Failed > 0 {
			return true
		}
	}
	return false
}

// IsMethodUnavailable reports whether err is the daemon saying it has no
// handler for the verb (an older release, or a service not started).
func IsMethodUnavailable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown or unregistered method")
}

// fatal errors stop the import: the user cancelled, or nobody answers (a late
// reply would also be taken for the answer to the next call).
func fatal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, ErrRPCTimeout)
}

func (s *SectionReport) fail(item string, err error) {
	s.Failed++
	s.Notes = append(s.Notes, fmt.Sprintf("%s: %v", item, err))
}

func (s *SectionReport) skip(item, why string) {
	s.Skipped++
	s.Notes = append(s.Notes, fmt.Sprintf("%s: %s", item, why))
}

// probe records why the section cannot be imported when the read of the
// target's current state failed.
func (s *SectionReport) probe(verb Verb, err error) {
	if IsMethodUnavailable(err) {
		s.Unavailable = fmt.Sprintf("the daemon does not serve %s", verb)
	} else {
		s.Unavailable = fmt.Sprintf("cannot read the current state (%s): %v", verb, err)
	}
}

// ImportDatabase applies dump to the daemon behind c through its RPC verbs.
//
// The dump never wins blindly over the target:
//   - rooms, weekday defaults and kind schedules are overwritten when they
//     differ (an empty ical_url in the dump leaves the target's untouched);
//   - a device is only created when --create-missing is given. The database
//     keeps no record of forgotten devices, so "absent" cannot be told from
//     "forgotten"; not recreating is the safe reading;
//   - a device the target already holds keeps its name, host, info and config
//     (the device owns those, a backup would only make them stale); only its
//     room assignment, which the user owns, is restored, and the room-id KVS
//     key is then re-derived on the device, which is a device write.
//
// A section the daemon does not serve is reported and left alone. The returned
// error is non-nil only for fatal conditions (cancelled, daemon silent); the
// report is then partial.
func ImportDatabase(ctx context.Context, c Client, dump *DatabaseDump, opt ImportOptions) (*ImportReport, error) {
	r := &ImportReport{}
	if opt.DevicesOnly {
		r.Rooms.NotRequested = true
		r.WeekdayDefaults.NotRequested = true
		r.KindSchedules.NotRequested = true
	} else {
		// Rooms first, so that a device's room exists when it is assigned.
		for _, step := range []func() error{
			func() error { return importRooms(ctx, c, dump, &r.Rooms) },
			func() error { return importWeekdayDefaults(ctx, c, dump, &r.WeekdayDefaults) },
			func() error { return importKindSchedules(ctx, c, dump, &r.KindSchedules) },
		} {
			if err := step(); err != nil {
				return r, err
			}
		}
	}
	return r, importDevices(ctx, c, dump, opt, r)
}

func sameKinds(a, b []RoomKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameLevels(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}

func importRooms(ctx context.Context, c Client, dump *DatabaseDump, rep *SectionReport) error {
	if len(dump.TemperatureRooms) == 0 {
		return nil
	}
	out, err := c.CallE(ctx, TemperatureList, nil)
	if err != nil {
		if fatal(ctx, err) {
			return err
		}
		rep.probe(TemperatureList, err)
		return nil
	}
	existing, _ := out.(*TemperatureRoomList)

	ids := make([]string, 0, len(dump.TemperatureRooms))
	for id := range dump.TemperatureRooms {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		want := dump.TemperatureRooms[id]
		var have *TemperatureRoomConfig
		if existing != nil {
			have = (*existing)[id]
		}
		if have != nil && have.Name == want.Name && sameKinds(have.Kinds, want.Kinds) &&
			sameLevels(have.Levels, want.Levels) && (want.ICalURL == "" || have.ICalURL == want.ICalURL) {
			rep.Unchanged++
			continue
		}
		if have == nil {
			res, err := c.CallE(ctx, RoomCreate, &RoomCreateParams{ID: id, Name: want.Name})
			if err != nil {
				if fatal(ctx, err) {
					return err
				}
				rep.fail("room "+id, err)
				continue
			}
			if cr, ok := res.(*RoomCreateResult); ok && !cr.Success {
				rep.fail("room "+id, errors.New(cr.Message))
				continue
			}
		}
		// room.edit writes every field, ical_url included (temperature.set cannot).
		edit := &RoomEditParams{ID: id, Name: &want.Name, Kinds: want.Kinds, Levels: want.Levels}
		if want.ICalURL != "" {
			edit.ICalURL = &want.ICalURL
		}
		res, err := c.CallE(ctx, RoomEdit, edit)
		if err == nil {
			if er, ok := res.(*RoomEditResult); ok && !er.Success {
				err = errors.New(er.Message)
			}
		}
		if err != nil {
			if fatal(ctx, err) {
				return err
			}
			rep.fail("room "+id, err)
			continue
		}
		if have == nil {
			rep.Created++
		} else {
			rep.Updated++
		}
	}
	return nil
}

func importWeekdayDefaults(ctx context.Context, c Client, dump *DatabaseDump, rep *SectionReport) error {
	if len(dump.WeekdayDefaults) == 0 {
		return nil
	}
	out, err := c.CallE(ctx, TemperatureGetWeekdayDefaults, &TemperatureGetWeekdayDefaultsParams{})
	if err != nil {
		if fatal(ctx, err) {
			return err
		}
		rep.probe(TemperatureGetWeekdayDefaults, err)
		return nil
	}
	var existing map[int]DayType
	if d, ok := out.(*TemperatureWeekdayDefaults); ok && d != nil {
		existing = d.Defaults
	}

	days := make([]int, 0, len(dump.WeekdayDefaults))
	for d := range dump.WeekdayDefaults {
		days = append(days, d)
	}
	sort.Ints(days)
	for _, day := range days {
		want := dump.WeekdayDefaults[day]
		have, found := existing[day]
		if found && have == want {
			rep.Unchanged++
			continue
		}
		if _, err := c.CallE(ctx, TemperatureSetWeekdayDefault, &TemperatureSetWeekdayDefaultParams{Weekday: day, DayType: want}); err != nil {
			if fatal(ctx, err) {
				return err
			}
			rep.fail(fmt.Sprintf("weekday %d", day), err)
			continue
		}
		if found {
			rep.Updated++
		} else {
			rep.Created++
		}
	}
	return nil
}

func formatRanges(ranges []TemperatureTimeRange) []string {
	s := make([]string, len(ranges))
	for i, r := range ranges {
		s[i] = fmt.Sprintf("%02d:%02d-%02d:%02d", r.Start/60, r.Start%60, r.End/60, r.End%60)
	}
	return s
}

func importKindSchedules(ctx context.Context, c Client, dump *DatabaseDump, rep *SectionReport) error {
	if len(dump.KindSchedules) == 0 {
		return nil
	}
	out, err := c.CallE(ctx, TemperatureGetKindSchedules, &TemperatureGetKindSchedulesParams{})
	if err != nil {
		if fatal(ctx, err) {
			return err
		}
		rep.probe(TemperatureGetKindSchedules, err)
		return nil
	}
	existing := map[string]string{} // "<kind>/<day type>" -> ranges
	if l, ok := out.(*TemperatureKindScheduleList); ok && l != nil {
		for _, s := range *l {
			existing[fmt.Sprintf("%s/%s", s.Kind, s.DayType)] = strings.Join(formatRanges(s.Ranges), ",")
		}
	}
	for _, s := range dump.KindSchedules {
		key := fmt.Sprintf("%s/%s", s.Kind, s.DayType)
		ranges := formatRanges(s.Ranges)
		have, found := existing[key]
		if found && have == strings.Join(ranges, ",") {
			rep.Unchanged++
			continue
		}
		_, err := c.CallE(ctx, TemperatureSetKindSchedule, &TemperatureSetKindScheduleParams{Kind: s.Kind, DayType: s.DayType, Ranges: ranges})
		if err != nil {
			if fatal(ctx, err) {
				return err
			}
			rep.fail("kind schedule "+key, err)
			continue
		}
		if found {
			rep.Updated++
		} else {
			rep.Created++
		}
	}
	return nil
}

func importDevices(ctx context.Context, c Client, dump *DatabaseDump, opt ImportOptions, r *ImportReport) error {
	rep := &r.Devices
	if len(dump.Devices) == 0 {
		return nil
	}
	out, err := c.CallE(ctx, DeviceExport, "*")
	if err != nil {
		if fatal(ctx, err) {
			return err
		}
		rep.probe(DeviceExport, err)
		return nil
	}
	byId := map[string]*Device{}
	byMAC := map[string]*Device{}
	if l, ok := out.(*[]*Device); ok && l != nil {
		for _, d := range *l {
			byId[d.Id()] = d
			if d.MAC != "" {
				byMAC[d.MAC] = d
			}
		}
	}

	for _, d := range dump.Devices {
		have := byId[d.Id()]
		if have == nil && d.MAC != "" {
			have = byMAC[d.MAC]
		}
		if have == nil {
			if !opt.CreateMissing {
				rep.skip(d.Id(), "not in the target database (forgotten, or not discovered yet): not recreated, see --create-missing")
				continue
			}
			if _, err := c.CallE(ctx, DeviceUpdate, d); err != nil {
				if fatal(ctx, err) {
					return err
				}
				rep.fail(d.Id(), err)
				continue
			}
			rep.Created++
			if d.RoomId != "" {
				if err := setRoom(ctx, c, d.Id(), d.RoomId, r); err != nil {
					return err
				}
			}
			continue
		}
		// An empty room in the dump means "none recorded": leave the target alone.
		if d.RoomId == "" || d.RoomId == have.RoomId {
			rep.Unchanged++
			continue
		}
		before := rep.Failed
		if err := setRoom(ctx, c, have.Id(), d.RoomId, r); err != nil {
			return err
		}
		if rep.Failed == before {
			rep.Updated++
		}
	}
	return nil
}

// setRoom assigns the room (database) and re-derives room-id on the device (KVS).
func setRoom(ctx context.Context, c Client, id, room string, r *ImportReport) error {
	out, err := c.CallE(ctx, DeviceSetRoom, &DeviceSetRoomParams{Identifier: id, RoomId: room})
	if err != nil {
		if fatal(ctx, err) {
			return err
		}
		r.Devices.fail(id+" room "+room, err)
		return nil
	}
	res, _ := out.(*DeviceSetRoomResult)
	switch {
	case res == nil || res.KVS == "":
		r.RoomKVS.Unknown++
	case res.KVS == KVSSet:
		r.RoomKVS.Set++
	case res.KVS == KVSNotApplicable:
		r.RoomKVS.NotApplicable++
	default:
		r.RoomKVS.Unreachable = append(r.RoomKVS.Unreachable, fmt.Sprintf("%s: %s", id, res.KVSError))
	}
	return nil
}
