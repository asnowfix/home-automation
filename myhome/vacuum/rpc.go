package vacuum

import (
	"context"
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
			Type: rec.RegionType, Kind: rec.Kind, Name: rec.Name, Source: rec.Source, RobotMapVersion: ver,
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
