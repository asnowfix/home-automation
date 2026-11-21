package alert

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time          { return c.t }
func (c *clock) Advance(d time.Duration) { c.t = c.t.Add(d) }

type fakePub struct {
	docs []Alert
	err  error
	hook func()
}

func (p *fakePub) Publish(_ context.Context, a *Alert) error {
	if p.err != nil {
		return p.err
	}
	p.docs = append(p.docs, *a)
	if p.hook != nil {
		p.hook()
	}
	return nil
}

func (p *fakePub) states() (s []State) {
	for _, d := range p.docs {
		s = append(s, d.State)
	}
	return
}

func newDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

var rule = Rule{Name: "homedrive.offline", Source: "homedrive", Severity: Critical, For: 2 * time.Minute, Hold: 5 * time.Minute}

func on(key string) []Observation {
	return []Observation{{Key: key, Active: true, Summary: "homedrive offline: fix@nas", Detail: "No status for a while."}}
}

func off(key string) []Observation {
	return []Observation{{Key: key, Active: false, Summary: "homedrive back online: fix@nas"}}
}

// harness builds an evaluator (a "daemon run") on db; grace 0 unless set.
func start(t *testing.T, db *sqlx.DB, pub Publisher, c *clock, grace time.Duration) *Evaluator {
	t.Helper()
	st, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEvaluator(context.Background(), logr.Discard(), pub, st, WithClock(c.Now), WithGrace(grace))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func eval(t *testing.T, e *Evaluator, obs []Observation) {
	t.Helper()
	if err := e.Eval(context.Background(), rule, obs); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleForAndHold(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, 0)

	eval(t, e, on("k")) // inactive -> pending
	if len(pub.docs) != 0 || len(e.Snapshot()) != 0 {
		t.Fatal("pending must be silent")
	}
	c.Advance(time.Minute)
	eval(t, e, on("k")) // still within `for`
	if len(pub.docs) != 0 {
		t.Fatal("fired before `for`")
	}
	c.Advance(time.Minute)
	eval(t, e, on("k")) // pending -> firing
	if got := pub.states(); len(got) != 1 || got[0] != Firing {
		t.Fatalf("want one firing, got %v", got)
	}
	f := pub.docs[0]
	if f.Since != 1_000_000 || f.Ts != 1_000_120 || f.Ended != 0 {
		t.Fatalf("bad firing doc %+v", f)
	}
	c.Advance(time.Minute)
	eval(t, e, on("k"))  // firing stays silent
	eval(t, e, off("k")) // firing -> resolving
	if len(pub.docs) != 1 || len(e.Snapshot()) != 1 {
		t.Fatal("resolving is still firing and publishes nothing")
	}
	c.Advance(4 * time.Minute)
	eval(t, e, on("k")) // flaps back: same episode, no document
	eval(t, e, off("k"))
	c.Advance(4*time.Minute + 59*time.Second)
	eval(t, e, off("k")) // hold not over
	if len(pub.docs) != 1 {
		t.Fatal("resolved before the hold")
	}
	c.Advance(time.Second)
	eval(t, e, off("k")) // resolving -> inactive
	if got := pub.states(); len(got) != 2 || got[1] != Resolved {
		t.Fatalf("want firing then resolved, got %v", got)
	}
	r := pub.docs[1]
	if r.Since != f.Since || r.Ended != c.t.Unix()-300 || r.Summary != "homedrive back online: fix@nas" {
		t.Fatalf("bad resolved doc %+v", r)
	}
	if len(e.Snapshot()) != 0 {
		t.Fatal("inactive after resolve")
	}
	eval(t, e, off("k"))
	if len(pub.docs) != 2 {
		t.Fatal("no document for an inactive key")
	}
}

func TestPendingClearsSilently(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	db := newDB(t)
	e := start(t, db, pub, c, 0)
	eval(t, e, on("k"))
	c.Advance(time.Minute)
	eval(t, e, off("k"))
	c.Advance(10 * time.Minute)
	eval(t, e, off("k"))
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM alert_state`); err != nil || n != 0 || len(pub.docs) != 0 {
		t.Fatalf("rows=%d docs=%d err=%v", n, len(pub.docs), err)
	}
}

func TestStartupGraceHoldsNewFirings(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, DefaultGrace)
	eval(t, e, append(on("held"), on("gone")...)) // would fire after `for`, but grace is 10 min
	c.Advance(5 * time.Minute)
	eval(t, e, append(on("held"), off("gone")...))
	if len(pub.docs) != 0 {
		t.Fatal("fired during the grace")
	}
	c.Advance(5 * time.Minute)
	eval(t, e, append(on("held"), off("gone")...))
	if len(pub.docs) != 1 || pub.docs[0].Key != "held" || pub.docs[0].Since != 1_000_000 {
		t.Fatalf("want held to fire once at grace end with its own since, got %+v", pub.docs)
	}
}

func TestFrozenKeepsState(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, 0)
	eval(t, e, on("k"))
	c.Advance(time.Hour)
	eval(t, e, []Observation{{Key: "k", Frozen: true}})
	if len(pub.docs) != 0 {
		t.Fatal("a frozen key must not advance")
	}
	eval(t, e, on("k"))
	if len(pub.docs) != 1 {
		t.Fatal("must fire once unfrozen, its for clock never reset")
	}
}

func TestAbsentKeyResolves(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, 0)
	eval(t, e, on("k"))
	c.Advance(2 * time.Minute)
	eval(t, e, on("k"))
	eval(t, e, nil) // key vanished: counts as inactive
	c.Advance(5 * time.Minute)
	eval(t, e, nil)
	if got := pub.states(); len(got) != 2 || got[1] != Resolved || pub.docs[1].Summary != "homedrive offline: fix@nas" {
		t.Fatalf("want resolve with remembered text, got %+v", pub.docs)
	}
}

func TestPublishFailureRetriesNaturally(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{err: context.DeadlineExceeded}
	e := start(t, newDB(t), pub, c, 0)
	eval(t, e, on("k"))
	c.Advance(2 * time.Minute)
	eval(t, e, on("k")) // a transient publish failure is logged, not returned
	if len(e.Snapshot()) != 0 {
		t.Fatal("not firing until published")
	}
	pub.err = nil
	eval(t, e, on("k"))
	if len(pub.docs) != 1 {
		t.Fatal("next evaluation must publish")
	}
}

func TestRestartInPendingKeepsForClock(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	db := newDB(t)
	pub1 := &fakePub{}
	eval(t, start(t, db, pub1, c, 0), on("k")) // pending, then the daemon restarts
	c.Advance(90 * time.Second)

	pub2 := &fakePub{}
	e := start(t, db, pub2, c, 0)
	eval(t, e, on("k"))
	if len(pub2.docs) != 0 {
		t.Fatal("`for` is not over yet")
	}
	c.Advance(30 * time.Second) // 2 min since the pre-restart start, not since the restart
	eval(t, e, on("k"))
	if len(pub2.docs) != 1 || pub2.docs[0].Since != 1_000_000 {
		t.Fatalf("want one firing keeping the original since, got %+v", pub2.docs)
	}
}

func TestRestartInFiringNoDuplicateAndResolves(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	db := newDB(t)
	pub1 := &fakePub{}
	e := start(t, db, pub1, c, 0)
	eval(t, e, on("k"))
	c.Advance(2 * time.Minute)
	eval(t, e, on("k"))
	if len(pub1.docs) != 1 {
		t.Fatal("setup: must be firing")
	}

	c.Advance(time.Minute) // restart; condition still true; grace applies
	pub2 := &fakePub{}
	e = start(t, db, pub2, c, DefaultGrace)
	eval(t, e, on("k"))
	c.Advance(DefaultGrace)
	eval(t, e, on("k"))
	if len(pub2.docs) != 0 || len(e.Snapshot()) != 1 {
		t.Fatalf("no duplicate firing after a restart, got %+v", pub2.docs)
	}

	// The condition clears while another restart happens: resolve once, same episode.
	eval(t, e, off("k"))
	pub3 := &fakePub{}
	e = start(t, db, pub3, c, 0)
	c.Advance(5 * time.Minute)
	eval(t, e, off("k"))
	if got := pub3.states(); len(got) != 1 || got[0] != Resolved || pub3.docs[0].Since != 1_000_000 {
		t.Fatalf("want the resolve of the pre-restart episode, got %+v", pub3.docs)
	}
}

// A test producer publishes through the real publisher and RecordingMockClient; delivery's Decode accepts every document.
func TestProducerThroughMockClient(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	mc := mqtt.NewRecordingMockClient()
	e := start(t, newDB(t), NewMQTTPublisher(mc, "local"), c, 0)

	eval(t, e, []Observation{{Key: "nas-fix", Active: true, Summary: "homedrive offline: fix@nas",
		Detail: "No status since 14:32 (16 min).", Labels: map[string]string{"subject": "homedrive:nas/fix"}}})
	c.Advance(2 * time.Minute)
	eval(t, e, []Observation{{Key: "nas-fix", Active: true}})
	eval(t, e, []Observation{{Key: "nas-fix", Summary: "homedrive back online: fix@nas"}})
	c.Advance(5 * time.Minute)
	eval(t, e, []Observation{{Key: "nas-fix"}})

	got := mc.Published("local/alert/homedrive.offline/nas-fix")
	if len(got) != 2 {
		t.Fatalf("want firing and resolved on the instance topic, got %d", len(got))
	}
	var states []State
	for _, p := range got {
		a, err := Decode(p)
		if err != nil {
			t.Fatalf("published document violates the contract: %v: %s", err, p)
		}
		states = append(states, a.State)
		var raw map[string]any
		_ = json.Unmarshal(p, &raw)
		if raw["v"] != float64(1) || a.Severity != Critical || a.Source != "homedrive" || a.Labels["subject"] != "homedrive:nas/fix" {
			t.Fatalf("unexpected document %s", p)
		}
	}
	if states[0] != Firing || states[1] != Resolved {
		t.Fatalf("got %v", states)
	}
}

func TestPublisherRefusesInvalidDocument(t *testing.T) {
	mc := mqtt.NewRecordingMockClient()
	bad := valid()
	bad.Detail = "unreachable at 192.0.2.9"
	if err := NewMQTTPublisher(mc, "myhome").Publish(context.Background(), bad); err == nil {
		t.Fatal("must validate before publishing")
	}
	if len(mc.Published("myhome/alert/homedrive.offline/nas-fix")) != 0 {
		t.Fatal("nothing may be published")
	}
}
