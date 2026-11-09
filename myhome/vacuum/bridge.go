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
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

const (
	cmdMaxAge  = 30 * time.Second // a command older (or newer, clock skew) than this is refused
	dedupeTTL  = 5 * time.Minute  // command ids remembered, so a QoS 1 redelivery cannot run twice
	ownerFresh = 90 * time.Second // an availability heartbeat younger than this means a live owner

	cmdEvac        = "evac"
	cmdCleanRegion = "clean_regions"
)

// Timings of the bridge. Variables so tests can shorten them.
var (
	stateMinInterval = time.Second     // state is published at most once per interval; the last value always goes out
	guardWait        = 3 * time.Second // how long to wait for a retained availability message at start
	retainedWindow   = 2 * time.Second // a command arriving this soon after subscribing, made before it, is a retained replay
)

// BridgeTopic returns the topic of one leaf (state, rooms, availability, cmd, result) of robot id.
func BridgeTopic(id, leaf string) string { return "vacuum/" + id + "/" + leaf }

// Will is the MQTT last-will of a bridge: the broker publishes it if the owner
// dies without saying goodbye.
type Will struct {
	Topic    string
	Payload  []byte
	QoS      byte
	Retained bool
}

// AvailabilityWill returns the last-will settings for the bridge of robot id.
// This is the single seam to the MQTT client's last-will option (#781): the
// caller that builds the MQTT client passes these to it. Until then the
// availability heartbeat (every watchInterval) is the only liveness signal and
// followers notice an owner crash after ownerFresh.
func AvailabilityWill(id, instance string) Will {
	p, _ := json.Marshal(availability{Online: false, Instance: instance})
	return Will{Topic: BridgeTopic(id, "availability"), Payload: p, QoS: mqtt.AtLeastOnce, Retained: true}
}

type availability struct {
	Online   bool      `json:"online"`
	TS       time.Time `json:"ts"`
	Instance string    `json:"instance,omitempty"`
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

	dirty      chan struct{} // state needs publishing (capacity 1)
	roomsDirty atomic.Bool   // rooms need publishing too
	guarding   atomic.Bool   // the single-owner guard is listening

	runMu        sync.Mutex // commands run one at a time
	mu           sync.Mutex
	seen         map[string]time.Time // command id -> first seen
	subscribedAt time.Time
}

func newBridge(s *Service, r *robot) *bridge {
	return &bridge{
		s: s, r: r, mc: s.cfg.MQTT, instance: s.cfg.Instance,
		dirty: make(chan struct{}, 1), seen: map[string]time.Time{},
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

// claim implements the single-owner guard: it reports false, and the caller
// must not connect to the robot, when another instance published a fresh
// online availability. It waits up to guardWait for the retained message.
func (b *bridge) claim() bool {
	got := make(chan availability, 1)
	b.guarding.Store(true)
	defer b.guarding.Store(false)
	err := b.mc.SubscribeWithHandler(b.s.ctx, b.topic("availability"), 8, "vacuum-bridge-guard",
		func(_ string, payload []byte, _ string) error {
			if !b.guarding.Load() {
				return nil
			}
			var a availability
			if json.Unmarshal(payload, &a) == nil {
				select {
				case got <- a:
				default:
				}
			}
			return nil
		})
	if err != nil {
		b.s.log.Info("Vacuum bridge: cannot check for another owner", "error", err.Error())
		return true // fail open: the bridge is an addition, the robot link must not depend on it
	}
	select {
	case a := <-got:
		if a.Online && a.Instance != b.instance && time.Since(a.TS) < ownerFresh {
			b.s.log.Error(fmt.Errorf("robot already owned by instance %q", a.Instance),
				"Vacuum bridge: another daemon holds this robot; not connecting (the robot accepts a single local client)")
			return false
		}
	case <-time.After(guardWait):
	case <-b.s.ctx.Done():
		return false
	}
	return true
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
	b.publish(b.topic("availability"), availability{Online: online, TS: time.Now(), Instance: b.instance}, true)
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
			payload, _ := json.Marshal(availability{Online: false, TS: time.Now(), Instance: b.instance})
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
			case <-time.After(stateMinInterval):
			}
		}
	}
}

// onCmd runs for every message on vacuum/<id>/cmd.
func (b *bridge) onCmd(_ string, payload []byte, _ string) error {
	var c BridgeCommand
	if err := json.Unmarshal(payload, &c); err != nil || c.ID == "" {
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
	if now.Sub(sub) < retainedWindow && c.TS.Before(sub) {
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
	go b.exec(c)
	return nil
}

func (b *bridge) exec(c BridgeCommand) {
	b.runMu.Lock()
	defer b.runMu.Unlock()
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
	case c.Cmd == cmdEvac:
		cl, err := b.s.client(b.r)
		if err != nil {
			return err
		}
		return cl.Command(ctx, cmdEvac, nil)
	case c.Cmd == cmdCleanRegion:
		if c.PmapID == "" || c.MapVersion == "" || len(c.Regions) == 0 {
			return fmt.Errorf("clean_regions needs pmap_id, user_pmapv_id and regions")
		}
		cl, err := b.s.client(b.r)
		if err != nil {
			return err
		}
		return cl.CleanRegions(ctx, roomba.Map{ID: c.PmapID, Version: c.MapVersion}, c.Regions)
	}
	return fmt.Errorf("unknown command %q", strings.TrimSpace(c.Cmd))
}
