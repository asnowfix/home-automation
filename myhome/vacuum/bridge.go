package vacuum

// The MQTT bridge (#782, part of the vacuum bridge epic #768): the daemon that
// holds the robot's single local connection also publishes the robot's state
// to the broker and serves commands from it, so other daemons can follow the
// robot without opening a second connection. Topic contract:
//
//	vacuum/<id>/state         retained, QoS 1: VacuumStatus + connected + ts
//	vacuum/<id>/rooms         retained, QoS 1: the regions vacuum.rooms returns
//	vacuum/<id>/availability  retained, QoS 1: {"online":true,"ts":...,"instance":...}
//	vacuum/<id>/cmd           never retained, QoS 1: commands from any instance
//	vacuum/<id>/result        not retained, QoS 1: one result per accepted command
//
// <id> is the robot id (BLID), a LAN identifier already returned by
// vacuum.list. The robot password is never part of any payload.
//
// Off unless MYHOME_VACUUM_BRIDGE is set (Config.Bridge): the daemon then
// behaves exactly as before the bridge existed.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

const (
	cmdMaxAge = 30 * time.Second // a command older (or newer, clock skew) than this is refused
	dedupeTTL = 5 * time.Minute  // command ids remembered, so a QoS 1 redelivery cannot run twice

	cmdCleanRegion = "clean_regions"
	dedupeMax      = 1000 // cap on remembered command ids
	maxQueued      = 2    // commands running or waiting; more are refused as busy
)

// Timings of the bridge. Variables so tests can shorten them.
var (
	stateMinInterval = time.Second      // state is published at most once per interval; the last value always goes out
	guardWait        = 3 * time.Second  // how long to wait for a retained availability message at start
	ownerFresh       = 90 * time.Second // an availability heartbeat younger than this means a live owner
	guardRecheck     = watchInterval    // how often a refused owner re-checks for a stale rival
	retainedWindow   = 2 * time.Second  // a command arriving this soon after subscribing, made before it, is a retained replay
)

// BridgeTopic returns the topic of one leaf (state, rooms, availability, cmd, result) of robot id.
func BridgeTopic(id, leaf string) string { return "vacuum/" + id + "/" + leaf }

// AvailabilityWill returns the last-will settings for the bridge of robot id.
// The daemon passes it to mqtt.SetWill before creating the MQTT client, so the
// broker publishes it if the owner dies. The availability heartbeat (every
// watchInterval) remains the second line of defence.
func AvailabilityWill(id, instance string) mqtt.Will {
	p, _ := json.Marshal(availability{Online: false, Instance: instance, Owner: processOwner})
	return mqtt.Will{Topic: BridgeTopic(id, "availability"), Payload: p, QoS: mqtt.AtLeastOnce, Retained: true}
}

// processOwner identifies this process among daemons: the instance name alone
// does not (it defaults to "myhome" everywhere), so it is the hostname plus a
// random id drawn at start. A daemon recognises its own availability only by it.
var processOwner = newOwnerID()

func newOwnerID() string {
	host, _ := os.Hostname()
	var b [8]byte
	_, _ = rand.Read(b[:])
	return host + "/" + hex.EncodeToString(b[:])
}

type availability struct {
	Online   bool      `json:"online"`
	TS       time.Time `json:"ts"`
	Instance string    `json:"instance,omitempty"`
	Owner    string    `json:"owner,omitempty"` // per-process id, see processOwner
}

// bridgeState is the payload of vacuum/<id>/state.
type bridgeState struct {
	myhome.VacuumStatus
	Connected bool      `json:"connected"` // the owner has a live robot link
	TS        time.Time `json:"ts"`        // publish time
}

// BridgeCommand is the payload of vacuum/<id>/cmd.
type BridgeCommand struct {
	ID         string          `json:"id"`
	Cmd        string          `json:"cmd"`
	PmapID     string          `json:"pmap_id,omitempty"`
	MapVersion string          `json:"user_pmapv_id,omitempty"`
	Regions    []roomba.Region `json:"regions,omitempty"`
	Origin     string          `json:"origin,omitempty"`
	TS         time.Time       `json:"ts"`
}

// BridgeResult is the payload of vacuum/<id>/result.
type BridgeResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	AckMS int64  `json:"ack_ms"`
}

type bridge struct {
	s        *Service
	r        *robot
	mc       mqtt.Client
	instance string
	owner    string // per-process id stamped on availability

	inflight   atomic.Int32  // commands running or waiting
	dirty      chan struct{} // state needs publishing (capacity 1)
	roomsDirty atomic.Bool   // rooms need publishing too
	guarding   atomic.Bool   // the single-owner guard is listening

	throttle, window          time.Duration // stateMinInterval and retainedWindow, read once
	guardWait, recheck, fresh time.Duration // likewise guardWait, guardRecheck, ownerFresh

	runMu        sync.Mutex // commands run one at a time
	mu           sync.Mutex
	seen         map[string]time.Time // command id -> first seen
	subscribedAt time.Time
}

func newBridge(s *Service, r *robot) *bridge {
	owner := s.cfg.Owner
	if owner == "" {
		owner = processOwner
	}
	return &bridge{
		s: s, r: r, mc: s.cfg.MQTT, instance: s.cfg.Instance, owner: owner,
		dirty: make(chan struct{}, 1), seen: map[string]time.Time{},
		throttle: stateMinInterval, window: retainedWindow,
		guardWait: guardWait, recheck: guardRecheck, fresh: ownerFresh,
	}
}

func (b *bridge) topic(leaf string) string { return BridgeTopic(b.r.rec.BLID, leaf) }

// notify asks for the state (and rooms) to be published, coalescing bursts.
func (b *bridge) notify(rooms bool) {
	if rooms {
		b.roomsDirty.Store(true)
	}
	select {
	case b.dirty <- struct{}{}:
	default:
	}
}

func (b *bridge) publish(topic string, v any, retained bool) {
	payload, err := json.Marshal(v)
	if err != nil {
		b.s.log.Error(err, "Vacuum bridge: cannot encode payload", "topic", topic)
		return
	}
	ctx, cancel := context.WithTimeout(b.s.ctx, 5*time.Second)
	defer cancel()
	if err := b.mc.Publish(ctx, topic, payload, mqtt.AtLeastOnce, retained, "vacuum-bridge"); err != nil {
		b.s.log.Info("Vacuum bridge: publish failed", "topic", topic, "error", err.Error())
	}
}

// claim implements the single-owner guard: it returns true when this daemon
// may connect to the robot, and false only when the service is stopping. While
// another process (a different owner id) has a fresh online availability
// retained, it logs an error and keeps waiting, re-checking every
// watchInterval: a live owner keeps refreshing its heartbeat so it is never
// taken over, while a dead one goes stale after ownerFresh (this also covers a
// restart whose previous boot could not say goodbye). It waits up to guardWait
// for the retained message at the first check.
func (b *bridge) claim() bool {
	var mu sync.Mutex
	var latest *availability
	arrived := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(b.s.ctx)
	defer cancel() // the guard's subscription ends with the guard
	var done atomic.Bool
	err := b.mc.SubscribeWithHandler(ctx, b.topic("availability"), 8, "vacuum-bridge-guard",
		func(_ string, payload []byte, _ string) error {
			if done.Load() {
				return nil
			}
			var a availability
			if json.Unmarshal(payload, &a) == nil {
				mu.Lock()
				latest = &a
				mu.Unlock()
				select {
				case arrived <- struct{}{}:
				default:
				}
			}
			return nil
		})
	defer done.Store(true)
	if err != nil {
		b.s.log.Info("Vacuum bridge: cannot check for another owner", "error", err.Error())
		return true // fail open: the bridge is an addition, the robot link must not depend on it
	}
	wait := b.guardWait
	logged := false
	for {
		select {
		case <-arrived:
		case <-time.After(wait):
		case <-b.s.ctx.Done():
			return false
		}
		mu.Lock()
		a := latest
		mu.Unlock()
		if a == nil || !a.Online || a.Owner == b.owner || time.Since(a.TS) >= b.fresh {
			return true
		}
		const msg = "Vacuum bridge: another daemon holds this robot; not connecting (the robot accepts a single local client); will re-check"
		if !logged { // an error once, then repeats at info
			logged = true
			b.s.log.Error(fmt.Errorf("robot already owned by instance %q (%s)", a.Instance, a.Owner), msg, "every", b.recheck)
		} else {
			b.s.log.Info(msg, "instance", a.Instance, "owner", a.Owner, "every", b.recheck)
		}
		wait = b.recheck
	}
}

// start subscribes to commands and runs the publisher until the service context ends.
func (b *bridge) start() {
	b.mu.Lock()
	b.subscribedAt = time.Now()
	b.mu.Unlock()
	err := b.mc.SubscribeWithHandler(b.s.ctx, b.topic("cmd"), 16, "vacuum-bridge", b.onCmd)
	if err != nil {
		b.s.log.Error(err, "Vacuum bridge: cannot subscribe to commands; state is still published")
	}
	b.publishAll()
	go b.loop()
}

func (b *bridge) publishAvailability(online bool) {
	b.publish(b.topic("availability"), availability{Online: online, TS: time.Now(), Instance: b.instance, Owner: b.owner}, true)
}

func (b *bridge) publishState() {
	v := b.s.view(b.r)
	st := bridgeState{Connected: v.Connected, TS: time.Now()}
	if v.Status != nil {
		st.VacuumStatus = *v.Status
	}
	b.publish(b.topic("state"), st, true)
}

func (b *bridge) publishRooms() {
	regions, err := b.s.regions(b.r)
	if err != nil {
		b.s.log.Info("Vacuum bridge: cannot read rooms", "error", err.Error())
		return
	}
	b.publish(b.topic("rooms"), myhome.VacuumRoomsResult{Regions: regions, CloudNames: b.s.cfg.CloudEnabled()}, true)
}

func (b *bridge) publishAll() {
	b.publishAvailability(true)
	b.publishState()
	b.publishRooms()
}

// loop publishes state on change (throttled) and everything on the heartbeat.
// The heartbeat also republishes state and rooms: their ts is what followers
// age, and a docked idle robot may say nothing for a long time.
func (b *bridge) loop() {
	t := time.NewTicker(watchInterval)
	defer t.Stop()
	for {
		select {
		case <-b.s.ctx.Done():
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			payload, _ := json.Marshal(availability{Online: false, TS: time.Now(), Instance: b.instance, Owner: b.owner})
			_ = b.mc.Publish(ctx, b.topic("availability"), payload, mqtt.AtLeastOnce, true, "vacuum-bridge")
			cancel()
			return
		case <-t.C:
			b.publishAll()
		case <-b.dirty:
			if b.roomsDirty.Swap(false) {
				b.publishRooms()
			}
			b.publishState()
			select { // throttle; a notify during the wait is published right after
			case <-b.s.ctx.Done():
			case <-time.After(b.throttle):
			}
		}
	}
}

// onCmd runs for every message on vacuum/<id>/cmd.
func (b *bridge) onCmd(_ string, payload []byte, _ string) error {
	var c BridgeCommand
	if err := json.Unmarshal(payload, &c); err != nil || c.ID == "" {
		var idOnly struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(payload, &idOnly) == nil && idOnly.ID != "" {
			b.publish(b.topic("result"), BridgeResult{ID: idOnly.ID, Error: "malformed command"}, false)
		}
		b.s.log.Info("Vacuum bridge: ignoring malformed command")
		return nil
	}
	now := time.Now()

	// The client API does not expose the MQTT retain flag, so a retained
	// replay is recognised by when it arrives and when it was made: the broker
	// delivers it right after the subscription, and it predates it.
	b.mu.Lock()
	sub := b.subscribedAt
	b.mu.Unlock()
	if now.Sub(sub) < b.window && c.TS.Before(sub) {
		b.s.log.Info("Vacuum bridge: ignoring a command that predates the subscription (retained replay)", "id", c.ID, "origin", c.Origin)
		return nil
	}

	b.mu.Lock()
	for id, at := range b.seen {
		if now.Sub(at) > dedupeTTL {
			delete(b.seen, id)
		}
	}
	_, dup := b.seen[c.ID]
	if !dup {
		if len(b.seen) >= dedupeMax { // bounded memory: forget the oldest
			var oldest string
			for id, at := range b.seen {
				if oldest == "" || at.Before(b.seen[oldest]) {
					oldest = id
				}
			}
			delete(b.seen, oldest)
		}
		b.seen[c.ID] = now
	}
	b.mu.Unlock()
	if dup {
		b.s.log.Info("Vacuum bridge: ignoring a duplicate command", "id", c.ID, "origin", c.Origin)
		return nil
	}

	if age := now.Sub(c.TS); c.TS.IsZero() || age > cmdMaxAge || age < -cmdMaxAge {
		b.s.log.Info("Vacuum bridge: refusing a stale command", "id", c.ID, "cmd", c.Cmd, "origin", c.Origin, "age", age.Round(time.Second))
		b.publish(b.topic("result"), BridgeResult{ID: c.ID, Error: "stale command (older than 30s or clock skew)"}, false)
		return nil
	}
	if b.inflight.Add(1) > maxQueued {
		b.inflight.Add(-1)
		b.mu.Lock()
		delete(b.seen, c.ID) // refused, not run: the caller may resend the same id
		b.mu.Unlock()
		b.publish(b.topic("result"), BridgeResult{ID: c.ID, Error: "busy: another command is still waiting for the robot"}, false)
		return nil
	}
	go b.exec(c)
	return nil
}

func (b *bridge) exec(c BridgeCommand) {
	defer b.inflight.Add(-1)
	b.runMu.Lock()
	defer b.runMu.Unlock()
	// Freshness was checked on receipt; a command that waited for the one
	// before it may have aged out since.
	if age := time.Since(c.TS); age > cmdMaxAge {
		b.publish(b.topic("result"), BridgeResult{ID: c.ID, Error: "stale command (expired while queued)"}, false)
		return
	}
	log := b.s.log.WithValues("id", c.ID, "cmd", c.Cmd, "origin", c.Origin)
	if c.Origin != b.instance {
		log.Info("Vacuum bridge: command from another instance")
	} else {
		log.Info("Vacuum bridge: command")
	}
	start := time.Now()
	err := b.dispatch(c)
	res := BridgeResult{ID: c.ID, OK: err == nil, AckMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		log.Info("Vacuum bridge: command failed", "error", res.Error)
	}
	b.publish(b.topic("result"), res, false)
}

func (b *bridge) dispatch(c BridgeCommand) error {
	ctx := b.s.ctx
	switch {
	case plainCommands[c.Cmd]:
		_, err := b.s.Command(ctx, b.r.rec.BLID, c.Cmd)
		return err
	case c.Cmd == cmdCleanRegion:
		if c.PmapID == "" || c.MapVersion == "" || len(c.Regions) == 0 {
			return fmt.Errorf("clean_regions needs pmap_id, user_pmapv_id and regions")
		}
		if err := b.validateClean(c); err != nil {
			return err
		}
		cl, err := b.s.client(b.r)
		if err != nil {
			return err
		}
		return cl.CleanRegions(ctx, roomba.Map{ID: c.PmapID, Version: c.MapVersion}, c.Regions)
	}
	return fmt.Errorf("unknown command %q", strings.TrimSpace(c.Cmd))
}

// validateClean refuses a room clean the robot's own RPC path would refuse: a
// map the robot does not report, regions not known on that map, regions whose
// map changed since they were learned (or that the cloud no longer lists), and
// a map version nobody learned. Retry and abort-watch stay with the caller.
func (b *bridge) validateClean(c BridgeCommand) error {
	known, err := b.s.regions(b.r)
	if err != nil {
		return err
	}
	if _, ok := b.s.robotMapVersion(b.r, c.PmapID); !ok {
		return fmt.Errorf("the robot does not report map %s: refusing to clean on it", c.PmapID)
	}
	versionKnown := false
	for _, want := range c.Regions {
		found := false
		for _, reg := range known {
			if reg.MapID != c.PmapID || reg.ID != want.ID || reg.Type != want.Type {
				continue
			}
			found = true
			if reg.Stale {
				return fmt.Errorf("region %s is stale (its map changed since it was learned, or the cloud no longer lists it)", want.ID)
			}
			if reg.MapVersion == c.MapVersion {
				versionKnown = true
			}
		}
		if !found {
			return fmt.Errorf("region %s (%s) is not known on map %s", want.ID, want.Type, c.PmapID)
		}
	}
	if !versionKnown {
		return fmt.Errorf("map version %q is not one known for map %s", c.MapVersion, c.PmapID)
	}
	return nil
}
