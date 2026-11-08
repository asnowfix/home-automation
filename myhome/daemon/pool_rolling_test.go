package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

var rollNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func smp(hoursAgo float64, achieved float64, anchor int64) turnoverSample {
	return turnoverSample{ts: rollNow.Add(-time.Duration(hoursAgo * float64(time.Hour))), achieved: achieved, anchor: anchor}
}

func TestRollingTurnover(t *testing.T) {
	long := rollNow.Add(-72 * time.Hour) // daemon up the whole window
	for _, tc := range []struct {
		name string
		in   []turnoverSample
		up   time.Time
		want float64
		ok   bool
	}{
		{"same day increments", []turnoverSample{smp(30, 1, 1), smp(10, 2.5, 1), smp(5, 3.5, 1)}, long, 2.5, true},
		{"reset: first stop after counts whole", []turnoverSample{smp(30, 4, 1), smp(20, 1.5, 2), smp(5, 3, 2)}, long, 3, true},
		{"reset to a value above the old one is still detected", []turnoverSample{smp(30, 1, 1), smp(20, 2, 2)}, long, 2, true},
		{"old stop outside window ignored", []turnoverSample{smp(40, 3, 1), smp(30, 5, 1)}, long, 0, true},
		{"no stop, daemon up whole window: 0", nil, long, 0, true},
		{"no stop, daemon started recently: unknown", nil, rollNow.Add(-time.Hour), 0, false},
		{"window start has no predecessor: unknown", []turnoverSample{smp(5, 1, 1)}, long, 0, false},
		{"pre-anchor event: unknown", []turnoverSample{smp(30, 1, 0), smp(5, 2, 0)}, long, 0, false},
		{"counter fell within a day: unknown", []turnoverSample{smp(30, 3, 1), smp(5, 2, 1)}, long, 0, false},
	} {
		got, ok := rollingTurnover(tc.in, rollNow, tc.up)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func turnoverEvent(hoursAgo float64, achieved float64, anchor int64) events.Event {
	d := fmt.Sprintf(`{"turnover_achieved":%v,"runtime_ts":%d}`, achieved, anchor)
	return events.Event{Ts: float64(rollNow.Add(-time.Duration(hoursAgo * float64(time.Hour))).Unix()), Data: &d}
}

// pool.snapshot carries the rolling figure, from events only: no device call.
func TestPoolSnapshotRolling24h(t *testing.T) {
	dev := &fakeKVSDevice{id: "pool-1"}
	p := &PoolNotices{log: logr.Discard(), device: dev, deviceID: "pool-1"}
	// Store returns newest first, as events.Storage.Query does.
	p.loadTurnoverEvents = func(context.Context) ([]events.Event, error) {
		return []events.Event{turnoverEvent(5, 3.5, 1), turnoverEvent(10, 2.5, 1), turnoverEvent(30, 1, 1)}, nil
	}
	h := NewPoolRPCHandler(logr.Discard(), p, true).WithSnapshotSource("pool-1", nil)
	h.now = func() time.Time { return rollNow }
	h.startedAt = rollNow.Add(-72 * time.Hour)
	out, err := h.handleSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := out.(*myhome.PoolSnapshotResult)
	if r.Turnover24h == nil || *r.Turnover24h != 2.5 {
		t.Errorf("Turnover24h = %v, want 2.5", r.Turnover24h)
	}
	if n := dev.calls.Load(); n != 0 {
		t.Errorf("handleSnapshot made %d device call(s), want 0", n)
	}

	// Unknown stays nil: no events and a daemon that has not been up for 24 h.
	p.loadTurnoverEvents = func(context.Context) ([]events.Event, error) { return nil, nil }
	h.startedAt = rollNow.Add(-time.Hour)
	out, _ = h.handleSnapshot(context.Background(), nil)
	if r := out.(*myhome.PoolSnapshotResult); r.Turnover24h != nil {
		t.Errorf("Turnover24h = %v, want nil", *r.Turnover24h)
	}
}

// A pump stop persists the device's day anchor, and Rolling24h reads it back
// through the real events store.
func TestRecordTurnoverTodayCarriesAnchor(t *testing.T) {
	store, err := events.NewStorage(logr.Discard(), t.TempDir()+"/events.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := events.NewService(logr.Discard(), store, nil, nil, time.Hour)
	dev := &fakeKVSDevice{id: "pool-1", kvs: map[string]string{
		kvsKeyRuntimeToday: "3600", kvsKeyTurnoverToday: "1.5", kvsKeyTurnover: "5", kvsKeyRuntimeTs: "1794000000"}}
	p := &PoolNotices{log: logr.Discard(), events: svc, device: dev, deviceID: "pool-1"}
	p.recordTurnoverToday(context.Background())

	evs, err := store.Query(context.Background(), events.Query{DeviceID: "pool-1", EventType: "pool.turnover_today"})
	if err != nil || len(evs) != 1 {
		t.Fatalf("events = %v, err = %v", evs, err)
	}
	s := parseTurnoverSamples(evs)
	if len(s) != 1 || s[0].achieved != 1.5 || s[0].anchor != 1794000000 {
		t.Errorf("samples = %+v", s)
	}
}
