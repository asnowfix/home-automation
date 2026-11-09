package vacuum

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

// plainCommands are the commands vacuum.command accepts. Room cleans go through vacuum.clean.
var plainCommands = map[string]bool{
	roomba.CmdStart: true, roomba.CmdStop: true, roomba.CmdPause: true,
	roomba.CmdResume: true, roomba.CmdDock: true, roomba.CmdFind: true,
}

// RegisterHandlers registers the vacuum.* RPC methods.
func (s *Service) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.VacuumList, func(ctx context.Context, _ any) (any, error) {
		return s.List(), nil
	})
	myhome.RegisterMethodHandler(myhome.VacuumStatusGet, func(ctx context.Context, in any) (any, error) {
		return s.Status(in.(*myhome.VacuumParams).Robot)
	})
	myhome.RegisterMethodHandler(myhome.VacuumCommand, func(ctx context.Context, in any) (any, error) {
		p := in.(*myhome.VacuumCommandParams)
		return s.Command(ctx, p.Robot, p.Command)
	})
	myhome.RegisterMethodHandler(myhome.VacuumClean, func(ctx context.Context, in any) (any, error) {
		p := in.(*myhome.VacuumCleanParams)
		return s.Clean(ctx, p.Robot, p.Rooms)
	})
	myhome.RegisterMethodHandler(myhome.VacuumRooms, func(ctx context.Context, in any) (any, error) {
		return s.Rooms(in.(*myhome.VacuumParams).Robot)
	})
	myhome.RegisterMethodHandler(myhome.VacuumRoomName, func(ctx context.Context, in any) (any, error) {
		p := in.(*myhome.VacuumRoomNameParams)
		return s.NameRoom(p.Robot, p.RegionID, p.Name)
	})
	myhome.RegisterMethodHandler(myhome.VacuumRoomAssign, func(ctx context.Context, in any) (any, error) {
		return s.AssignRoom(ctx, in.(*myhome.VacuumRoomAssignParams))
	})
	myhome.RegisterMethodHandler(myhome.VacuumRoomsSync, func(ctx context.Context, in any) (any, error) {
		return s.SyncRooms(ctx, in.(*myhome.VacuumParams).Robot)
	})
	myhome.RegisterMethodHandler(myhome.VacuumExport, func(ctx context.Context, _ any) (any, error) {
		return s.store.Export()
	})
	myhome.RegisterMethodHandler(myhome.VacuumImport, func(ctx context.Context, in any) (any, error) {
		dump, ok := in.(*myhome.VacuumDump)
		if !ok || dump == nil {
			return nil, fmt.Errorf("vacuum.import: missing or invalid parameters")
		}
		return s.store.Import(dump)
	})
	s.log.Info("Vacuum RPC handlers registered")
}

// List returns the configured robot, or none when the integration is disabled.
func (s *Service) List() *myhome.VacuumListResult {
	s.mu.Lock()
	rs := make([]*robot, 0, len(s.robots))
	for _, r := range s.robots {
		rs = append(rs, r)
	}
	s.mu.Unlock()
	res := &myhome.VacuumListResult{Robots: []myhome.VacuumRobot{}}
	for _, r := range rs {
		res.Robots = append(res.Robots, s.view(r))
	}
	return res
}

// Status returns one robot.
func (s *Service) Status(sel string) (*myhome.VacuumRobot, error) {
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	v := s.view(r)
	return &v, nil
}

// Command sends a plain command and waits for the robot to acknowledge it.
func (s *Service) Command(ctx context.Context, sel, cmd string) (*myhome.VacuumOKResult, error) {
	if !plainCommands[cmd] {
		return nil, fmt.Errorf("unknown vacuum command %q (want start, stop, pause, resume, dock or find)", cmd)
	}
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	c, err := s.client(r)
	if err != nil {
		return nil, err
	}
	if cmd != roomba.CmdStart && cmd != roomba.CmdFind {
		// stop, pause, dock or resume: a room clean being watched must not retry.
		s.mu.Lock()
		r.interrupts++
		s.mu.Unlock()
	}
	if err := c.Command(ctx, cmd, nil); err != nil {
		return nil, err
	}
	return &myhome.VacuumOKResult{OK: true}, nil
}

func findRegion(known []myhome.VacuumRegion, room string) (myhome.VacuumRegion, error) {
	var matches []myhome.VacuumRegion
	for _, reg := range known {
		if strings.EqualFold(reg.Name, room) || reg.ID == room {
			matches = append(matches, reg)
		}
	}
	switch len(matches) {
	case 0:
		return myhome.VacuumRegion{}, fmt.Errorf("unknown room %q (see `myhome ctl vacuum rooms`)", room)
	case 1:
		return matches[0], nil
	}
	// The same id on several maps: prefer the one that is not stale.
	var fresh []myhome.VacuumRegion
	for _, m := range matches {
		if !m.Stale {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) == 1 {
		return fresh[0], nil
	}
	return myhome.VacuumRegion{}, fmt.Errorf("room %q is ambiguous (%d matches)", room, len(matches))
}

// Rooms lists the known regions, flagging stale ones, with the robot's
// reported version of each map for information.
func (s *Service) Rooms(sel string) (*myhome.VacuumRoomsResult, error) {
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	regions, err := s.regions(r)
	if err != nil {
		return nil, err
	}
	return &myhome.VacuumRoomsResult{Regions: regions, CloudNames: s.cfg.CloudEnabled()}, nil
}

func (s *Service) regions(r *robot) ([]myhome.VacuumRegion, error) {
	recs, err := s.store.regions(r.rec.BLID)
	if err != nil {
		return nil, err
	}
	current := map[string]string{} // map id -> current version, from the robot's live state
	s.mu.Lock()
	if r.status != nil {
		for _, m := range r.status.Maps {
			current[m.ID] = m.Version
		}
	}
	s.mu.Unlock()

	out := []myhome.VacuumRegion{}
	for _, rec := range recs {
		ver, known := current[rec.MapID]
		reg := myhome.VacuumRegion{
			MapID: rec.MapID, MapName: rec.MapName, MapVersion: rec.MapVersion, ID: rec.RegionID,
			Type: rec.RegionType, Kind: rec.Kind, Name: rec.Name, Source: rec.Source, RobotMapVersion: ver, RoomID: rec.RoomID,
		}
		if rec.Source == myhome.VacuumSourceCloud {
			reg.Stale = rec.Removed
		} else {
			reg.Source = myhome.VacuumSourceRobot
			// Without live state we cannot tell; the robot re-checks the version anyway.
			reg.Stale = len(current) > 0 && (!known || ver != rec.MapVersion)
		}
		out = append(out, reg)
	}
	return out, nil
}

// AssignRoom assigns a region to a daemon room (RoomID == "" unassigns) and
// reports the room it was in. The region is resolved here, not in the CLI, from
// the regions the daemon stores: by RegionName (the human-friendly name, case-
// and space-insensitive; what the CLI sends) or by RegionID (the robot's or
// cloud's id). With MapID only that map is searched; without, a name or id that
// matches on several maps resolves to the single non-stale match, else asks for
// --map. Two areas of one map with the same name cannot be told apart by --map:
// that is an error asking to rename one. A stale region may be assigned (the
// owner may be repairing a mapping); cleaning one stays refused by vacuum.clean.
func (s *Service) AssignRoom(ctx context.Context, p *myhome.VacuumRoomAssignParams) (*myhome.VacuumRoomAssignResult, error) {
	name, id, roomID := strings.TrimSpace(p.RegionName), strings.TrimSpace(p.RegionID), strings.TrimSpace(p.RoomID)
	if (name == "") == (id == "") {
		return nil, fmt.Errorf("give exactly one of the area's name or id")
	}
	r, err := s.pick(p.Robot)
	if err != nil {
		return nil, err
	}
	known, err := s.regions(r)
	if err != nil {
		return nil, err
	}
	what := fmt.Sprintf("area %q", name)
	if id != "" {
		what = fmt.Sprintf("region id %q", id)
	}
	var matches, fresh []myhome.VacuumRegion
	for _, reg := range known {
		hit := reg.ID == id
		if name != "" {
			hit = strings.EqualFold(strings.TrimSpace(reg.Name), name)
		}
		if hit && (p.MapID == "" || reg.MapID == p.MapID) {
			matches = append(matches, reg)
			if !reg.Stale {
				fresh = append(fresh, reg)
			}
		}
	}
	var reg myhome.VacuumRegion
	if dup := sameMapDuplicates(matches); len(dup) > 0 {
		// Checked before the stale/fresh pick: a stale duplicate must not resolve silently.
		return nil, fmt.Errorf("%d areas of one map match %s: %s; rename one so they differ (`vacuum rooms name <region-id> <name>`, or in the iRobot app then `vacuum rooms sync`)", len(dup), what, pairs(dup))
	}
	switch {
	case len(matches) == 0 && p.MapID != "":
		return nil, fmt.Errorf("no %s on map %q for robot %s (see `myhome ctl vacuum rooms`)", what, p.MapID, r.rec.Name)
	case len(matches) == 0:
		return nil, fmt.Errorf("no %s for robot %s (see `myhome ctl vacuum rooms`; an area without a name needs `vacuum rooms name`)", what, r.rec.Name)
	case len(matches) == 1:
		reg = matches[0]
	case len(fresh) == 1:
		reg = fresh[0]
	default:
		return nil, fmt.Errorf("%s exists on %d robot maps (%s); pass --map <map-id>", what, len(matches), pairs(matches))
	}
	if roomID != "" {
		roomID, err = resolveRoom(ctx, roomID)
		if err != nil {
			return nil, err
		}
	}
	prev, err := s.store.assignRegion(r.rec.BLID, reg.MapID, reg.ID, roomID)
	if err != nil {
		return nil, err
	}
	return &myhome.VacuumRoomAssignResult{MapID: reg.MapID, RegionID: reg.ID, RegionName: reg.Name, RoomID: roomID, Previous: prev}, nil
}

// pairs renders regions as "map/id" pairs for error messages.
func pairs(regs []myhome.VacuumRegion) string {
	var parts []string
	for _, r := range regs {
		parts = append(parts, r.MapID+"/"+r.ID)
	}
	return strings.Join(parts, ", ")
}

// sameMapDuplicates returns the regions of every map that holds more than one match.
func sameMapDuplicates(matches []myhome.VacuumRegion) []myhome.VacuumRegion {
	count := map[string]int{}
	for _, m := range matches {
		count[m.MapID]++
	}
	var dup []myhome.VacuumRegion
	for _, m := range matches {
		if count[m.MapID] > 1 {
			dup = append(dup, m)
		}
	}
	return dup
}

// resolveRoom returns the id of the daemon room designated by ref, which is the
// room's id or its name. It asks the rooms service through its RPC verbs rather
// than importing myhome/rooms (an RPC boundary, as for every cross-service
// call; vacuum must not depend on rooms). An exact id match wins over a name
// match; names are compared case-insensitively and trimmed; a name shared by
// several rooms is refused with the candidates listed.
func resolveRoom(ctx context.Context, ref string) (string, error) {
	get, err := myhome.Methods(myhome.TemperatureGet)
	if err != nil {
		return "", fmt.Errorf("rooms are not enabled on this daemon")
	}
	if _, err := get.ActionE(ctx, &myhome.TemperatureGetParams{RoomID: ref}); err == nil {
		return ref, nil
	} else if !errors.Is(err, myhome.ErrRoomNotFound) {
		return "", fmt.Errorf("checking room %q: %w", ref, err)
	}
	list, err := myhome.Methods(myhome.TemperatureList)
	if err != nil {
		return "", fmt.Errorf("unknown room %q", ref)
	}
	out, err := list.ActionE(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("listing rooms to look up %q: %w", ref, err)
	}
	rooms, _ := out.(*myhome.TemperatureRoomList)
	var ids []string
	if rooms != nil {
		for id, cfg := range *rooms {
			if cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.Name), ref) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("unknown room %q (neither a room id nor a room name)", ref)
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("room name %q matches %d rooms (%s): use the room id", ref, len(ids), strings.Join(ids, ", "))
}

// NameRoom gives a region a local name. When the iRobot account is configured
// the cloud is the source of truth for names (owner decision, #628: no usable
// rename API is known for this robot, see docs/irobot-api-verbs.md), so regions
// the cloud provided are refused; a sync would overwrite the name anyway. This
// is the policy's code point, with Storage.syncCloud, if a rename API turns up.
func (s *Service) NameRoom(sel, regionID, name string) (*myhome.VacuumOKResult, error) {
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	cloudOwned := s.cfg.CloudEnabled()
	n, err := s.store.nameRegion(r.rec.BLID, regionID, strings.TrimSpace(name), cloudOwned)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return &myhome.VacuumOKResult{OK: true}, nil
	}
	if cloudOwned {
		recs, err := s.store.regions(r.rec.BLID)
		if err != nil {
			return nil, err
		}
		for _, rec := range recs {
			if rec.RegionID == regionID && rec.Source == myhome.VacuumSourceCloud {
				return nil, fmt.Errorf("the name of region %s is managed by the iRobot cloud (MYHOME_IROBOT_* is set): it cannot be renamed here, and `myhome ctl vacuum rooms sync` refreshes it", regionID)
			}
		}
	}
	return nil, fmt.Errorf("no region %q for robot %s", regionID, r.rec.Name)
}
