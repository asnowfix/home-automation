package daemon

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
)

// Rolling 24 h turnover (#731, owner decision on #745).
//
// The device's turnover-today counter is cumulative since the device's own
// reset (sunrise in summer, 00:15 in winter), so it is not a 24 h window. The
// daemon already persists one pool.turnover_today event per pump stop carrying
// that counter, and (since this change) the device's day anchor runtime_ts,
// which changes exactly when the device resets the counters. Differencing
// consecutive events with the same anchor gives the volume of each completed
// run; the first event after a reset contributes its whole value.
//
// Known limits, all of which under-count and none of which invent volume:
//   - a run still in progress is counted only once it stops;
//   - a run straddling the window start is attributed whole to its stop time;
//   - stops missed while the daemon was down are folded into the next stop;
//   - events recorded before runtime_ts existed make the figure unknown.
//
// Unknown is reported as ok=false and rendered "--".

const (
	rollingWindow     = 24 * time.Hour
	rollingLookback   = 30 * 24 * time.Hour // how far back to find the event preceding the window
	rollingEventLimit = 500
)

type turnoverSample struct {
	ts       time.Time
	achieved float64
	anchor   int64 // device day anchor (runtime_ts); 0 = not recorded
}

// parseTurnoverSamples decodes pool.turnover_today events; undecodable ones are
// skipped. The result is sorted oldest first.
func parseTurnoverSamples(evs []events.Event) []turnoverSample {
	out := make([]turnoverSample, 0, len(evs))
	for _, e := range evs {
		if e.Data == nil {
			continue
		}
		var d struct {
			Achieved  *float64 `json:"turnover_achieved"`
			RuntimeTs int64    `json:"runtime_ts"`
		}
		if json.Unmarshal([]byte(*e.Data), &d) != nil || d.Achieved == nil {
			continue
		}
		out = append(out, turnoverSample{
			ts:       time.Unix(int64(e.Ts), 0),
			achieved: *d.Achieved,
			anchor:   d.RuntimeTs,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts.Before(out[j].ts) })
	return out
}

// rollingTurnover sums the volume of the pump runs that stopped in the 24 h
// before now. upSince is when the daemon began listening: with no stop in the
// window, 0 is only a fact if the daemon was up for all of it.
func rollingTurnover(samples []turnoverSample, now, upSince time.Time) (float64, bool) {
	start := now.Add(-rollingWindow)
	var sum float64
	inWindow := 0
	for i, s := range samples {
		if s.ts.Before(start) || s.ts.After(now) {
			continue
		}
		inWindow++
		if i == 0 || s.anchor == 0 || samples[i-1].anchor == 0 {
			return 0, false // no predecessor, or a pre-anchor event: cannot tell a reset
		}
		prev := samples[i-1]
		if s.anchor != prev.anchor {
			sum += s.achieved // the device reset between the two stops
			continue
		}
		d := s.achieved - prev.achieved
		if d < 0 {
			return 0, false // same day but the counter fell (e.g. speed reconfigured): inconsistent
		}
		sum += d
	}
	if inWindow == 0 {
		if upSince.IsZero() || upSince.After(start) {
			return 0, false
		}
		return 0, true
	}
	return math.Round(sum*100) / 100, true
}

// Rolling24h returns the pool volumes filtered in the 24 h before now, from the
// persisted pool.turnover_today events. It never reads the device. A nil
// receiver, a missing events store or any gap in the data yields ok=false.
func (p *PoolNotices) Rolling24h(ctx context.Context, now, upSince time.Time) (float64, bool) {
	if p == nil {
		return 0, false
	}
	load := p.loadTurnoverEvents
	if load == nil {
		if p.events == nil || p.events.Store() == nil {
			return 0, false
		}
		load = func(ctx context.Context) ([]events.Event, error) {
			return p.events.Store().Query(ctx, events.Query{
				DeviceID: p.deviceID, EventType: "pool.turnover_today",
				Since: rollingLookback, Limit: rollingEventLimit,
			})
		}
	}
	evs, err := load(ctx)
	if err != nil {
		return 0, false
	}
	return rollingTurnover(parseTurnoverSamples(evs), now, upSince)
}
