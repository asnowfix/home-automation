package alert

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// countingPub fails every publish and counts the attempts.
type countingPub struct{ n atomic.Int32 }

func (p *countingPub) Publish(context.Context, *Alert) error {
	p.n.Add(1)
	return errors.New("broker down")
}

func TestPublishFailureBacksOffAcrossKeysAndRules(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &countingPub{}
	e := start(t, newDB(t), pub, c, 0)
	obs := append(on("a"), append(on("b"), on("c")...)...)
	eval(t, e, obs)
	c.Advance(3 * time.Minute) // past For: every key wants to fire
	eval(t, e, obs)
	if got := pub.n.Load(); got != 1 {
		t.Fatalf("publish attempts = %d, want 1 (one timeout for the whole tick)", got)
	}
	eval(t, e, obs) // still inside the back-off
	if got := pub.n.Load(); got != 1 {
		t.Fatalf("publish attempts during back-off = %d, want 1", got)
	}
	c.Advance(publishBackoff)
	eval(t, e, obs)
	if got := pub.n.Load(); got != 2 {
		t.Fatalf("publish attempts after back-off = %d, want 2", got)
	}
}

// blockingPub blocks every publish until released.
type blockingPub struct {
	entered chan struct{}
	release chan struct{}
}

func (p *blockingPub) Publish(context.Context, *Alert) error {
	p.entered <- struct{}{}
	<-p.release
	return nil
}

// A publish stalled on a slow broker must not block Snapshot: the homedrive cache calls it under its own
// lock (Prune's pinned predicate), so a blocked Snapshot would stall the status ingestion.
func TestSnapshotNotBlockedByStalledPublish(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &blockingPub{entered: make(chan struct{}, 1), release: make(chan struct{})}
	e := start(t, newDB(t), pub, c, 0)
	eval(t, e, on("k"))
	c.Advance(3 * time.Minute)
	done := make(chan struct{})
	go func() { eval(t, e, on("k")); close(done) }()
	select {
	case <-pub.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("publish never started")
	}
	snap := make(chan struct{})
	go func() { _ = e.Snapshot(); close(snap) }()
	select {
	case <-snap:
	case <-time.After(2 * time.Second):
		t.Fatal("Snapshot blocked behind a stalled publish")
	}
	close(pub.release)
	<-done
	if len(e.Snapshot()) != 1 {
		t.Fatal("alert must be open after the publish completes")
	}
}

func TestEvalStopsOnCancelledContext(t *testing.T) {
	c := &clock{time.Unix(1_000_000, 0)}
	pub := &fakePub{}
	e := start(t, newDB(t), pub, c, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.Eval(ctx, rule, on("k")); err != nil {
		t.Fatal(err)
	}
	if len(pub.docs) != 0 || len(e.rows) != 0 {
		t.Fatal("a cancelled context must not advance any key")
	}
}
