// Package vacuum is the daemon-side robot vacuum service (#606): it keeps one
// long-lived local connection per paired robot, learns the robot's rooms, and
// serves the vacuum.* RPC methods for the CLI and the web UI.
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
	pairTimeout      = 12 * time.Second // must fit in the CLI's default 14 s RPC timeout
)

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
	client    robotClient // nil while not connected
	status    *myhome.VacuumStatus
	connected bool
}

// Service supervises the paired robots.
type Service struct {
	ctx       context.Context
	log       logr.Logger
	store     *Storage
	finder    finder
	newClient newClientFunc
	password  func(ctx context.Context, host, blid string) (string, error)

	mu     sync.Mutex
	robots map[string]*robot // by BLID
}

// NewService loads the paired robots and starts a supervisor for each.
// With no robot paired it is idle until vacuum.pair is called.
func NewService(ctx context.Context, log logr.Logger, store *Storage) (*Service, error) {
	s := &Service{
		ctx:    ctx,
		log:    log.WithName("Vacuum"),
		store:  store,
		finder: &roomba.Discoverer{},
		newClient: func(log logr.Logger, blid, password, host string, onChange func(roomba.Status)) robotClient {
			return roomba.NewClient(log, blid, password, host, onChange)
		},
		password: roomba.GetPassword,
		robots:   map[string]*robot{},
	}
	return s, s.start()
}

func (s *Service) start() error {
	recs, err := s.store.robots()
	if err != nil {
		return err
	}
	for _, rec := range recs {
		s.add(rec)
	}
	s.log.Info("Vacuum service started", "robots", len(recs))
	return nil
}

// add registers a robot and starts its supervisor.
func (s *Service) add(rec robotRecord) {
	s.mu.Lock()
	if _, exists := s.robots[rec.BLID]; exists {
		s.mu.Unlock()
		return
	}
	r := &robot{rec: rec}
	s.robots[rec.BLID] = r
	s.mu.Unlock()
	go s.supervise(r)
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
		c := s.newClient(s.log, r.rec.BLID, r.rec.Password, host, func(st roomba.Status) { s.onStatus(r, st) })
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
				log.Error(err, "Robot rejected its stored password; pair it again", "retry_in", wait)
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

func (s *Service) resolve(r *robot) string {
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
		MissionMinutes: st.MissionMinutes, LastCommand: st.LastCommand.Command,
		UpdatedAt: time.Now(),
	}
	for _, m := range st.Maps {
		vs.Maps = append(vs.Maps, myhome.VacuumMap{ID: m.ID, Version: m.Version})
	}
	return vs
}

// pick resolves a robot selector: BLID or name, or "" when exactly one robot is paired.
func (s *Service) pick(sel string) (*robot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sel == "" {
		switch len(s.robots) {
		case 0:
			return nil, errors.New("no robot vacuum paired (see `myhome ctl vacuum pair`)")
		case 1:
			for _, r := range s.robots {
				return r, nil
			}
		default:
			return nil, fmt.Errorf("%d robots paired: name one", len(s.robots))
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
