package vacuum

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

const fakePassword = "fake-robot-password-do-not-publish"

type pub struct {
	topic    string
	payload  []byte
	qos      byte
	retained bool
}

// recMQTT wraps the mock to also record QoS and the retain flag, and the
// topics subscribed to, which the mock does not expose.
type recMQTT struct {
	*mqtt.RecordingMockClient
	mu   sync.Mutex
	pubs []pub
	subs map[string]bool
}

func newRecMQTT() *recMQTT {
	return &recMQTT{RecordingMockClient: mqtt.NewRecordingMockClient(), subs: map[string]bool{}}
}

func (m *recMQTT) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool, name string) error {
	m.mu.Lock()
	m.pubs = append(m.pubs, pub{topic, append([]byte(nil), payload...), qos, retained})
	m.mu.Unlock()
	return m.RecordingMockClient.Publish(ctx, topic, payload, qos, retained, name)
}

func (m *recMQTT) SubscribeWithHandler(ctx context.Context, topic string, qlen uint, name string, h func(string, []byte, string) error) error {
	m.mu.Lock()
	m.subs[topic] = true
	m.mu.Unlock()
	return m.RecordingMockClient.SubscribeWithHandler(ctx, topic, qlen, name, h)
}

func (m *recMQTT) subscribed(topic string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subs[topic]
}

func (m *recMQTT) published(topic string) []pub {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []pub
	for _, p := range m.pubs {
		if p.topic == topic {
			out = append(out, p)
		}
	}
	return out
}

func (m *recMQTT) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pubs)
}

func fastBridge(t *testing.T) {
	t.Helper()
	g, s, r, f, gr := guardWait, stateMinInterval, retainedWindow, ownerFresh, guardRecheck
	guardWait, stateMinInterval, retainedWindow = 50*time.Millisecond, 10*time.Millisecond, 0
	guardRecheck = 50 * time.Millisecond
	t.Cleanup(func() { guardWait, stateMinInterval, retainedWindow, ownerFresh, guardRecheck = g, s, r, f, gr })
}

func bridgeHarness(t *testing.T, mc *recMQTT, tweak ...func()) *harness {
	t.Helper()
	fastBridge(t)
	for _, f := range tweak {
		f()
	}
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50", Name: "Edmond"}},
		Config{BLID: blid, Password: fakePassword, Bridge: true, MQTT: mc, Instance: "prod", Owner: "boot-A"},
		&robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"})
	eventually(t, "bridge subscribed to commands", func() bool { return mc.subscribed(BridgeTopic(blid, "cmd")) })
	eventually(t, "robot connected", func() bool { return connected(h) })
	return h
}

func sendCmd(mc *recMQTT, c BridgeCommand) {
	p, _ := json.Marshal(c)
	mc.Feed(BridgeTopic(blid, "cmd"), p)
}

func results(mc *recMQTT) []BridgeResult {
	var out []BridgeResult
	for _, p := range mc.published(BridgeTopic(blid, "result")) {
		var r BridgeResult
		_ = json.Unmarshal(p.payload, &r)
		out = append(out, r)
	}
	return out
}

func commandsRun(h *harness) []string {
	c := h.last()
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.commands...)
}

func TestBridgePublishesRetainedStateRoomsAvailability(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	for _, leaf := range []string{"state", "rooms", "availability"} {
		eventually(t, leaf+" published", func() bool { return len(mc.published(BridgeTopic(blid, leaf))) > 0 })
		for _, p := range mc.published(BridgeTopic(blid, leaf)) {
			if !p.retained || p.qos != mqtt.AtLeastOnce {
				t.Errorf("%s: retained=%v qos=%d, want retained QoS 1", leaf, p.retained, p.qos)
			}
		}
	}

	// A status change from the robot reaches the retained state.
	h.last().onChange(roomba.Status{Battery: 77, Phase: "run", Cycle: "clean"})
	eventually(t, "state with battery 77", func() bool {
		ps := mc.published(BridgeTopic(blid, "state"))
		var st map[string]any
		return len(ps) > 0 && json.Unmarshal(ps[len(ps)-1].payload, &st) == nil &&
			st["battery"] == float64(77) && st["connected"] == true && st["ts"] != nil
	})

	// A robot reconnect republishes state and rooms.
	before := len(mc.published(BridgeTopic(blid, "rooms")))
	h.svc.notifyBridge(true)
	eventually(t, "rooms republished", func() bool { return len(mc.published(BridgeTopic(blid, "rooms"))) > before })
}

func TestBridgeStateReportsDisconnected(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	h.last().Close()
	h.svc.mu.Lock()
	h.svc.robots[blid].connected = false
	h.svc.mu.Unlock()
	h.svc.notifyBridge(false)
	eventually(t, "state connected=false", func() bool {
		ps := mc.published(BridgeTopic(blid, "state"))
		var st map[string]any
		return len(ps) > 0 && json.Unmarshal(ps[len(ps)-1].payload, &st) == nil && st["connected"] == false
	})
}

func TestBridgeServesCommandAndPublishesResult(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	sendCmd(mc, BridgeCommand{ID: "c1", Cmd: "dock", Origin: "side", TS: time.Now()})
	eventually(t, "result c1", func() bool { return len(results(mc)) == 1 })
	if r := results(mc)[0]; r.ID != "c1" || !r.OK {
		t.Fatalf("result = %+v", r)
	}
	if got := commandsRun(h); len(got) != 1 || got[0] != "dock" {
		t.Fatalf("commands run = %v", got)
	}
	if p := mc.published(BridgeTopic(blid, "result"))[0]; p.retained || p.qos != mqtt.AtLeastOnce {
		t.Errorf("result retained=%v qos=%d, want not retained QoS 1", p.retained, p.qos)
	}

	if err := h.store.learnRegion(regionRecord{BLID: blid, MapID: "m1", MapVersion: "v1", RegionID: "1", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}
	h.last().onChange(roomba.Status{Maps: []roomba.Map{{ID: "m1", Version: "v1"}}})
	sendCmd(mc, BridgeCommand{ID: "c2", Cmd: "clean_regions", PmapID: "m1", MapVersion: "v1",
		Regions: []roomba.Region{{ID: "1", Type: "rid"}}, TS: time.Now()})
	eventually(t, "result c2", func() bool { return len(results(mc)) == 2 })
	if r := results(mc)[1]; !r.OK {
		t.Fatalf("clean_regions result = %+v", r)
	}
	if c := h.last(); c.cleanMap.ID != "m1" || c.cleanMap.Version != "v1" || len(c.cleaned) != 1 {
		t.Fatalf("clean sent map=%+v regions=%v", c.cleanMap, c.cleaned)
	}

	sendCmd(mc, BridgeCommand{ID: "c3", Cmd: "launch-missiles", TS: time.Now()})
	eventually(t, "result c3", func() bool { return len(results(mc)) == 3 })
	if r := results(mc)[2]; r.OK || r.Error == "" {
		t.Fatalf("unknown command result = %+v, want a failure", r)
	}
}

func TestBridgeRefusesStaleCommand(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	sendCmd(mc, BridgeCommand{ID: "old", Cmd: "start", TS: time.Now().Add(-time.Minute)})
	eventually(t, "stale result", func() bool { return len(results(mc)) == 1 })
	if r := results(mc)[0]; r.OK || !strings.Contains(r.Error, "stale") {
		t.Fatalf("result = %+v, want stale failure", r)
	}
	if got := commandsRun(h); len(got) != 0 {
		t.Fatalf("stale command ran: %v", got)
	}
}

func TestBridgeDedupesByID(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	c := BridgeCommand{ID: "dup", Cmd: "find", TS: time.Now()}
	sendCmd(mc, c)
	eventually(t, "first result", func() bool { return len(results(mc)) == 1 })
	sendCmd(mc, c) // QoS 1 redelivery
	time.Sleep(100 * time.Millisecond)
	if n := len(results(mc)); n != 1 {
		t.Fatalf("%d results for a duplicated id, want 1", n)
	}
	if got := commandsRun(h); len(got) != 1 {
		t.Fatalf("duplicate ran twice: %v", got)
	}
}

func TestBridgeIgnoresRetainedReplay(t *testing.T) {
	mc := newRecMQTT()
	// The replay arrives "right after subscribing".
	h := bridgeHarness(t, mc, func() { retainedWindow = time.Minute })
	// Made before the bridge subscribed, yet younger than the stale limit: only
	// the retained-replay rule keeps it from starting the robot.
	sendCmd(mc, BridgeCommand{ID: "replay", Cmd: "start", TS: time.Now().Add(-10 * time.Second)})
	time.Sleep(100 * time.Millisecond)
	if got := commandsRun(h); len(got) != 0 {
		t.Fatalf("retained replay ran: %v", got)
	}
	if n := len(results(mc)); n != 0 {
		t.Fatalf("%d results for a retained replay, want none", n)
	}
	// A command made after the subscription is served normally.
	sendCmd(mc, BridgeCommand{ID: "live", Cmd: "find", TS: time.Now()})
	eventually(t, "live result", func() bool { return len(results(mc)) == 1 })
}

func TestBridgeNeverPublishesPassword(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	h.last().onChange(roomba.Status{Battery: 50, Name: "Edmond"})
	sendCmd(mc, BridgeCommand{ID: "p1", Cmd: "stop", TS: time.Now()})
	eventually(t, "result", func() bool { return len(results(mc)) == 1 })
	eventually(t, "state published", func() bool { return len(mc.published(BridgeTopic(blid, "state"))) > 1 })
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.pubs) < 4 {
		t.Fatalf("only %d publishes, the scan proves nothing", len(mc.pubs))
	}
	for _, p := range mc.pubs {
		if strings.Contains(string(p.payload), fakePassword) {
			t.Fatalf("password found in payload on %s", p.topic)
		}
	}
}

func TestBridgeSingleOwnerGuard(t *testing.T) {
	fastBridge(t)
	guardWait = 2 * time.Second
	mc := newRecMQTT()
	// Same instance name as ours (the default "myhome" everywhere), other process.
	other, _ := json.Marshal(availability{Online: true, TS: time.Now(), Instance: "prod", Owner: "boot-B"})
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() { // the broker delivers its retained message once subscribed
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				mc.Feed(BridgeTopic(blid, "availability"), other)
			}
		}
	}()
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: fakePassword, Bridge: true, MQTT: mc, Instance: "prod", Owner: "boot-A"},
		&robotRecord{BLID: blid, Host: "192.0.2.50"})
	time.Sleep(300 * time.Millisecond)
	if h.last() != nil {
		t.Fatal("connected to the robot although another instance owns it")
	}
	if mc.subscribed(BridgeTopic(blid, "cmd")) || len(mc.published(BridgeTopic(blid, "state"))) > 0 {
		t.Fatal("bridge started although another instance owns the robot")
	}
}

func TestBridgeGuardIgnoresStaleAndOwnAvailability(t *testing.T) {
	for name, a := range map[string]availability{
		"stale other owner": {Online: true, TS: time.Now().Add(-5 * time.Minute), Instance: "other"},
		"offline":           {Online: false, TS: time.Now(), Instance: "other"},
		"own restart":       {Online: true, TS: time.Now(), Instance: "prod", Owner: "boot-A"},
	} {
		t.Run(name, func(t *testing.T) {
			fastBridge(t)
			mc := newRecMQTT()
			p, _ := json.Marshal(a)
			go func() {
				for i := 0; i < 50; i++ {
					mc.Feed(BridgeTopic(blid, "availability"), p)
					time.Sleep(2 * time.Millisecond)
				}
			}()
			h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
				Config{BLID: blid, Password: fakePassword, Bridge: true, MQTT: mc, Instance: "prod", Owner: "boot-A"},
				&robotRecord{BLID: blid, Host: "192.0.2.50"})
			eventually(t, "robot connected", func() bool { return connected(h) })
		})
	}
}

// Without MYHOME_VACUUM_BRIDGE the service must not touch the broker at all.
func TestBridgeOffByDefault(t *testing.T) {
	mc := newRecMQTT()
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: fakePassword, MQTT: mc, Instance: "prod", Owner: "boot-A"},
		&robotRecord{BLID: blid, Host: "192.0.2.50"})
	eventually(t, "robot connected", func() bool { return connected(h) })
	h.last().onChange(roomba.Status{Battery: 10})
	time.Sleep(100 * time.Millisecond)
	if n := mc.count(); n != 0 {
		t.Fatalf("%d publishes with the bridge off", n)
	}
	if mc.subscribed(BridgeTopic(blid, "cmd")) || mc.subscribed(BridgeTopic(blid, "availability")) {
		t.Fatal("subscribed with the bridge off")
	}
}

func TestAvailabilityWill(t *testing.T) {
	w := AvailabilityWill(blid, "prod")
	var a availability
	if err := json.Unmarshal(w.Payload, &a); err != nil || a.Online {
		t.Fatalf("will payload = %s (%v), want online=false", w.Payload, err)
	}
	if w.Topic != "vacuum/"+blid+"/availability" || !w.Retained || w.QoS != mqtt.AtLeastOnce {
		t.Fatalf("will = %+v", w)
	}
}

// A refused owner re-checks and takes over once the rival's heartbeat is stale.
func TestBridgeGuardRetriesAfterRivalGoesStale(t *testing.T) {
	fastBridge(t)
	ownerFresh = 300 * time.Millisecond
	mc := newRecMQTT()
	rival, _ := json.Marshal(availability{Online: true, TS: time.Now(), Instance: "prod", Owner: "boot-B"})
	go func() {
		for !mc.subscribed(BridgeTopic(blid, "availability")) {
			time.Sleep(time.Millisecond)
		}
		for i := 0; i < 10; i++ { // a dead owner: only the retained message, delivered at subscribe
			mc.Feed(BridgeTopic(blid, "availability"), rival)
			time.Sleep(2 * time.Millisecond)
		}
	}()
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: fakePassword, Bridge: true, MQTT: mc, Instance: "prod", Owner: "boot-A"},
		&robotRecord{BLID: blid, Host: "192.0.2.50"})
	time.Sleep(100 * time.Millisecond)
	if h.last() != nil {
		t.Fatal("connected while the rival was fresh")
	}
	eventually(t, "takeover after the rival went stale", func() bool { return connected(h) })
}

func TestBridgeRefusesCleanOnUnknownRegionOrStaleMap(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	if err := h.store.learnRegion(regionRecord{BLID: blid, MapID: "m1", MapVersion: "v1", RegionID: "1", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}
	h.last().onChange(roomba.Status{Maps: []roomba.Map{{ID: "m1", Version: "v1"}}})
	cases := map[string]BridgeCommand{
		"unknown region":  {ID: "u1", Cmd: "clean_regions", PmapID: "m1", MapVersion: "v1", Regions: []roomba.Region{{ID: "9", Type: "rid"}}},
		"unknown map":     {ID: "u2", Cmd: "clean_regions", PmapID: "nope", MapVersion: "v1", Regions: []roomba.Region{{ID: "1", Type: "rid"}}},
		"unknown version": {ID: "u3", Cmd: "clean_regions", PmapID: "m1", MapVersion: "vX", Regions: []roomba.Region{{ID: "1", Type: "rid"}}},
	}
	n := 0
	for name, c := range cases {
		c.TS = time.Now()
		sendCmd(mc, c)
		n++
		eventually(t, name, func() bool { return len(results(mc)) == n })
		if r := results(mc)[n-1]; r.OK {
			t.Fatalf("%s: accepted", name)
		}
	}
	// The robot now reports a changed map: the learned region is stale.
	h.last().onChange(roomba.Status{Maps: []roomba.Map{{ID: "m1", Version: "v2"}}})
	sendCmd(mc, BridgeCommand{ID: "u4", Cmd: "clean_regions", PmapID: "m1", MapVersion: "v1", Regions: []roomba.Region{{ID: "1", Type: "rid"}}, TS: time.Now()})
	eventually(t, "stale map", func() bool { return len(results(mc)) == n+1 })
	if r := results(mc)[n]; r.OK || !strings.Contains(r.Error, "stale") {
		t.Fatalf("changed map accepted: %+v", r)
	}
	if h.last().cleaned != nil {
		t.Fatal("a refused clean reached the robot")
	}
}

func TestBridgeDropsEvac(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	sendCmd(mc, BridgeCommand{ID: "e1", Cmd: "evac", TS: time.Now()})
	eventually(t, "result", func() bool { return len(results(mc)) == 1 })
	if r := results(mc)[0]; r.OK {
		t.Fatalf("evac accepted: %+v", r)
	}
	if got := commandsRun(h); len(got) != 0 {
		t.Fatalf("evac reached the robot: %v", got)
	}
}

func TestBridgeMalformedCommandGetsFailedResult(t *testing.T) {
	mc := newRecMQTT()
	bridgeHarness(t, mc)
	mc.Feed(BridgeTopic(blid, "cmd"), []byte(`{"id":"bad1","cmd":"stop","ts":"not-a-time"}`))
	eventually(t, "result", func() bool { return len(results(mc)) == 1 })
	if r := results(mc)[0]; r.ID != "bad1" || r.OK {
		t.Fatalf("result = %+v", r)
	}
}

func TestBridgeBusyAndExpiredWhileQueued(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	b := h.svc.bridge
	b.runMu.Lock() // a command is "running": the next ones queue behind it
	sendCmd(mc, BridgeCommand{ID: "q1", Cmd: "find", TS: time.Now().Add(-29 * time.Second)})
	sendCmd(mc, BridgeCommand{ID: "q2", Cmd: "find", TS: time.Now()})
	sendCmd(mc, BridgeCommand{ID: "q3", Cmd: "find", TS: time.Now()}) // third: over the bound
	eventually(t, "busy result", func() bool { return len(results(mc)) == 1 })
	if r := results(mc)[0]; r.ID != "q3" || !strings.Contains(r.Error, "busy") {
		t.Fatalf("result = %+v, want busy for q3", r)
	}
	time.Sleep(1500 * time.Millisecond) // q1 ages past 30s while queued
	b.runMu.Unlock()
	eventually(t, "q1 expired, q2 ran", func() bool { return len(results(mc)) == 3 })
	for _, r := range results(mc)[1:] {
		switch r.ID {
		case "q1":
			if r.OK || !strings.Contains(r.Error, "expired") {
				t.Fatalf("q1 = %+v", r)
			}
		case "q2":
			if !r.OK {
				t.Fatalf("q2 = %+v", r)
			}
		}
	}
	if got := commandsRun(h); len(got) != 1 {
		t.Fatalf("commands run = %v, want only q2", got)
	}
}

func TestBridgeDedupeMapIsBounded(t *testing.T) {
	mc := newRecMQTT()
	h := bridgeHarness(t, mc)
	b := h.svc.bridge
	for i := 0; i < dedupeMax+50; i++ {
		_ = b.onCmd("", []byte(`{"id":"x`+strings.Repeat("a", 1)+string(rune('0'+i%10))+time.Duration(i).String()+`","cmd":"nope","ts":"2000-01-01T00:00:00Z"}`), "")
	}
	b.mu.Lock()
	n := len(b.seen)
	b.mu.Unlock()
	if n > dedupeMax {
		t.Fatalf("dedupe map holds %d ids, cap %d", n, dedupeMax)
	}
}
