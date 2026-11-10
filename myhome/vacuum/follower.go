package vacuum

// Follower mode (#783, part of the vacuum bridge epic #768): a daemon without
// the robot password uses the robot through the broker, against the bridge of
// the daemon that owns it (bridge.go, #782). remoteRobot implements robotClient
// on top of the topic contract, so the vacuum.* RPCs and the UI work unchanged.
//
// Off unless MYHOME_VACUUM_FOLLOW is set (Config.Follow): nothing here runs and
// the broker is not touched.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

// Timings of the follower. Variables so tests can shorten them.
var (
	followStale   = 90 * time.Second                         // state or availability older than this (three heartbeats) is unknown
	resultWait    = roomba.DefaultAckTimeout + 5*time.Second // how long a command waits for its result
	followRefresh = 5 * time.Second                          // how often staleness is re-evaluated without any message
	followNow     = time.Now                                 // the clock; tests replace it
)

var (
	// ErrBridgeOffline is returned at once, without publishing, when the owner's bridge is not available.
	ErrBridgeOffline = errors.New("robot bridge offline: the daemon that owns the robot is not publishing; nothing was sent")
	// ErrUnconfirmed is returned when no result arrived in time. The command may or may not have run; it is never retried.
	ErrUnconfirmed = errors.New("unconfirmed: no result from the robot bridge in time; the command may or may not have run (not retried)")
)

// errFollower refuses the writes a follower leaves to the owner.
func errFollower(what, id string) error {
	return fmt.Errorf("this daemon follows robot %s through the broker: %s is written on the daemon that owns the robot (target it with -I <owner-instance>)", id, what)
}

// remoteRobot is a robotClient backed by the bridge topics of one robot.
type remoteRobot struct {
	mc       mqtt.Client
	id       string
	instance string
	onState  func(myhome.VacuumStatus) // called, outside the lock, for every state message
	onChange func()                    // called, outside the lock, when availability or rooms change

	stale, wait time.Duration // followStale and resultWait, read once

	mu      sync.Mutex
	avail   *availability
	state   *bridgeState
	rooms   *myhome.VacuumRoomsResult
	waiters map[string]chan BridgeResult
	cancel  context.CancelFunc
}

func newRemoteRobot(mc mqtt.Client, id, instance string) *remoteRobot {
	return &remoteRobot{mc: mc, id: id, instance: instance, stale: followStale, wait: resultWait, waiters: map[string]chan BridgeResult{}}
}

func (r *remoteRobot) topic(leaf string) string { return BridgeTopic(r.id, leaf) }

// Connect subscribes to the bridge topics. The broker replays the retained
// state, rooms and availability; the call does not wait for them.
func (r *remoteRobot) Connect(ctx context.Context, _ time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	handlers := []struct {
		leaf string
		fn   func([]byte)
	}{
		{"state", r.onStateMsg}, {"rooms", r.onRoomsMsg}, {"availability", r.onAvailMsg}, {"result", r.onResultMsg},
	}
	for _, h := range handlers {
		fn := h.fn
		err := r.mc.SubscribeWithHandler(ctx, r.topic(h.leaf), 8, "vacuum-follower",
			func(_ string, payload []byte, _ string) error { fn(payload); return nil })
		if err != nil {
			cancel()
			return fmt.Errorf("subscribe to %s: %w", r.topic(h.leaf), err)
		}
	}
	return nil
}

func (r *remoteRobot) Close() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *remoteRobot) onStateMsg(payload []byte) {
	var st bridgeState
	if json.Unmarshal(payload, &st) != nil {
		return
	}
	r.mu.Lock()
	r.state = &st
	r.mu.Unlock()
	if r.onState != nil {
		r.onState(st.VacuumStatus)
	}
}

func (r *remoteRobot) onRoomsMsg(payload []byte) {
	var rs myhome.VacuumRoomsResult
	if json.Unmarshal(payload, &rs) != nil {
		return
	}
	r.mu.Lock()
	r.rooms = &rs
	r.mu.Unlock()
}

func (r *remoteRobot) onAvailMsg(payload []byte) {
	var a availability
	if json.Unmarshal(payload, &a) != nil {
		return
	}
	r.mu.Lock()
	r.avail = &a
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
}

func (r *remoteRobot) onResultMsg(payload []byte) {
	var res BridgeResult
	if json.Unmarshal(payload, &res) != nil || res.ID == "" {
		return
	}
	r.mu.Lock()
	ch := r.waiters[res.ID]
	r.mu.Unlock()
	if ch != nil { // not ours: another follower's result
		select {
		case ch <- res:
		default:
		}
	}
}

// bridgeUp reports whether the owner's bridge announced itself online, recently.
func (r *remoteRobot) bridgeUp() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.avail != nil && r.avail.Online && followNow().Sub(r.avail.TS) <= r.stale
}

// known returns the last state when it can be trusted: the bridge is online,
// the owner has a live robot link, and the state is not older than followStale
// (the owner republishes it with a fresh ts on every heartbeat). Otherwise the
// state is unknown and the UI renders "--".
func (r *remoteRobot) known() (myhome.VacuumStatus, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := followNow()
	if r.avail == nil || !r.avail.Online || now.Sub(r.avail.TS) > r.stale {
		return myhome.VacuumStatus{}, false
	}
	if r.state == nil || !r.state.Connected || now.Sub(r.state.TS) > r.stale {
		return myhome.VacuumStatus{}, false
	}
	return r.state.VacuumStatus, true
}

func (r *remoteRobot) roomsResult() (myhome.VacuumRoomsResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rooms == nil {
		return myhome.VacuumRoomsResult{}, false
	}
	return *r.rooms, true
}

// Connected reports whether the robot is usable through the bridge, i.e. its state is known.
func (r *remoteRobot) Connected() bool { _, ok := r.known(); return ok }

// Status returns the robot's state as the robot client would, or false when unknown.
func (r *remoteRobot) Status() (roomba.Status, bool) {
	vs, ok := r.known()
	if !ok {
		return roomba.Status{}, false
	}
	st := roomba.Status{
		Battery: vs.Battery, BinFull: vs.BinFull, BinPresent: vs.BinPresent, Phase: vs.Phase, Cycle: vs.Cycle,
		Error: vs.Error, NotReady: vs.NotReady, MissionMinutes: vs.MissionMinutes, Mission: vs.Mission,
		LastCommand: roomba.LastCommand{Command: vs.LastCommand},
	}
	for _, m := range vs.Maps {
		st.Maps = append(st.Maps, roomba.Map{ID: m.ID, Version: m.Version})
	}
	return st, true
}

func (r *remoteRobot) Command(ctx context.Context, cmd string, _ map[string]any) error {
	return r.send(ctx, BridgeCommand{Cmd: cmd})
}

func (r *remoteRobot) CleanRegions(ctx context.Context, m roomba.Map, regions []roomba.Region) error {
	return r.send(ctx, BridgeCommand{Cmd: cmdCleanRegion, PmapID: m.ID, MapVersion: m.Version, Regions: regions})
}

// send publishes one command and waits for its result. It never retries: a
// timeout is "unconfirmed", and the caller decides what to do.
func (r *remoteRobot) send(ctx context.Context, c BridgeCommand) error {
	if !r.bridgeUp() {
		return ErrBridgeOffline
	}
	var b [12]byte
	_, _ = rand.Read(b[:])
	c.ID, c.Origin, c.TS = hex.EncodeToString(b[:]), r.instance, followNow()
	payload, err := json.Marshal(c)
	if err != nil {
		return err
	}
	ch := make(chan BridgeResult, 1)
	r.mu.Lock()
	r.waiters[c.ID] = ch
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.waiters, c.ID)
		r.mu.Unlock()
	}()

	if err := r.mc.Publish(ctx, r.topic("cmd"), payload, mqtt.AtLeastOnce, false, "vacuum-follower"); err != nil {
		return fmt.Errorf("could not publish the command to the broker (nothing was sent): %w", err)
	}
	timer := time.NewTimer(r.wait)
	defer timer.Stop()
	select {
	case res := <-ch:
		if res.OK {
			return nil
		}
		return fmt.Errorf("the robot bridge refused or failed the command: %s", res.Error)
	case <-timer.C:
		return ErrUnconfirmed
	case <-ctx.Done():
		return fmt.Errorf("%w (%v)", ErrUnconfirmed, ctx.Err())
	}
}

// startFollower runs follower mode: no database, no robot connection. The
// robot's state is whatever the owner's bridge publishes.
func (s *Service) startFollower() error {
	if s.cfg.MQTT == nil {
		return errors.New("vacuum follow mode needs the MQTT broker")
	}
	id := s.cfg.Follow
	remote := newRemoteRobot(s.cfg.MQTT, id, s.cfg.Instance)
	// The owner's name is not part of the topic contract: the id stands in for it.
	r := &robot{rec: robotRecord{BLID: id, Name: id}, client: remote, remote: remote}
	remote.onState = func(vs myhome.VacuumStatus) { s.followState(r, vs) }
	remote.onChange = func() { s.followRefresh(r) }
	s.mu.Lock()
	s.robots[id] = r
	s.mu.Unlock()
	if err := remote.Connect(s.ctx, 0); err != nil {
		return err
	}
	refresh := followRefresh
	go func() {
		t := time.NewTicker(refresh)
		defer t.Stop()
		for {
			select {
			case <-s.ctx.Done():
				remote.Close()
				return
			case <-t.C:
				s.followRefresh(r)
			}
		}
	}()
	s.log.Info("Vacuum service started in follower mode: the robot is used through the broker", "robot", id)
	return nil
}

// followRefresh re-evaluates whether the followed state is known; unknown clears it.
func (s *Service) followRefresh(r *robot) {
	vs, ok := r.remote.known()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok {
		r.status, r.connected = nil, false
		return
	}
	r.status, r.connected = &vs, true
}

// followState applies a state message and feeds room cleans watching for an abort.
func (s *Service) followState(r *robot, vs myhome.VacuumStatus) {
	s.followRefresh(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !r.connected {
		return
	}
	for ch := range r.watchers {
		select {
		case ch <- vs:
		default:
		}
	}
}
