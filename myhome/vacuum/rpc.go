package vacuum

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

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
	myhome.RegisterMethodHandler(myhome.VacuumPair, func(ctx context.Context, in any) (any, error) {
		return s.Pair(ctx, in.(*myhome.VacuumPairParams).Host)
	})
	s.log.Info("Vacuum RPC handlers registered")
}

// List returns every paired robot.
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

// Clean cleans the named rooms, in order. All rooms must be on the same map, and
// that map must not have changed since the rooms were learned: a stale region id
// could clean the wrong place, so it is refused rather than guessed.
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
			return nil, fmt.Errorf("room %q was learned on an older version of the map; start one room clean from the iRobot app to refresh it", room)
		}
		if m.ID != "" && m.ID != reg.MapID {
			return nil, fmt.Errorf("rooms are on different maps (%s, %s)", m.ID, reg.MapID)
		}
		m = roomba.Map{ID: reg.MapID, Version: reg.MapVersion}
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

// Rooms lists the learned regions, flagging those whose map has changed since.
func (s *Service) Rooms(sel string) (*myhome.VacuumRoomsResult, error) {
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	regions, err := s.regions(r)
	if err != nil {
		return nil, err
	}
	return &myhome.VacuumRoomsResult{Regions: regions}, nil
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
		out = append(out, myhome.VacuumRegion{
			MapID: rec.MapID, MapVersion: rec.MapVersion, ID: rec.RegionID, Type: rec.RegionType, Name: rec.Name,
			// Without live state we cannot tell; the robot re-checks the version anyway.
			Stale: len(current) > 0 && (!known || ver != rec.MapVersion),
		})
	}
	return out, nil
}

// NameRoom gives a learned region a name.
func (s *Service) NameRoom(sel, regionID, name string) (*myhome.VacuumOKResult, error) {
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	n, err := s.store.nameRegion(r.rec.BLID, regionID, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("no learned region %q for robot %s", regionID, r.rec.Name)
	}
	return &myhome.VacuumOKResult{OK: true}, nil
}

// Pair fetches the local password of a robot whose HOME button was just held,
// stores it, and starts supervising the robot. The password stays on this host.
func (s *Service) Pair(ctx context.Context, host string) (*myhome.VacuumRobot, error) {
	ctx, cancel := context.WithTimeout(ctx, pairTimeout)
	defer cancel()

	info, err := s.pairCandidate(ctx, host)
	if err != nil {
		return nil, err
	}

	var password string
	for {
		pctx, pcancel := context.WithTimeout(ctx, 4*time.Second)
		password, err = s.password(pctx, info.IP, info.BLID)
		pcancel()
		if err == nil || errors.Is(err, roomba.ErrNeedsButton) || ctx.Err() != nil {
			break
		}
		// The robot refuses a connection that follows another too closely: retry.
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	if err != nil {
		return nil, err
	}

	rec := robotRecord{BLID: info.BLID, Name: info.Name, Password: password, Host: info.IP}
	if err := s.store.saveRobot(rec); err != nil {
		return nil, err
	}
	s.log.Info("Robot paired", "robot", info.Name, "host", info.IP)

	s.mu.Lock()
	old, exists := s.robots[info.BLID]
	s.mu.Unlock()
	if exists {
		// Re-pairing: the supervisor picks the new password up on its next cycle.
		s.mu.Lock()
		old.rec.Password = password
		s.mu.Unlock()
		v := s.view(old)
		return &v, nil
	}
	s.add(rec)
	r, _ := s.pick(info.BLID)
	v := s.view(r)
	return &v, nil
}

// pairCandidate finds the robot to pair: the one at host, or the only robot on
// the LAN that answers discovery.
func (s *Service) pairCandidate(ctx context.Context, host string) (roomba.Info, error) {
	f := s.finder
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil {
			return roomba.Info{}, fmt.Errorf("host must be an IP address, got %q", host)
		}
		f = &roomba.Discoverer{Addr: &net.UDPAddr{IP: ip, Port: roomba.DiscoveryPort}}
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	found, err := f.Scan(sctx)
	if err != nil {
		return roomba.Info{}, err
	}
	switch len(found) {
	case 0:
		if host != "" {
			return roomba.Info{}, fmt.Errorf("no robot answered discovery at %s", host)
		}
		return roomba.Info{}, errors.New("no robot answered discovery; retry, or pass its IP address")
	case 1:
		return found[0], nil
	}
	names := make([]string, len(found))
	for i, f := range found {
		names[i] = f.Name + " (" + f.IP + ")"
	}
	return roomba.Info{}, fmt.Errorf("several robots answered (%s): pass the IP address of the one to pair", strings.Join(names, ", "))
}
