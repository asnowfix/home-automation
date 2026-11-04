package vacuum

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// syncTimeout bounds one whole room sync (four HTTP calls, each with its own
// timeout in pkg/roomba/cloud). A variable so tests can shorten it.
var syncTimeout = 90 * time.Second

// errCloudDisabled is returned by SyncRooms without an iRobot account.
var errCloudDisabled = errors.New("iRobot cloud room sync is not configured: set MYHOME_IROBOT_EMAIL and MYHOME_IROBOT_PASSWORD in the daemon's .env (see docs/vacuum.md)")

// syncAtStart is the one sync attempt made when the daemon starts (owner
// decision, #628: there is no periodic sync). It runs in its own goroutine, so
// a slow or unreachable cloud never delays the daemon, and a failure only
// logs: the rooms already stored stay in use.
func (s *Service) syncAtStart() {
	if _, err := s.SyncRooms(s.ctx, ""); err != nil && s.ctx.Err() == nil {
		s.log.Info("Room sync at start failed; keeping the stored rooms", "error", err.Error())
	}
}

// SyncRooms fetches the robot's maps and rooms from the iRobot cloud account and
// stores them (vacuum.roomsync). One login attempt, no retry. On any failure
// the stored rooms are kept unchanged.
//
// The cloud is the source of truth for room names (owner decision, #628): a
// sync overwrites local names, and a cloud room the cloud no longer lists is
// marked stale, not deleted. Room ids are stored with the cloud's map version,
// which normally differs from the robot's; a room clean always sends the
// robot's own version (see Clean).
func (s *Service) SyncRooms(ctx context.Context, sel string) (*myhome.VacuumRoomsSyncResult, error) {
	if !s.cfg.CloudEnabled() {
		return nil, errCloudDisabled
	}
	r, err := s.pick(sel)
	if err != nil {
		return nil, err
	}
	if !s.syncMu.TryLock() {
		return nil, errors.New("a room sync is already running")
	}
	defer s.syncMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	// cloud errors name the failing step and never carry a credential or token.
	maps, err := s.cloud.Maps(ctx, s.cfg.IRobotEmail, s.cfg.IRobotPassword, r.rec.BLID)
	if err != nil {
		s.log.Info("iRobot cloud room sync failed; stored rooms kept", "robot", r.rec.Name, "error", err.Error())
		return nil, fmt.Errorf("room sync failed, stored rooms kept: %w", err)
	}

	robotVersions := map[string]string{}
	s.mu.Lock()
	if r.status != nil {
		for _, m := range r.status.Maps {
			robotVersions[m.ID] = m.Version
		}
	}
	s.mu.Unlock()

	res := &myhome.VacuumRoomsSyncResult{Maps: []myhome.VacuumSyncedMap{}}
	var recs []regionRecord
	for _, m := range maps {
		res.Maps = append(res.Maps, myhome.VacuumSyncedMap{
			ID: m.ID, Name: m.Name, CloudVersion: m.Version, RobotVersion: robotVersions[m.ID], Rooms: len(m.Regions),
		})
		for _, reg := range m.Regions {
			recs = append(recs, regionRecord{
				MapID: m.ID, MapVersion: m.Version, MapName: m.Name,
				RegionID: reg.ID, RegionType: "rid", Name: reg.Name, Kind: reg.Type,
			})
		}
	}
	c, err := s.store.syncCloud(r.rec.BLID, recs)
	if err != nil {
		s.log.Error(err, "Failed to store the rooms from the iRobot cloud", "robot", r.rec.Name)
		return nil, fmt.Errorf("room sync: failed to store rooms: %w", err)
	}
	res.Added, res.Updated, res.Stale = c.added, c.updated, c.stale
	for _, m := range res.Maps {
		s.log.Info("iRobot cloud map synced", "robot", r.rec.Name, "map", m.ID, "name", m.Name, "rooms", m.Rooms,
			"cloud_version", m.CloudVersion, "robot_version", m.RobotVersion)
	}
	s.log.Info("iRobot cloud room sync done", "robot", r.rec.Name, "added", c.added, "updated", c.updated, "stale", c.stale)
	return res, nil
}
