// Package vacuum is the daemon-side robot vacuum service (#606): it keeps one
// long-lived local connection to the configured robot, learns the robot's
// rooms, and serves the vacuum.* RPC methods for the CLI and the web UI.
//
// The robot is configured like every other integration secret (#626): its BLID
// and local password come from MYHOME_VACUUM_BLID / MYHOME_VACUUM_PASSWORD in
// the daemon's .env, never from a flag, the YAML file or the database.
//
// Room names and ids come from the iRobot cloud account (#628) when
// MYHOME_IROBOT_EMAIL / MYHOME_IROBOT_PASSWORD are set: see sync.go.
//
// Design notes and the field measurements behind them: docs/vacuum-plan.md.
package vacuum

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/roomba"
	"github.com/asnowfix/home-automation/pkg/roomba/cloud"
	"github.com/go-logr/logr"
)

// Timings of the per-robot supervisor. Variables so tests can shorten them.
var (
	discoveryTimeout = 8 * time.Second  // one discovery round before falling back to the stored host
	connectRetry     = 3 * time.Second  // the robot refuses a connect that follows another too closely
	connectTimeout   = 2 * time.Minute  // give up on this host and re-resolve the robot's address
	watchInterval    = 30 * time.Second // how often a live connection is checked
	downTolerance    = 2 * time.Minute  // how long auto-reconnect may fail before re-resolving
	badCredsBackoff  = 10 * time.Minute // a rejected password will not fix itself quickly
)

// Config identifies the robot and holds its local password and the iRobot
// account. Password and IRobotPassword are secrets: never log them, return
// them over RPC, or store them.
type Config struct {
	BLID     string // robot id (MYHOME_VACUUM_BLID)
	Password string // robot local password (MYHOME_VACUUM_PASSWORD), from .env only
	Host     string // optional fixed address (MYHOME_VACUUM_HOST); empty: discovery by BLID

	IRobotEmail    string // iRobot account email (MYHOME_IROBOT_EMAIL), for room sync
	IRobotPassword string // iRobot account password (MYHOME_IROBOT_PASSWORD), from .env only
}

// CloudEnabled reports whether the iRobot account is configured. Without it
// room sync is a no-op and room names are local.
func (c Config) CloudEnabled() bool { return c.IRobotEmail != "" && c.IRobotPassword != "" }

// Enabled reports whether the robot is configured. Without both BLID and
// password the service holds no connection and vacuum.list is empty.
func (c Config) Enabled() bool { return c.BLID != "" && c.Password != "" }

// robotClient is the part of *roomba.Client the service uses; faked in tests.
type robotClient interface {
	Connect(ctx context.Context, retry time.Duration) error
	Connected() bool
	Close()
	Status() (roomba.Status, bool)
	Command(ctx context.Context, cmd string, extra map[string]any) error
	CleanRegions(ctx context.Context, m roomba.Map, regions []roomba.Region) error
}

type newClientFunc func(log logr.Logger, blid, password, host string, onChange func(roomba.Status)) robotClient

// finder locates robots on the LAN; *roomba.Discoverer in production.
type finder interface {
	Find(ctx context.Context, blid string) (roomba.Info, error)
	Scan(ctx context.Context) ([]roomba.Info, error)
}

type robot struct {
	rec       robotRecord
	password  string      // from Config; never stored, logged or returned
	client    robotClient // nil while not connected
	status    *myhome.VacuumStatus
	connected bool

	watchers   map[chan myhome.VacuumStatus]struct{} // room cleans watching for an immediate abort
	interrupts int                                   // stop/pause/resume/dock commands sent; a change cancels a retry
}

// cloudMaps reads the robot's maps from the iRobot cloud; *cloud.Client in production.
type cloudMaps interface {
	Maps(ctx context.Context, email, password, blid string) ([]cloud.Map, error)
}

// Service supervises the configured robot.
type Service struct {
	ctx       context.Context
	log       logr.Logger
	store     *Storage
	cfg       Config
	finder    finder
	newClient newClientFunc
	cloud     cloudMaps

	mu     sync.Mutex
	robots map[string]*robot // by BLID

	syncMu sync.Mutex // one room sync at a time
}

// NewService starts supervising the configured robot. When cfg is not
// Enabled the service is idle: it holds no connection and lists no robot.
func NewService(ctx context.Context, log logr.Logger, store *Storage, cfg Config) (*Service, error) {
	s := &Service{
		ctx:    ctx,
		log:    log.WithName("Vacuum"),
		store:  store,
		cfg:    cfg,
		finder: &roomba.Discoverer{},
		newClient: func(log logr.Logger, blid, password, host string, onChange func(roomba.Status)) robotClient {
			return roomba.NewClient(log, blid, password, host, onChange)
		},
		cloud:  cloud.New(),
		robots: map[string]*robot{},
	}
	return s, s.start()
}

func (s *Service) start() error {
	if !s.cfg.Enabled() {
		if s.cfg.CloudEnabled() {
			s.log.Info("iRobot account set but no robot configured: room sync needs MYHOME_VACUUM_BLID and MYHOME_VACUUM_PASSWORD")
		}
		return nil
	}
	rec, err := s.store.robot(s.cfg.BLID)
	if err != nil {
		return err
	}
	r := &robot{rec: rec, password: s.cfg.Password}
	s.mu.Lock()
	s.robots[rec.BLID] = r
	s.mu.Unlock()
	go s.supervise(r)
	s.log.Info("Vacuum service started", "robot", rec.Name, "fixed_host", s.cfg.Host, "cloud_rooms", s.cfg.CloudEnabled())
	if s.cfg.CloudEnabled() {
		go s.syncAtStart()
	} else {
		s.log.Info("iRobot cloud room sync disabled (MYHOME_IROBOT_EMAIL / MYHOME_IROBOT_PASSWORD not set)")
	}
	return nil
}

// supervise keeps one robot connected for the life of the daemon: find its
// current address, connect with retries, and start over when the connection
// stays down. The robot's address is DHCP-assigned and has changed before, so
// it is re-resolved by BLID on every cycle, with the last known host as fallback.
func (s *Service) supervise(r *robot) {
	log := s.log.WithValues("robot", r.rec.Name)
	for s.ctx.Err() == nil {
		host := s.resolve(r)
		if host == "" {
			log.Info("Robot not found and no known address; retrying", "in", connectTimeout)
			s.sleep(connectTimeout)
			continue
		}
		c := s.newClient(s.log, r.rec.BLID, r.password, host, func(st roomba.Status) { s.onStatus(r, st) })
		cctx, cancel := context.WithTimeout(s.ctx, connectTimeout)
		err := c.Connect(cctx, connectRetry)
		cancel()
		if err != nil {
			c.Close()
			if s.ctx.Err() != nil {
				return
			}
			wait := time.Duration(0)
			if errors.Is(err, roomba.ErrBadCredentials) {
				wait = badCredsBackoff
				log.Error(err, "Robot rejected MYHOME_VACUUM_PASSWORD; run `myhome ctl vacuum pair` and update .env", "retry_in", wait)
			} else {
				log.Info("Could not connect; re-resolving the robot's address", "host", host, "error", err.Error())
			}
			s.sleep(wait)
			continue
		}
		if host != r.rec.Host {
			if err := s.store.saveHost(r.rec.BLID, host); err != nil {
				log.Error(err, "Failed to save robot address")
			}
		}
		s.mu.Lock()
		r.rec.Host, r.client, r.connected = host, c, true
		s.mu.Unlock()

		s.watch(r, c)

		s.mu.Lock()
		r.client, r.connected = nil, false
		s.mu.Unlock()
		c.Close()
	}
}

// watch returns when the context ends or the connection has stayed down for downTolerance.
func (s *Service) watch(r *robot, c robotClient) {
	t := time.NewTicker(watchInterval)
	defer t.Stop()
	var downSince time.Time
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		up := c.Connected()
		s.mu.Lock()
		r.connected = up
		s.mu.Unlock()
		switch {
		case up:
			downSince = time.Time{}
		case downSince.IsZero():
			downSince = time.Now()
		case time.Since(downSince) > downTolerance:
			s.log.Info("Connection down too long; re-resolving the robot", "robot", r.rec.Name, "down_for", time.Since(downSince).Round(time.Second))
			return
		}
	}
}

// resolve returns the address to connect to: MYHOME_VACUUM_HOST when set,
// otherwise discovery by BLID with the last known address as fallback.
func (s *Service) resolve(r *robot) string {
	if s.cfg.Host != "" {
		return s.cfg.Host
	}
	ctx, cancel := context.WithTimeout(s.ctx, discoveryTimeout)
	defer cancel()
	info, err := s.finder.Find(ctx, r.rec.BLID)
	if err == nil {
		return info.IP
	}
	s.log.V(1).Info("Discovery missed; using last known address", "robot", r.rec.Name, "host", r.rec.Host, "error", err.Error())
	return r.rec.Host
}

func (s *Service) sleep(d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	select {
	case <-s.ctx.Done():
	case <-time.After(d):
	}
}

// onStatus runs on the MQTT goroutine for every pushed update.
func (s *Service) onStatus(r *robot, st roomba.Status) {
	vs := toStatus(st)
	s.mu.Lock()
	prev := r.status
	r.status = &vs
	for ch := range r.watchers {
		select {
		case ch <- vs:
		default: // a full watcher misses an update rather than blocking the MQTT goroutine
		}
	}
	s.mu.Unlock()

	if prev == nil || prev.Phase != vs.Phase || prev.Error != vs.Error || prev.BinFull != vs.BinFull {
		s.log.Info("Robot state", "robot", r.rec.Name, "phase", vs.Phase, "cycle", vs.Cycle,
			"battery", vs.Battery, "error", vs.Error, "bin_full", vs.BinFull)
	}
	if st.Name != "" && st.Name != r.rec.Name {
		s.mu.Lock()
		r.rec.Name = st.Name
		s.mu.Unlock()
		if err := s.store.saveName(r.rec.BLID, st.Name); err != nil {
			s.log.Error(err, "Failed to save robot name")
		}
	}

	// Learn regions from room cleans, wherever they were started from.
	lc := st.LastCommand
	if lc.MapID == "" || len(lc.Regions) == 0 {
		return
	}
	for _, reg := range lc.Regions {
		err := s.store.learnRegion(regionRecord{
			BLID: r.rec.BLID, MapID: lc.MapID, MapVersion: lc.MapVer, RegionID: reg.ID, RegionType: reg.Type,
		})
		if err != nil {
			s.log.Error(err, "Failed to record region", "region", reg.ID)
		}
	}
}

func toStatus(st roomba.Status) myhome.VacuumStatus {
	vs := myhome.VacuumStatus{
		Battery: st.Battery, BinFull: st.BinFull, BinPresent: st.BinPresent,
		Phase: st.Phase, Cycle: st.Cycle, Error: st.Error, NotReady: st.NotReady,
		MissionMinutes: st.MissionMinutes, Mission: st.Mission, LastCommand: st.LastCommand.Command,
		UpdatedAt: time.Now(),
	}
	for _, m := range st.Maps {
		vs.Maps = append(vs.Maps, myhome.VacuumMap{ID: m.ID, Version: m.Version})
	}
	return vs
}

// pick resolves a robot selector: BLID or name, or "" for the configured robot.
func (s *Service) pick(sel string) (*robot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sel == "" {
		switch len(s.robots) {
		case 0:
			return nil, errors.New("no robot vacuum configured: set MYHOME_VACUUM_BLID and MYHOME_VACUUM_PASSWORD in the daemon's .env (see `myhome ctl vacuum pair`)")
		case 1:
			for _, r := range s.robots {
				return r, nil
			}
		default:
			return nil, fmt.Errorf("%d robots configured: name one", len(s.robots))
		}
	}
	if r, ok := s.robots[sel]; ok {
		return r, nil
	}
	for _, r := range s.robots {
		if strings.EqualFold(r.rec.Name, sel) {
			return r, nil
		}
	}
	return nil, fmt.Errorf("no robot vacuum %q", sel)
}

// client returns the robot's live client, or an error saying why there is none.
func (s *Service) client(r *robot) (robotClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.client == nil || !r.connected {
		return nil, fmt.Errorf("robot %s is not connected (last known address %q)", r.rec.Name, r.rec.Host)
	}
	return r.client, nil
}

func (s *Service) view(r *robot) myhome.VacuumRobot {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := myhome.VacuumRobot{ID: r.rec.BLID, Name: r.rec.Name, Host: r.rec.Host, Connected: r.connected}
	if r.status != nil {
		st := *r.status
		v.Status = &st
	}
	return v
}
