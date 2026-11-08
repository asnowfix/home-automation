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

func ago(h float64) time.Time { return rollNow.Add(-time.Duration(h * float64(time.Hour))) }

func smp(hoursAgo, achieved float64, anchor int64) turnoverSample {
	return turnoverSample{ts: ago(hoursAgo), achieved: achieved, target: 5, anchor: anchor}
}

// stopsAt builds pump_stop times for samples (a stop just before each).
func stopsAt(hoursAgo ...float64) []time.Time {
	var out []time.Time
	for _, h := range hoursAgo {
		out = append(out, ago(h).Add(-2*time.Second))
	}
	return out
}

func vol(f poolFigures) string {
	if f.Turnover24h == nil {
		return "unknown"
	}
	return fmt.Sprint(*f.Turnover24h)
}

func TestRollingFigures(t *testing.T) {
	long := ago(72) // daemon up the whole window
	for _, tc := range []struct {
		name  string
		in    []turnoverSample
		stops []time.Time
		up    time.Time
		want  string
	}{
		{"same day increments", []turnoverSample{smp(30, 1, 1), smp(10, 2.5, 1), smp(5, 3.5, 1)}, stopsAt(30, 10, 5), long, "2.5"},
		{"reset: first stop after counts whole", []turnoverSample{smp(30, 4, 1), smp(20, 1.5, 2), smp(5, 3, 2)}, stopsAt(30, 20, 5), long, "3"},
		{"reset to a value above the old one is still detected", []turnoverSample{smp(30, 1, 1), smp(20, 2, 2)}, stopsAt(30, 20), long, "2"},
		{"old stop outside window ignored", []turnoverSample{smp(40, 3, 1), smp(30, 5, 1)}, stopsAt(40, 30), long, "0"},
		{"no stop, daemon up whole window: 0", nil, nil, long, "0"},
		{"no stop, daemon started recently: unknown", nil, nil, ago(1), "unknown"},
		{"window start has no predecessor: unknown", []turnoverSample{smp(5, 1, 1)}, stopsAt(5), long, "unknown"},
		{"pre-anchor event: unknown", []turnoverSample{smp(30, 1, 0), smp(5, 2, 0)}, stopsAt(30, 5), long, "unknown"},
		{"counter fell within a day: unknown", []turnoverSample{smp(30, 3, 1), smp(5, 2, 1)}, stopsAt(30, 5), long, "unknown"},
		// A stop (at 10 h) whose read failed must not be absorbed by the next increment.
		{"failed read in window: unknown", []turnoverSample{smp(30, 1, 1), smp(5, 3, 1)}, stopsAt(30, 10, 5), long, "unknown"},
		{"failed read before window start: unknown, not over-counted", []turnoverSample{smp(40, 1, 1), smp(5, 3, 1)}, stopsAt(40, 30, 5), long, "unknown"},
		{"no read, stop 1 min ago, daemon up: unknown (no grace in the zero branch)", nil, []time.Time{rollNow.Add(-time.Minute)}, long, "unknown"},
		{"all reads failed, stop in window: never 0", nil, stopsAt(8), long, "unknown"},
		{"stop just now, read pending: not yet unknown", []turnoverSample{smp(30, 1, 1), smp(5, 3, 1)}, append(stopsAt(30, 5), rollNow.Add(-20*time.Second)), long, "2"},
		// An unanchored event among good ones: unknown while in the window, recovers after.
		{"unanchored event in window", []turnoverSample{smp(30, 1, 1), smp(10, 2, 0), smp(5, 3, 1)}, stopsAt(30, 10, 5), long, "unknown"},
		{"unanchored event has left the window", []turnoverSample{smp(40, 2, 0), smp(30, 1, 1), smp(5, 3, 1)}, stopsAt(40, 30, 5), long, "2"},
	} {
		got := rollingFigures(tc.in, tc.stops, rollNow, tc.up)
		if vol(got) != tc.want {
			t.Errorf("%s: volume = %s, want %s", tc.name, vol(got), tc.want)
			continue
		}
	}
}

func TestRollingTargetSurvivesUnknownVolume(t *testing.T) {
	f := rollingFigures([]turnoverSample{smp(5, 1, 1)}, stopsAt(5), rollNow, ago(72))
	if f.Turnover24h != nil || f.Target == nil || *f.Target != 5 {
		t.Errorf("figures = %+v, want unknown volume with target 5", f)
	}
}

// evAt builds a persisted event at hoursAgo before base.
func evAt(base time.Time, kind string, hoursAgo float64, data string) events.Event {
	e := events.Event{Ts: float64(base.Add(-time.Duration(hoursAgo * float64(time.Hour))).Unix()), DeviceID: "pool-roll", Component: "pool", Event: kind, Severity: "notice"}
	if data != "" {
		e.Data = &data
	}
	return e
}

func ev(kind string, hoursAgo float64, data string) events.Event {
	return evAt(rollNow, kind, hoursAgo, data)
}

// Known answer for a realistic day, through Rolling24h and a real events store
// (default query path: DeviceID, EventType, Since, Limit). Counters follow the
// device's own formula, seconds/3600 x flow / volume.
func TestRolling24hKnownAnswerRealStore(t *testing.T) {
	store, err := events.NewStorage(logr.Discard(), t.TempDir()+"/events.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := events.NewService(logr.Discard(), store, nil, nil, time.Hour)
	ctx := context.Background()
	now := time.Now()
	const flow, volume = 20.0, 40.0 // m3/h, m3
	runs := []struct {
		stopAgo float64 // hours before now
		cumSec  int64   // runtime_sec at the stop
		anchor  int64
	}{
		{25, 2 * 3600, 1}, // previous device day, before the window
		{19, 4 * 3600, 1}, // previous device day, 19 h ago: inside the window
		{2, 1 * 3600, 2},  // after the device reset
	}
	for _, r := range runs {
		d := fmt.Sprintf(`{"turnover_achieved":%v,"turnover_target":5,"runtime_sec":%d,"runtime_ts":%d}`,
			float64(r.cumSec)/3600*flow/volume, r.cumSec, r.anchor)
		for _, e := range []events.Event{evAt(now, "pool.turnover_today", r.stopAgo, d), evAt(now, "pool.pump_stop", r.stopAgo+0.001, "")} {
			if err := store.Record(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	foreign := evAt(now, "pool.pump_stop", 3, "") // another device's stop must not leak in
	foreign.DeviceID = "other"
	if err := store.Record(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	p := &PoolNotices{log: logr.Discard(), events: svc, deviceID: "pool-roll"}
	f := p.Rolling24h(ctx, now, now.Add(-72*time.Hour))
	// In the window: the 19 h-ago run (stop at cumulative 4 h, previous 2 h: 2 h
	// = 1.0 volume) and the run after the reset (1 h = 0.5): 1.5 volume.
	if f.Turnover24h == nil || *f.Turnover24h != 1.5 || f.Target == nil || *f.Target != 5 {
		t.Errorf("figures = %+v (volume %s), want 1.5 / target 5", f, vol(f))
	}
}

// Guards against the calendar-day drop returning: pool.snapshot shows the
// figures and the target at 00:01 (no calendar-day
// check), from events only: no device call.
func TestPoolSnapshotRollingAcrossMidnight(t *testing.T) {
	dev := &fakeKVSDevice{id: "pool-1"}
	p := &PoolNotices{log: logr.Discard(), device: dev, deviceID: "pool-1"}
	p.loadEvents = func(_ context.Context, kind string) ([]events.Event, error) {
		if kind == "pool.pump_stop" {
			return []events.Event{ev(kind, 5, ""), ev(kind, 10, ""), ev(kind, 30, "")}, nil
		}
		d := func(a float64, r int64) string {
			return fmt.Sprintf(`{"turnover_achieved":%v,"turnover_target":5,"runtime_sec":%d,"runtime_ts":1}`, a, r)
		}
		return []events.Event{ev(kind, 5, d(3.5, 12600)), ev(kind, 10, d(2.5, 9000)), ev(kind, 30, d(1, 3600))}, nil
	}
	h := NewPoolRPCHandler(logr.Discard(), p, true).WithSnapshotSource("pool-1", nil)
	h.startedAt = ago(72)
	midnight := time.Date(2026, 11, 9, 0, 1, 0, 0, time.UTC)
	h.now = func() time.Time { return midnight }
	// Re-anchor the fixture relative to the injected clock.
	rollShift := midnight.Sub(rollNow)
	orig := p.loadEvents
	p.loadEvents = func(c context.Context, kind string) ([]events.Event, error) {
		evs, err := orig(c, kind)
		for i := range evs {
			evs[i].Ts += rollShift.Seconds()
		}
		return evs, err
	}
	out, err := h.handleSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := out.(*myhome.PoolSnapshotResult)
	if r.Turnover24h == nil || *r.Turnover24h != 2.5 || r.TurnoverTarget == nil || *r.TurnoverTarget != 5 {
		t.Errorf("at 00:01 result = %+v, want 2.5 / target 5", r)
	}
	if n := dev.calls.Load(); n != 0 {
		t.Errorf("handleSnapshot made %d device call(s), want 0", n)
	}
}

// A store failure is logged and yields unknown, not a silent 0.
func TestRolling24hStoreError(t *testing.T) {
	p := &PoolNotices{log: logr.Discard(), loadEvents: func(context.Context, string) ([]events.Event, error) {
		return nil, fmt.Errorf("db closed")
	}}
	if f := p.Rolling24h(context.Background(), rollNow, ago(72)); f.Turnover24h != nil || f.Target != nil {
		t.Errorf("figures = %+v, want unknown", f)
	}
}

// (A distinct device id: the kvs package caches reads per device id.)
// A pump stop persists the device's day anchor, which Rolling24h reads back.
func TestRecordTurnoverTodayCarriesAnchor(t *testing.T) {
	store, err := events.NewStorage(logr.Discard(), t.TempDir()+"/events.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := events.NewService(logr.Discard(), store, nil, nil, time.Hour)
	dev := &fakeKVSDevice{id: "pool-anchor", kvs: map[string]string{
		kvsKeyRuntimeToday: "3600", kvsKeyTurnoverToday: "1.5", kvsKeyTurnover: "5", kvsKeyRuntimeTs: "1794000000"}}
	p := &PoolNotices{log: logr.Discard(), events: svc, device: dev, deviceID: "pool-anchor"}
	p.recordTurnoverToday(context.Background())

	evs, err := store.Query(context.Background(), events.Query{DeviceID: "pool-anchor", EventType: "pool.turnover_today"})
	if err != nil || len(evs) != 1 {
		t.Fatalf("events = %v, err = %v", evs, err)
	}
	s := parseTurnoverSamples(evs)
	if len(s) != 1 || s[0].achieved != 1.5 || s[0].anchor != 1794000000 || s[0].target != 5 {
		t.Errorf("samples = %+v", s)
	}
}
