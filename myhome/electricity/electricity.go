package electricity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
)

// Pricer reports whether electricity is currently cheap.
type Pricer interface {
	// IsCheapNow returns true if electricity is cheap right now or will be cheap
	// within the next horizonHours hours (used for pre-heating decisions).
	IsCheapNow(ctx context.Context, horizonHours int) bool

	// UntilEpoch returns the unix epoch when the current cheap/expensive period ends.
	UntilEpoch(now time.Time) int64
}

// statusPayload is the MQTT payload published to myhome/electricity/status
type statusPayload struct {
	Cheap      bool  `json:"cheap"`
	UntilEpoch int64 `json:"until_epoch"`
}

// windowInterval is a single cheap electricity time window within a day.
type windowInterval struct {
	startMin int // minutes since midnight
	endMin   int // minutes since midnight
}

// inWindow reports whether t falls within this interval.
func (w windowInterval) inWindow(t time.Time) bool {
	min := t.Hour()*60 + t.Minute()
	if w.endMin < w.startMin {
		// Midnight-crossing window (e.g. 23:15–07:15)
		return min >= w.startMin || min < w.endMin
	}
	return min >= w.startMin && min < w.endMin
}

// untilEnd returns the unix epoch when this interval ends, given that t is inside it.
func (w windowInterval) untilEnd(t time.Time) int64 {
	y, m, d := t.Date()
	loc := t.Location()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	min := t.Hour()*60 + t.Minute()
	if w.endMin > w.startMin {
		// Normal (no midnight crossing)
		return today.Add(time.Duration(w.endMin) * time.Minute).Unix()
	}
	// Midnight-crossing: end is tomorrow if we are in the evening segment
	if min >= w.startMin {
		return today.Add(24*time.Hour + time.Duration(w.endMin)*time.Minute).Unix()
	}
	return today.Add(time.Duration(w.endMin) * time.Minute).Unix()
}

// nextStart returns the unix epoch of the soonest future start of this interval.
func (w windowInterval) nextStart(t time.Time) int64 {
	y, m, d := t.Date()
	loc := t.Location()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	min := t.Hour()*60 + t.Minute()
	if min < w.startMin {
		return today.Add(time.Duration(w.startMin) * time.Minute).Unix()
	}
	return today.Add(24*time.Hour + time.Duration(w.startMin)*time.Minute).Unix()
}

// MultiIntervalPricer treats one or more fixed daily time windows as cheap periods.
// Windows may cross midnight; overlapping windows are handled correctly.
type MultiIntervalPricer struct {
	intervals []windowInterval
}

// NewFixedWindowPricer parses a single "HH:MM" start/end pair.
// Kept as a convenience constructor so existing call-sites need no changes.
func NewFixedWindowPricer(cheapStart, cheapEnd string) (*MultiIntervalPricer, error) {
	return NewMultiIntervalPricerFromString(cheapStart + "-" + cheapEnd)
}

// NewMultiIntervalPricerFromString parses the flag/config string:
//
//	"START1-END1[,START2-END2,...]"  where each time is HH:MM
//
// Example: "23:15-07:15,12:00-14:00"
func NewMultiIntervalPricerFromString(s string) (*MultiIntervalPricer, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("cheap-electricity: empty interval string")
	}
	var intervals []windowInterval
	for _, seg := range strings.Split(s, ",") {
		seg = strings.TrimSpace(seg)
		// HH:MM uses ':', so the only '-' is the separator between start and end.
		parts := strings.SplitN(seg, "-", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("cheap-electricity: invalid interval %q (expected HH:MM-HH:MM)", seg)
		}
		start, err := parseHHMM(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("cheap-electricity interval %q start: %w", seg, err)
		}
		end, err := parseHHMM(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("cheap-electricity interval %q end: %w", seg, err)
		}
		intervals = append(intervals, windowInterval{startMin: start, endMin: end})
	}
	return &MultiIntervalPricer{intervals: intervals}, nil
}

// IsCheapNow returns true if now (or now+horizonHours) falls within any cheap window.
func (p *MultiIntervalPricer) IsCheapNow(ctx context.Context, horizonHours int) bool {
	now := time.Now()
	for _, iv := range p.intervals {
		if iv.inWindow(now) {
			return true
		}
	}
	if horizonHours > 0 {
		future := now.Add(time.Duration(horizonHours) * time.Hour)
		for _, iv := range p.intervals {
			if iv.inWindow(future) {
				return true
			}
		}
	}
	return false
}

// UntilEpoch returns when the current period ends.
// If cheap: earliest end among active intervals.
// If expensive: soonest next interval start.
func (p *MultiIntervalPricer) UntilEpoch(now time.Time) int64 {
	var earliest int64
	for _, iv := range p.intervals {
		if iv.inWindow(now) {
			if e := iv.untilEnd(now); earliest == 0 || e < earliest {
				earliest = e
			}
		}
	}
	if earliest != 0 {
		return earliest
	}
	for _, iv := range p.intervals {
		if ns := iv.nextStart(now); earliest == 0 || ns < earliest {
			earliest = ns
		}
	}
	return earliest
}

// parseHHMM converts "HH:MM" to minutes since midnight.
func parseHHMM(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	return h*60 + m, nil
}

// Publisher periodically publishes the electricity status to MQTT.
type Publisher struct {
	log    logr.Logger
	pricer Pricer
	mc     mqtt.Client
}

// NewPublisher creates an electricity status publisher.
func NewPublisher(log logr.Logger, pricer Pricer, mc mqtt.Client) *Publisher {
	return &Publisher{
		log:    log.WithName("electricity.Publisher"),
		pricer: pricer,
		mc:     mc,
	}
}

const topic = "myhome/electricity/status"
const publishInterval = 15 * time.Minute

// Run publishes electricity status immediately and then every 15 minutes.
// Blocks until ctx is cancelled.
func (p *Publisher) Run(ctx context.Context) {
	p.publish(ctx)

	ticker := time.NewTicker(publishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.publish(ctx)
		}
	}
}

func (p *Publisher) publish(ctx context.Context) {
	now := time.Now()
	payload := statusPayload{
		Cheap:      p.pricer.IsCheapNow(ctx, 0),
		UntilEpoch: p.pricer.UntilEpoch(now),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		p.log.Error(err, "Failed to marshal electricity status")
		return
	}

	if err := p.mc.Publish(ctx, topic, data, mqtt.AtLeastOnce, true, "electricity"); err != nil {
		p.log.Error(err, "Failed to publish electricity status")
		return
	}

	p.log.V(1).Info("Published electricity status", "cheap", payload.Cheap, "until_epoch", payload.UntilEpoch)
}
