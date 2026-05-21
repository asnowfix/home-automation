package electricity

import (
	"context"
	"encoding/json"
	"fmt"
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

// FixedWindowPricer treats a fixed daily time window as the cheap period.
// The window can cross midnight (e.g., 23:15–07:15).
type FixedWindowPricer struct {
	cheapStartMin int // minutes since midnight
	cheapEndMin   int // minutes since midnight
}

// NewFixedWindowPricer parses "HH:MM" cheap_start and cheap_end strings.
func NewFixedWindowPricer(cheapStart, cheapEnd string) (*FixedWindowPricer, error) {
	start, err := parseHHMM(cheapStart)
	if err != nil {
		return nil, fmt.Errorf("cheap_start %q: %w", cheapStart, err)
	}
	end, err := parseHHMM(cheapEnd)
	if err != nil {
		return nil, fmt.Errorf("cheap_end %q: %w", cheapEnd, err)
	}
	return &FixedWindowPricer{cheapStartMin: start, cheapEndMin: end}, nil
}

// IsCheapNow returns true if now OR (now + horizonHours) falls within the cheap window.
func (p *FixedWindowPricer) IsCheapNow(ctx context.Context, horizonHours int) bool {
	now := time.Now()
	if p.inWindow(now) {
		return true
	}
	if horizonHours > 0 {
		return p.inWindow(now.Add(time.Duration(horizonHours) * time.Hour))
	}
	return false
}

// UntilEpoch returns when the current cheap/expensive period ends.
func (p *FixedWindowPricer) UntilEpoch(now time.Time) int64 {
	nowMin := now.Hour()*60 + now.Minute()

	// Start of today in local time
	y, m, d := now.Date()
	loc := now.Location()
	todayStart := time.Date(y, m, d, 0, 0, 0, 0, loc)

	if p.inWindow(now) {
		// Currently cheap — find when the cheap period ends
		if p.cheapEndMin > p.cheapStartMin {
			// Normal window (no midnight crossing)
			return todayStart.Add(time.Duration(p.cheapEndMin) * time.Minute).Unix()
		}
		// Midnight-crossing window — end is tomorrow
		if nowMin >= p.cheapStartMin {
			return todayStart.Add(24*time.Hour + time.Duration(p.cheapEndMin)*time.Minute).Unix()
		}
		// Before midnight but after midnight crossing started (in the early-morning part)
		return todayStart.Add(time.Duration(p.cheapEndMin) * time.Minute).Unix()
	}

	// Currently expensive — find when the cheap period starts
	if nowMin < p.cheapStartMin {
		return todayStart.Add(time.Duration(p.cheapStartMin) * time.Minute).Unix()
	}
	// Past today's start — next start is tomorrow
	return todayStart.Add(24*time.Hour + time.Duration(p.cheapStartMin)*time.Minute).Unix()
}

// inWindow checks whether t falls within the cheap window.
func (p *FixedWindowPricer) inWindow(t time.Time) bool {
	min := t.Hour()*60 + t.Minute()
	if p.cheapEndMin < p.cheapStartMin {
		// Midnight-crossing window (e.g., 23:15–07:15)
		return min >= p.cheapStartMin || min < p.cheapEndMin
	}
	return min >= p.cheapStartMin && min < p.cheapEndMin
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
