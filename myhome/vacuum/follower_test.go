package vacuum

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/roomba"
	"github.com/go-logr/logr"
)

// newFollower starts a follower Service with no database and no password.
// tweak adjusts the timing variables before the service reads them.
func newFollower(t *testing.T, mc mqtt.Client, tweak ...func()) *Service {
	t.Helper()
	fs, rw, rf := followStale, resultWait, followRefresh
	followRefresh = 10 * time.Millisecond
	for _, f := range tweak {
		f()
	}
	t.Cleanup(func() { followStale, resultWait, followRefresh = fs, rw, rf })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &Service{ctx: ctx, log: logr.Discard(), cfg: Config{Follow: blid, MQTT: mc, Instance: "side"}, robots: map[string]*robot{}}
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	return s
}

func feedJSON(mc *recMQTT, leaf string, v any) {
	p, _ := json.Marshal(v)
	mc.Feed(BridgeTopic(blid, leaf), p)
}

func feedAvail(mc *recMQTT, online bool, ts time.Time) {
	feedJSON(mc, "availability", availability{Online: online, TS: ts, Instance: "prod", Owner: "boot-A"})
}

func feedState(mc *recMQTT, connected bool, ts time.Time, battery int) {
	feedJSON(mc, "state", bridgeState{
		VacuumStatus: myhome.VacuumStatus{Battery: battery, Phase: "charge", Cycle: "none", UpdatedAt: ts,
			Maps: []myhome.VacuumMap{{ID: "m1", Version: "v1"}}},
		Connected: connected, TS: ts,
	})
}

func feedRooms(mc *recMQTT, cloud bool, regions ...myhome.VacuumRegion) {
	feedJSON(mc, "rooms", myhome.VacuumRoomsResult{Regions: regions, CloudNames: cloud})
}

func kitchen() myhome.VacuumRegion {
	return myhome.VacuumRegion{MapID: "m1", MapVersion: "v1", ID: "1", Type: "rid", Name: "Kitchen", Source: myhome.VacuumSourceCloud}
}

func sent(mc *recMQTT) []BridgeCommand {
	var out []BridgeCommand
	for _, p := range mc.published(BridgeTopic(blid, "cmd")) {
		var c BridgeCommand
		_ = json.Unmarshal(p.payload, &c)
		out = append(out, c)
	}
	return out
}

// answer plays the owner: it replies to the next published command with res.
func answer(t *testing.T, mc *recMQTT, res func(BridgeCommand) BridgeResult) {
	t.Helper()
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if cs := sent(mc); len(cs) > 0 {
				feedJSON(mc, "result", res(cs[0]))
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
}

func statusOf(t *testing.T, s *Service) *myhome.VacuumRobot {
	t.Helper()
	v, err := s.Status("")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestFollowerStateKnownThenUnknown(t *testing.T) {
	now := func() time.Time { return time.Now() }
	cases := map[string]func(mc *recMQTT){
		"bridge offline":   func(mc *recMQTT) { feedAvail(mc, false, now()); feedState(mc, true, now(), 55) },
		"robot not linked": func(mc *recMQTT) { feedAvail(mc, true, now()); feedState(mc, false, now(), 55) },
		"state is stale": func(mc *recMQTT) {
			feedAvail(mc, true, now())
			feedState(mc, true, now().Add(-2*time.Minute), 55)
		},
		"no availability yet": func(mc *recMQTT) { feedState(mc, true, now(), 55) },
	}
	for name, feed := range cases {
		t.Run(name, func(t *testing.T) {
			mc := newRecMQTT()
			s := newFollower(t, mc)
			feed(mc)
			if v := statusOf(t, s); v.Status != nil || v.Connected {
				t.Fatalf("state = %+v connected=%v, want unknown", v.Status, v.Connected)
			}
		})
	}

	t.Run("known", func(t *testing.T) {
		mc := newRecMQTT()
		s := newFollower(t, mc)
		feedAvail(mc, true, now())
		feedState(mc, true, now(), 55)
		if v := statusOf(t, s); v.Status == nil || v.Status.Battery != 55 || !v.Connected {
			t.Fatalf("view = %+v, want battery 55", v)
		}
	})

	// Silence ages the state out by itself: no message announces the owner's death.
	t.Run("silence", func(t *testing.T) {
		mc := newRecMQTT()
		s := newFollower(t, mc, func() { followStale = 150 * time.Millisecond })
		feedAvail(mc, true, now())
		feedState(mc, true, now(), 55)
		if v := statusOf(t, s); v.Status == nil {
			t.Fatal("fresh state unknown")
		}
		eventually(t, "state unknown after the heartbeat stopped", func() bool { return statusOf(t, s).Status == nil })
	})
}

func TestFollowerCommandOK(t *testing.T) {
	mc := newRecMQTT()
	s := newFollower(t, mc)
	feedAvail(mc, true, time.Now())
	answer(t, mc, func(c BridgeCommand) BridgeResult { return BridgeResult{ID: c.ID, OK: true} })
	if _, err := s.Command(context.Background(), "", "dock"); err != nil {
		t.Fatal(err)
	}
	cs := sent(mc)
	if len(cs) != 1 || cs[0].Cmd != "dock" || cs[0].Origin != "side" || cs[0].ID == "" || time.Since(cs[0].TS) > 5*time.Second {
		t.Fatalf("commands sent = %+v", cs)
	}
	if p := mc.published(BridgeTopic(blid, "cmd"))[0]; p.retained || p.qos != 1 {
		t.Errorf("cmd retained=%v qos=%d, want not retained QoS 1", p.retained, p.qos)
	}
}

func TestFollowerCommandIgnoresOtherFollowersResult(t *testing.T) {
	mc := newRecMQTT()
	s := newFollower(t, mc, func() { resultWait = 150 * time.Millisecond })
	feedAvail(mc, true, time.Now())
	answer(t, mc, func(c BridgeCommand) BridgeResult { return BridgeResult{ID: "someone-else", OK: true} })
	if _, err := s.Command(context.Background(), "", "dock"); !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("err = %v, want unconfirmed (the result was for another command)", err)
	}
}

func TestFollowerCommandTimeoutIsUnconfirmedAndNeverRetried(t *testing.T) {
	mc := newRecMQTT()
	s := newFollower(t, mc, func() { resultWait = 100 * time.Millisecond })
	feedAvail(mc, true, time.Now())
	_, err := s.Command(context.Background(), "", "start")
	if !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("err = %v, want unconfirmed", err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(sent(mc)); n != 1 {
		t.Fatalf("%d commands published, want exactly 1 (never retried)", n)
	}
}

func TestFollowerCommandFailsFastWhenBridgeOffline(t *testing.T) {
	for name, feed := range map[string]func(mc *recMQTT){
		"announced offline": func(mc *recMQTT) { feedAvail(mc, false, time.Now()) },
		"never heard":       func(mc *recMQTT) {},
		"heartbeat stale":   func(mc *recMQTT) { feedAvail(mc, true, time.Now().Add(-5*time.Minute)) },
	} {
		t.Run(name, func(t *testing.T) {
			mc := newRecMQTT()
			s := newFollower(t, mc)
			feed(mc)
			start := time.Now()
			_, err := s.Command(context.Background(), "", "start")
			if !errors.Is(err, ErrBridgeOffline) || !strings.Contains(err.Error(), "robot bridge offline") {
				t.Fatalf("err = %v, want robot bridge offline", err)
			}
			if time.Since(start) > time.Second || len(sent(mc)) != 0 {
				t.Fatalf("not immediate (%v) or published %d commands", time.Since(start), len(sent(mc)))
			}
		})
	}
}

// busy, expired-while-queued and validation failures from the owner reach the
// RPC caller as errors that say what happened.
func TestFollowerSurfacesOwnerRefusals(t *testing.T) {
	for _, msg := range []string{
		"busy: another command is still waiting for the robot",
		"stale command (expired while queued)",
		"region 9 (rid) is not known on map m1",
	} {
		t.Run(msg, func(t *testing.T) {
			mc := newRecMQTT()
			s := newFollower(t, mc)
			feedAvail(mc, true, time.Now())
			answer(t, mc, func(c BridgeCommand) BridgeResult { return BridgeResult{ID: c.ID, Error: msg} })
			_, err := s.Command(context.Background(), "", "stop")
			if err == nil || !strings.Contains(err.Error(), msg) || errors.Is(err, ErrUnconfirmed) {
				t.Fatalf("err = %v, want the owner's message %q", err, msg)
			}
		})
	}
}

func TestFollowerRoomsFromTopicAndClean(t *testing.T) {
	old := abortWindow
	abortWindow = 50 * time.Millisecond
	t.Cleanup(func() { abortWindow = old })
	mc := newRecMQTT()
	s := newFollower(t, mc)

	if _, err := s.Rooms(""); err == nil {
		t.Fatal("rooms listed before any rooms message")
	}
	feedAvail(mc, true, time.Now())
	feedState(mc, true, time.Now(), 80)
	feedRooms(mc, true, kitchen())

	rooms, err := s.Rooms("")
	if err != nil || len(rooms.Regions) != 1 || rooms.Regions[0].Name != "Kitchen" || !rooms.CloudNames {
		t.Fatalf("rooms = %+v, %v", rooms, err)
	}

	answer(t, mc, func(c BridgeCommand) BridgeResult { return BridgeResult{ID: c.ID, OK: true} })
	if _, err := s.Clean(context.Background(), "", []string{"kitchen"}); err != nil {
		t.Fatal(err)
	}
	cs := sent(mc)
	if len(cs) != 1 || cs[0].Cmd != cmdCleanRegion || cs[0].PmapID != "m1" || cs[0].MapVersion != "v1" ||
		len(cs[0].Regions) != 1 || cs[0].Regions[0].ID != "1" {
		t.Fatalf("clean command = %+v", cs)
	}
}

func TestFollowerCleanRefusedWhileStateUnknown(t *testing.T) {
	mc := newRecMQTT()
	s := newFollower(t, mc)
	feedAvail(mc, true, time.Now())
	feedState(mc, false, time.Now(), 80) // the owner has lost the robot
	feedRooms(mc, false, kitchen())
	if _, err := s.Clean(context.Background(), "", []string{"Kitchen"}); err == nil {
		t.Fatal("room clean sent although the robot's maps are unknown")
	}
	if n := len(sent(mc)); n != 0 {
		t.Fatalf("%d commands published", n)
	}
}

func TestFollowerWritesStayWithTheOwner(t *testing.T) {
	mc := newRecMQTT()
	s := newFollower(t, mc)
	feedRooms(mc, false, kitchen())
	if _, err := s.NameRoom("", "1", "Cuisine"); err == nil || !strings.Contains(err.Error(), "follows robot") {
		t.Fatalf("NameRoom err = %v", err)
	}
	if _, err := s.SyncRooms(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "follows robot") {
		t.Fatalf("SyncRooms err = %v", err)
	}
	if mc.count() != 0 {
		t.Fatalf("a follower published %d messages for a write", mc.count())
	}
}

// A follower never holds, needs or publishes the robot password; it also
// refuses to start with one.
func TestFollowerNeverNeedsThePassword(t *testing.T) {
	mc := newRecMQTT()
	s := newFollower(t, mc) // Config has no Password
	feedAvail(mc, true, time.Now())
	feedState(mc, true, time.Now(), 10)
	answer(t, mc, func(c BridgeCommand) BridgeResult { return BridgeResult{ID: c.ID, OK: true} })
	if _, err := s.Command(context.Background(), "", "find"); err != nil {
		t.Fatal(err)
	}
	if s.robots[blid].password != "" {
		t.Fatal("follower robot carries a password")
	}

	bad := &Service{ctx: context.Background(), log: logr.Discard(), robots: map[string]*robot{},
		cfg: Config{Follow: blid, Password: fakePassword, MQTT: mc}}
	if err := bad.start(); err == nil || strings.Contains(err.Error(), fakePassword) {
		t.Fatalf("start with follow+password: err = %v, want a rejection that does not echo the password", err)
	}
	if err := (&Service{ctx: context.Background(), log: logr.Discard(), robots: map[string]*robot{}, cfg: Config{Follow: blid}}).start(); err == nil {
		t.Fatal("follower started without a broker")
	}
}

// With MYHOME_VACUUM_FOLLOW unset, a daemon that has a broker and a robot
// behaves as before #783: it follows nothing and touches no follower topic.
func TestFollowerOffByDefault(t *testing.T) {
	mc := newRecMQTT()
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: fakePassword, MQTT: mc, Instance: "prod"},
		&robotRecord{BLID: blid, Host: "192.0.2.50"})
	eventually(t, "robot connected", func() bool { return connected(h) })
	h.last().onChange(roomba.Status{Battery: 10})
	feedState(mc, true, time.Now(), 99) // a stray message must change nothing
	time.Sleep(100 * time.Millisecond)
	for _, leaf := range []string{"state", "rooms", "availability", "result", "cmd"} {
		if mc.subscribed(BridgeTopic(blid, leaf)) {
			t.Errorf("subscribed to %s with follower mode and the bridge off", leaf)
		}
	}
	if mc.count() != 0 {
		t.Errorf("%d publishes with follower mode and the bridge off", mc.count())
	}
	if v := statusOf(t, h.svc); v.Status == nil || v.Status.Battery != 10 {
		t.Fatalf("status = %+v, want the robot's own battery 10", v.Status)
	}
	if h.svc.robots[blid].remote != nil {
		t.Fatal("remote robot installed without MYHOME_VACUUM_FOLLOW")
	}
}

// loopMQTT is a broker of one: what is published is delivered to subscribers.
type loopMQTT struct{ *recMQTT }

func (m loopMQTT) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool, name string) error {
	if err := m.recMQTT.Publish(ctx, topic, payload, qos, retained, name); err != nil {
		return err
	}
	go m.Feed(topic, payload)
	return nil
}

// A follower against a real owner bridge: a command runs on the owner's robot,
// and an owner-side validation failure comes back as the follower's error.
func TestFollowerAgainstOwnerBridge(t *testing.T) {
	rec := newRecMQTT()
	mc := loopMQTT{rec}
	follower := newFollower(t, mc)
	fastBridge(t)
	// Two services on one mock broker: the owner is "prod", the follower "side".
	owner := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50", Name: "Edmond"}},
		Config{BLID: blid, Password: fakePassword, Bridge: true, MQTT: mc, Instance: "prod", Owner: "boot-A"},
		&robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"})
	eventually(t, "owner connected", func() bool { return connected(owner) })
	if err := owner.store.learnRegion(regionRecord{BLID: blid, MapID: "m1", MapVersion: "v1", RegionID: "1", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}
	owner.last().onChange(roomba.Status{Battery: 64, Maps: []roomba.Map{{ID: "m1", Version: "v1"}}})
	owner.svc.notifyBridge(true)

	eventually(t, "follower sees the owner's state", func() bool {
		v := statusOf(t, follower)
		return v.Status != nil && v.Status.Battery == 64
	})
	if _, err := follower.Command(context.Background(), "", "dock"); err != nil {
		t.Fatal(err)
	}
	if got := commandsRun(owner); len(got) != 1 || got[0] != "dock" {
		t.Fatalf("owner ran %v, want dock", got)
	}
	// Owner-side validation failure: the region is not one the owner knows.
	eventually(t, "follower has the owner's rooms", func() bool { r, err := follower.Rooms(""); return err == nil && len(r.Regions) == 1 })
	err := follower.robots[blid].remote.CleanRegions(context.Background(), roomba.Map{ID: "m1", Version: "v1"}, []roomba.Region{{ID: "99", Type: "rid"}})
	if err == nil || !strings.Contains(err.Error(), "not known on map m1") {
		t.Fatalf("err = %v, want the owner's validation failure", err)
	}
	// And the password never reached the follower's wire.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, p := range rec.pubs {
		if strings.Contains(string(p.payload), fakePassword) {
			t.Fatalf("password found in payload on %s", p.topic)
		}
	}
}
