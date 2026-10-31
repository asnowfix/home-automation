package vacuum

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

// Room-clean policy (#628). Measured on one robot (Edmond, Roomba i7) on
// 2026-10-30: a room clean naming the robot's own reported map version
// (261029T090306) was acknowledged, opened a mission and aborted within ~3 s
// with cleanMissionStatus.error 224, never leaving the dock. The same command
// naming the iRobot cloud's version (251027T114719) cleaned the kitchen.
// Hypothesis, unverified: the robot reports its latest learned map version,
// while a clean must name the version the cloud considers active.
//
// So the first attempt names, for a cloud room, the stored cloud version, and
// for a room learned from a robot echo, that echo's version. If the robot
// aborts at once, one retry names the other known version for the same map
// (owner decision 2026-10-30). Never a third attempt.
var (
	// abortWindow is how long a room clean is watched, after the robot
	// acknowledged it, for an immediate abort.
	abortWindow = 15 * time.Second
	// retryOtherVersion enables the single retry with the other known map
	// version after an immediate abort. False: one attempt only.
	retryOtherVersion = true
)

// Clean cleans the named rooms, in order. All rooms must be on the same map,
// and that map must be one the robot reports. A stale room is refused rather
// than guessed, since a wrong id could clean the wrong place: a learned room
// whose map changed since, or a cloud room the last sync no longer listed.
//
// The command is acknowledged by the robot's echo, then watched for
// abortWindow. An immediate abort (back on the dock, cycle none, error set,
// without ever running) is retried once with the other known version when
// retryOtherVersion is set; see the policy notes above.
func (s *Service) Clean(ctx context.Context, sel string, rooms []string) (*myhome.VacuumCleanResult, error) {
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
	var mapID, first string
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
		if mapID != "" && mapID != reg.MapID {
			return nil, fmt.Errorf("rooms are on different maps (%s, %s)", mapID, reg.MapID)
		}
		mapID = reg.MapID
		// A cloud room's version wins over a learned one: it is the version
		// measured to work.
		if first == "" || reg.Source == myhome.VacuumSourceCloud {
			first = reg.MapVersion
		}
		regions = append(regions, roomba.Region{ID: reg.ID, Type: reg.Type})
	}
	if _, ok := s.robotMapVersion(r, mapID); !ok {
		return nil, fmt.Errorf("robot %s does not report map %s (not connected yet, or the map is gone): refusing to clean rooms on it", r.rec.Name, mapID)
	}
	c, err := s.client(r)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	interrupts := r.interrupts
	s.mu.Unlock()

	res := &myhome.VacuumCleanResult{MapID: mapID}
	att, err := s.cleanAttempt(ctx, r, c, mapID, first, regions)
	if err != nil {
		return nil, err
	}
	res.Attempts = append(res.Attempts, att)
	if att.Outcome != myhome.VacuumCleanAborted {
		res.OK = true
		return res, nil
	}

	second, why := s.retryVersion(r, mapID, first, known, interrupts)
	if second == "" {
		s.log.Info("Room clean aborted at once; not retrying", "robot", r.rec.Name, "map", mapID, "version", first, "error", att.Error, "reason", why)
		return nil, cleanFailure(res, why)
	}
	att2, err := s.cleanAttempt(ctx, r, c, mapID, second, regions)
	if err != nil {
		return nil, fmt.Errorf("%w (after a first attempt with map version %s aborted with error %d)", err, first, att.Error)
	}
	res.Attempts = append(res.Attempts, att2)
	if att2.Outcome != myhome.VacuumCleanAborted {
		res.OK = true
		return res, nil
	}
	return nil, cleanFailure(res, "")
}

// cleanAttempt sends one room clean and watches it for an immediate abort.
func (s *Service) cleanAttempt(ctx context.Context, r *robot, c robotClient, mapID, version string, regions []roomba.Region) (myhome.VacuumCleanAttempt, error) {
	att := myhome.VacuumCleanAttempt{MapVersion: version}
	ch := make(chan myhome.VacuumStatus, 64)
	s.mu.Lock()
	if r.watchers == nil {
		r.watchers = map[chan myhome.VacuumStatus]struct{}{}
	}
	r.watchers[ch] = struct{}{}
	// An error code left over from an earlier mission must not read as an
	// abort of this one: an abort needs a new mission or a new error code.
	var errBefore, missionBefore int
	if r.status != nil {
		errBefore, missionBefore = r.status.Error, r.status.Mission
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(r.watchers, ch)
		s.mu.Unlock()
	}()

	s.log.Info("Room clean: sending", "robot", r.rec.Name, "map", mapID, "version", version, "regions", len(regions))
	if err := c.CleanRegions(ctx, roomba.Map{ID: mapID, Version: version}, regions); err != nil {
		return att, err
	}

	timer := time.NewTimer(abortWindow)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			att.Outcome = myhome.VacuumCleanNoReport
			return att, nil
		case <-timer.C:
			att.Outcome = myhome.VacuumCleanNoReport
			s.log.Info("Room clean: no definite outcome within the watch window", "robot", r.rec.Name, "version", version, "window", abortWindow)
			return att, nil
		case st := <-ch:
			switch {
			case st.Phase == roomba.PhaseRun || st.Cycle == "clean":
				att.Outcome = myhome.VacuumCleanRunning
				s.log.Info("Room clean: running", "robot", r.rec.Name, "map", mapID, "version", version)
				return att, nil
			case st.Phase == roomba.PhaseCharge && st.Cycle == "none" && st.Error != 0 &&
				(st.Error != errBefore || st.Mission != missionBefore):
				att.Outcome, att.Error = myhome.VacuumCleanAborted, st.Error
				s.log.Info("Room clean: aborted at once", "robot", r.rec.Name, "map", mapID, "version", version, "error", st.Error)
				return att, nil
			}
		}
	}
}

// retryVersion returns the version for the single retry after an immediate
// abort, or "" and the reason there is none: retry disabled, the user sent a
// command meanwhile, the robot is not docked, or no other version is known.
func (s *Service) retryVersion(r *robot, mapID, first string, known []myhome.VacuumRegion, interruptsAtStart int) (string, string) {
	if !retryOtherVersion {
		return "", "retry disabled"
	}
	s.mu.Lock()
	interrupted := r.interrupts
	docked := r.status != nil && r.status.Phase == roomba.PhaseCharge
	s.mu.Unlock()
	if interrupted != interruptsAtStart {
		return "", "a stop, pause or dock command was sent meanwhile"
	}
	if !docked {
		return "", "the robot is not docked"
	}
	robotVer, _ := s.robotMapVersion(r, mapID)
	candidates := []string{robotVer}
	for _, reg := range known {
		if reg.MapID == mapID && reg.Source == myhome.VacuumSourceCloud && !reg.Stale {
			candidates = append(candidates, reg.MapVersion)
		}
	}
	for _, v := range candidates {
		if v != "" && v != first {
			return v, ""
		}
	}
	return "", "no other version of this map is known"
}

// robotMapVersion is the version the robot currently reports for mapID.
func (s *Service) robotMapVersion(r *robot, mapID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.status == nil {
		return "", false
	}
	for _, m := range r.status.Maps {
		if m.ID == mapID {
			return m.Version, true
		}
	}
	return "", false
}

// cleanFailure describes every attempt, with the hint for known error codes.
func cleanFailure(res *myhome.VacuumCleanResult, why string) error {
	var b strings.Builder
	b.WriteString("the robot acknowledged the room clean but aborted it at once")
	hints := map[int]bool{}
	for i, a := range res.Attempts {
		fmt.Fprintf(&b, "; attempt %d with map version %s: %s, error %d", i+1, a.MapVersion, a.Outcome, a.Error)
		hints[a.Error] = true
	}
	if why != "" {
		b.WriteString("; no retry: " + why)
	}
	for code := range hints {
		if h := myhome.VacuumErrorHint(code); h != "" {
			b.WriteString(". " + h)
		}
	}
	return errors.New(b.String())
}
