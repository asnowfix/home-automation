package daemon

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
)

// Rolling 24 h pool figures (#731, owner decision on #745).
//
// The device's turnover-today counter is cumulative since
// the device's own reset (sunrise in summer, 00:15 in winter), so they are not
// 24 h windows. The daemon already persists one pool.turnover_today event per
// pump stop carrying the counter, the configured target and the device's day
// anchor runtime_ts, which changes exactly when the device resets them.
// Differencing consecutive events with the same anchor gives the volume of each
// completed run; the first event after a reset contributes its
// whole value. The target is configuration, not a counter: it is taken from the
// newest event whatever its age.
//
// A stop whose turnover read failed leaves no pool.turnover_today event, and
// the next increment would silently absorb that run. The device's own
// pool.pump_stop events are persisted in the same store, so every stop since the
// baseline event must be followed by a turnover event; an unpaired one makes the
// figures unknown.
//
// Limits (the figure is exact for completed runs seen by the daemon, with these
// exceptions):
//   - a run still in progress counts only once it stops (lower bound), and the
//     daemon reads the device's last 60 s checkpoint at the stop, so a run's
//     last minute can be missing until the next stop of the same device day;
//   - a run straddling the window start is attributed whole to its stop time;
//   - stops missed while the daemon was down leave neither event, so they fold
//     into the next increment: this can over-count by up to one device day;
//   - the device recomputes turnover-today as seconds x current flow / volume, so
//     changing the volume or RPM configuration mid-day moves the whole day's
//     value (a drop is detected as unknown, a rise is counted as volume);
//   - a device script restart that loses its day stamp re-stamps runtime_ts and
//     counts that day's value a second time.
//
// Unknown is reported as nil and rendered "--".

const (
	rollingWindow     = 24 * time.Hour
	rollingLookback   = 30 * 24 * time.Hour // how far back to find the event preceding the window
	rollingEventLimit = 500
	// stopSlack: pool.pump_stop carries the device's clock, the turnover event the
	// daemon's; a turnover event this much before its stop still pairs with it.
	stopSlack = 30 * time.Second
	// stopGrace: a stop younger than this may simply not have been read yet.
	stopGrace = 5 * time.Minute
)

type turnoverSample struct {
	ts       time.Time
	achieved float64
	target   float64
	anchor   int64 // device day anchor (runtime_ts); 0 = not recorded
}

// poolFigures is the rolling answer; nil fields are unknown.
type poolFigures struct {
	Turnover24h *float64
	Target      *float64
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
			Target    float64  `json:"turnover_target"`
			RuntimeTs int64    `json:"runtime_ts"`
		}
		if json.Unmarshal([]byte(*e.Data), &d) != nil || d.Achieved == nil {
			continue
		}
		out = append(out, turnoverSample{
			ts: time.Unix(int64(e.Ts), 0), achieved: *d.Achieved,
			target: d.Target, anchor: d.RuntimeTs,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts.Before(out[j].ts) })
	return out
}

// stopTimes returns the pump_stop timestamps, oldest first.
func stopTimes(evs []events.Event) []time.Time {
	out := make([]time.Time, 0, len(evs))
	for _, e := range evs {
		out = append(out, time.Unix(int64(e.Ts), 0))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// unpairedStop reports whether some stop in (after, now-stopGrace] has no
// turnover sample between it (minus stopSlack) and the next stop.
func unpairedStop(stops []time.Time, samples []turnoverSample, after, now time.Time) bool {
	for i, st := range stops {
		if !st.After(after) || st.After(now.Add(-stopGrace)) {
			continue
		}
		lo := st.Add(-stopSlack)
		hi := now.Add(time.Hour) // unbounded for the last stop
		if i+1 < len(stops) {
			hi = stops[i+1].Add(-stopSlack)
		}
		found := false
		for _, s := range samples {
			if !s.ts.Before(lo) && s.ts.Before(hi) {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// rollingFigures sums the volume and runtime of the pump runs that stopped in
// the 24 h before now. upSince is when the daemon began listening: with no stop
// in the window, 0 is only a fact if the daemon was up for all of it.
func rollingFigures(samples []turnoverSample, stops []time.Time, now, upSince time.Time) poolFigures {
	var f poolFigures
	if n := len(samples); n > 0 {
		t := samples[n-1].target
		f.Target = &t
	}
	start := now.Add(-rollingWindow)
	var vol float64
	first := -1
	for i, s := range samples {
		if s.ts.Before(start) || s.ts.After(now) {
			continue
		}
		if first < 0 {
			first = i
		}
		if i == 0 || s.anchor == 0 || samples[i-1].anchor == 0 {
			return f // no predecessor, or a pre-anchor event: cannot tell a reset
		}
		prev := samples[i-1]
		if s.anchor != prev.anchor {
			vol += s.achieved // the device reset between the two stops
			continue
		}
		dv := s.achieved - prev.achieved
		if dv < 0 {
			return f // same day but a counter fell (e.g. reconfiguration): inconsistent
		}
		vol += dv
	}
	if first < 0 {
		// No turnover event in the window: 0 is a fact only if the daemon heard
		// every stop (it was up) and heard none.
		if upSince.IsZero() || upSince.After(start) || unpairedStop(stops, nil, start, now) {
			return f
		}
		zv := 0.0
		f.Turnover24h = &zv
		return f
	}
	if unpairedStop(stops, samples, samples[first-1].ts, now) {
		return f // a stop whose read failed: its run would be absorbed or lost
	}
	vol = math.Round(vol*100) / 100
	f.Turnover24h = &vol
	return f
}

// Rolling24h returns the rolling 24 h figures from the persisted
// pool.turnover_today and pool.pump_stop events. It never reads the device. A nil
// receiver or any gap in the data yields nil fields.
func (p *PoolNotices) Rolling24h(ctx context.Context, now, upSince time.Time) poolFigures {
	if p == nil {
		return poolFigures{}
	}
	load := p.loadEvents
	if load == nil {
		if p.events == nil || p.events.Store() == nil {
			return poolFigures{}
		}
		load = func(ctx context.Context, eventType string) ([]events.Event, error) {
			return p.events.Store().Query(ctx, events.Query{
				DeviceID: p.deviceID, EventType: eventType, Since: rollingLookback, Limit: rollingEventLimit,
			})
		}
	}
	turn, err := load(ctx, "pool.turnover_today")
	if err != nil {
		p.log.Error(err, "Rolling 24 h pool figures unavailable: cannot read turnover events")
		return poolFigures{}
	}
	stops, err := load(ctx, "pool.pump_stop")
	if err != nil {
		p.log.Error(err, "Rolling 24 h pool figures unavailable: cannot read pump_stop events")
		return poolFigures{}
	}
	return rollingFigures(parseTurnoverSamples(turn), stopTimes(stops), now, upSince)
}
