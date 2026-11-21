package alert

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// failDB makes every later `op` (UPDATE, INSERT or DELETE) on alert_state fail, like a failing disk:
// the row keeps its previous value.
func failDB(db *sqlx.DB, op string) {
	db.MustExec(`CREATE TRIGGER fail_` + op + ` BEFORE ` + op + ` ON alert_state BEGIN SELECT RAISE(ABORT, 'disk'); END`)
}

func fire(t *testing.T, e *Evaluator, c *clock) {
	t.Helper()
	eval(t, e, on("k"))
	c.Advance(2 * time.Minute)
	eval(t, e, on("k"))
}

// A text that fails Validate is returned to the producer and must never wedge an alert.
func TestInvalidTextsNeverWedgeAnAlert(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, 0)
	ctx := context.Background()

	bad := []Observation{{Key: "k", Active: true, Summary: "Pump error: dry run"}}
	if err := e.Eval(ctx, rule, bad); err == nil {
		t.Fatal("a contract violation must be returned to the producer")
	}
	c.Advance(2 * time.Minute)
	_ = e.Eval(ctx, rule, bad)
	if len(pub.docs) != 0 || len(e.Snapshot()) != 0 {
		t.Fatal("an alert with no valid summary must not be created")
	}

	fire(t, e, c)
	worse := []Observation{{Key: "k", Summary: "see /var/log/x", Detail: "host 192.0.2.9"}}
	if err := e.Eval(ctx, rule, worse); err == nil {
		t.Fatal("invalid texts on an open alert must be returned too")
	}
	c.Advance(5 * time.Minute)
	_ = e.Eval(ctx, rule, worse)
	if got := pub.states(); len(got) != 2 || got[1] != Resolved || pub.docs[1].Summary != "homedrive offline: fix@nas" {
		t.Fatalf("the resolve must go out with the last valid texts, got %+v", pub.docs)
	}

	if err := e.Eval(ctx, Rule{Name: "Bad Rule", Source: "s", Severity: Critical}, on("k")); err == nil {
		t.Fatal("a bad rule name is a contract error")
	}
	if err := e.Eval(ctx, rule, on("Bad Key")); err == nil {
		t.Fatal("a bad key is a contract error")
	}
	if err := e.Eval(ctx, Rule{Name: "r", Severity: "fatal"}, on("k")); err == nil {
		t.Fatal("a bad severity or source is a contract error")
	}
}

// The resolved document went out but the row survived (crash). After the hold, a re-activated
// condition is a NEW episode, never a silent return to the closed one.
func TestResolveCrashWindowStartsNewEpisode(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	db := newDB(t)
	pub1 := &fakePub{}
	e := start(t, db, pub1, c, 0)
	fire(t, e, c)
	eval(t, e, off("k"))
	c.Advance(5 * time.Minute)
	pub1.hook = func() { failDB(db, "DELETE") }
	_ = e.Eval(context.Background(), rule, off("k")) // resolved published, row not deleted
	if got := pub1.states(); len(got) != 2 || got[1] != Resolved {
		t.Fatalf("setup: %v", got)
	}
	db.MustExec(`DROP TRIGGER fail_DELETE`)

	c.Advance(10 * time.Minute) // restart; the condition is back, the stale resolving row is long past its hold
	pub2 := &fakePub{}
	e = start(t, db, pub2, c, 0)
	eval(t, e, on("k"))
	c.Advance(2 * time.Minute)
	eval(t, e, on("k"))
	var firing *Alert
	for i := range pub2.docs {
		if pub2.docs[i].State == Firing {
			firing = &pub2.docs[i]
		}
	}
	if firing == nil || !firing.Supersedes(&pub1.docs[0]) {
		t.Fatalf("want a firing of a newer episode, got %+v", pub2.docs)
	}
}

// After a restart, a restored firing row that its producer cannot observe yet is frozen for the grace.
func TestAbsentKeysFrozenDuringGrace(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	db := newDB(t)
	e := start(t, db, &fakePub{}, c, 0)
	fire(t, e, c)

	pub2 := &fakePub{}
	e = start(t, db, pub2, c, DefaultGrace)
	for i := 0; i < 4; i++ { // 8 min of silence from the producer: more than the hold
		c.Advance(2 * time.Minute)
		eval(t, e, nil)
	}
	if len(pub2.docs) != 0 || len(e.Snapshot()) != 1 {
		t.Fatalf("an unobserved key must not resolve during the grace, got %+v", pub2.docs)
	}
	c.Advance(3 * time.Minute) // grace over, producer observes the condition again
	eval(t, e, on("k"))
	if len(pub2.docs) != 0 {
		t.Fatalf("same episode, no document, got %+v", pub2.docs)
	}
}

// A failed state update after a firing: the restart re-fires with the SAME since, which delivery dedupes.
func TestPersistFailureAfterFiringRefiresSameSince(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	db := newDB(t)
	pub1 := &fakePub{}
	e := start(t, db, pub1, c, 0)
	eval(t, e, on("k"))
	c.Advance(2 * time.Minute)
	pub1.hook = func() { failDB(db, "INSERT") }
	_ = e.Eval(context.Background(), rule, on("k"))
	if len(pub1.docs) != 1 {
		t.Fatal("setup: the firing is published first")
	}
	db.MustExec(`DROP TRIGGER fail_INSERT`)

	pub2 := &fakePub{}
	e = start(t, db, pub2, c, 0)
	eval(t, e, on("k"))
	if len(pub2.docs) != 1 || pub2.docs[0].Since != pub1.docs[0].Since || pub2.docs[0].Supersedes(&pub1.docs[0]) {
		t.Fatalf("want the same episode re-fired, got %+v", pub2.docs)
	}
}

// Publish first, persist second, for the resolve as well as the firing.
func TestPublishThenPersistOrder(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	db := newDB(t)
	pub := &fakePub{}
	seen := map[State]string{}
	pub.hook = func() {
		var st string
		_ = db.Get(&st, `SELECT state FROM alert_state WHERE fingerprint = 'homedrive.offline/k'`)
		seen[pub.docs[len(pub.docs)-1].State] = st
	}
	e := start(t, db, pub, c, 0)
	fire(t, e, c)
	eval(t, e, off("k"))
	c.Advance(5 * time.Minute)
	eval(t, e, off("k"))
	if seen[Firing] != "pending" || seen[Resolved] != "resolving" {
		t.Fatalf("persisted state at publish time: %v", seen)
	}
}

func TestFailedResolvePublishKeepsResolving(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, 0)
	fire(t, e, c)
	eval(t, e, off("k"))
	c.Advance(5 * time.Minute)
	pub.err = context.DeadlineExceeded
	eval(t, e, off("k")) // logged, not returned
	if len(pub.docs) != 1 || len(e.Snapshot()) != 1 {
		t.Fatal("a failed resolve must stay open")
	}
	pub.err = nil
	c.Advance(publishBackoff)
	eval(t, e, off("k"))
	if got := pub.states(); len(got) != 2 || got[1] != Resolved {
		t.Fatalf("the next evaluation must publish the resolve, got %v", got)
	}
}

func TestFrozenFiringRowStaysFiring(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, 0)
	fire(t, e, c)
	c.Advance(time.Hour)
	eval(t, e, []Observation{{Key: "k", Frozen: true}})
	if len(pub.docs) != 1 || len(e.Snapshot()) != 1 {
		t.Fatal("a frozen firing key must neither resolve nor re-fire")
	}
}
