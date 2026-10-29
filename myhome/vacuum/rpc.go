package vacuum

import (
	"context"
	"errors"
	"fmt"
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
	myhome.RegisterMethodHandler(myhome.VacuumRoomsSync, func(ctx context.Context, in any) (any, error) {
		return s.SyncRooms(ctx, in.(*myhome.VacuumParams).Robot)
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
	if err := c.Command(ctx, cmd, nil); err != nil {
		return nil, err
	}
	return &myhome.VacuumOKResult{OK: true}, nil
}

// Clean cleans the named rooms, in order. All rooms must be on the same map.
// The map version sent is always the robot's own current one, read from its
// live state now, never the stored cloud version (#628).
//
// A stale region is refused rather than guessed, since a wrong id could clean
// the wrong place: a learned region whose map changed since, or a cloud region
// the last sync no longer listed. A cloud region whose map version differs from
// the robot's is NOT refused: that is the normal state (the cloud keeps the
// last map confirmed in the app, the robot moves on after each clean).
func (s *Service) Clean(ctx context.Context, sel string, rooms []string) (*myhome.VacuumOKResult, error) {
	if len(rooms) == 0 {
		return nil, errors.New("no room given (use vacuum.command start for a whole-home clean)")
	}
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	known, err := s.regions(r)
	if err != nil {
		return nil, err
	}
	var m roomba.Map
	var regions []roomba.Region
	for _, room := range rooms {
		reg, err := findRegion(known, room)
		if err != nil {
			return nil, err
		}
		if reg.Stale {
			if reg.Source == myhome.VacuumSourceCloud {
				return nil, fmt.Errorf("room %q is no longer on the iRobot cloud map (last `myhome ctl vacuum rooms sync`)", room)
			}
			return nil, fmt.Errorf("room %q was learned on an older version of the map; run `myhome ctl vacuum rooms sync` (or start one room clean from the iRobot app) to refresh it", room)
		}
		if m.ID != "" && m.ID != reg.MapID {
			return nil, fmt.Errorf("rooms are on different maps (%s, %s)", m.ID, reg.MapID)
		}
		version := reg.RobotMapVersion
		if version == "" {
			if reg.Source == myhome.VacuumSourceCloud {
				// Never fall back to the cloud's version: it is not the robot's.
				return nil, fmt.Errorf("robot %s has not reported map %s yet (is it connected?)", r.rec.Name, reg.MapID)
			}
			version = reg.MapVersion // learned from the robot itself
		}
		m = roomba.Map{ID: reg.MapID, Version: version}
		regions = append(regions, roomba.Region{ID: reg.ID, Type: reg.Type})
	}
	c, err := s.client(r)
	if err != nil {
		return nil, err
	}
	if err := c.CleanRegions(ctx, m, regions); err != nil {
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

// Rooms lists the known regions, flagging stale ones and cloud ones whose map
// version differs from the robot's.
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
			Type: rec.RegionType, Kind: rec.Kind, Name: rec.Name, Source: rec.Source, RobotMapVersion: ver,
		}
		if rec.Source == myhome.VacuumSourceCloud {
			reg.Stale = rec.Removed
			reg.Unverified = ver != rec.MapVersion
		} else {
			reg.Source = myhome.VacuumSourceRobot
			// Without live state we cannot tell; the robot re-checks the version anyway.
			reg.Stale = len(current) > 0 && (!known || ver != rec.MapVersion)
		}
		out = append(out, reg)
	}
	return out, nil
}

// NameRoom gives a region a local name. When the iRobot account is configured
// the cloud is the source of truth for names (owner decision, #628: no verified
// rename API exists), so regions the cloud provided are refused; a sync would
// overwrite the name anyway.
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
