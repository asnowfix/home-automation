package daemon

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
)

// SolarAvailableTopic is the retained MQTT topic where the daemon publishes
// the sum of all known solar-energy sources. Shelly device scripts (e.g.
// pool-pump.js) subscribe to it directly and decide for themselves whether to
// act on it — the daemon itself never issues a start/stop RPC based on this
// value, keeping devices daemon-optional (see AGENTS.md "Resilience Rules").
const SolarAvailableTopic = "myhome/energy/solar/available"

// SolarAvailablePayload is published (retained, QoS AtLeastOnce) to
// SolarAvailableTopic every time any registered source reports a reading.
//
// TS uses unix-epoch-seconds (not RFC3339): pool-pump.js parses this
// on-device with mJS, where epoch arithmetic is cheaper/less error-prone than
// ISO-8601 parsing.
//
// Staleness contract: the aggregator only recomputes and republishes when a
// reading arrives (see SolarAggregator doc comment). If every source stops
// reporting — e.g. Beem's REST API becomes unreachable because the internet
// is down — nothing republishes, so the retained topic keeps serving its
// last value indefinitely, with TS growing arbitrarily old. Because MQTT
// delivers a retained message to a new subscriber immediately regardless of
// its age, TS is not decorative: a subscriber (e.g. pool-pump.js in #405)
// MUST compare TS against its own staleness threshold before trusting
// AvailableW, rather than assuming a received message is fresh.
type SolarAvailablePayload struct {
	AvailableW float64            `json:"available_w"`
	TS         int64              `json:"ts"`
	Sources    []SolarSourceDebug `json:"sources,omitempty"`
}

// SolarSourceDebug reports one source's contribution to AvailableW, for
// observability (e.g. debugging via `mosquitto_sub` or the web UI). Not
// consumed by pool-pump.js today — informational only.
type SolarSourceDebug struct {
	Name  string  `json:"name"`
	Watts float64 `json:"watts"`
	Stale bool    `json:"stale"`
}

// SolarAggregator sums the last-known reading from every registered
// SolarSource and republishes the total to SolarAvailableTopic on every
// incoming reading (i.e. on whatever cadence sources report — currently
// Beem's ~60s poll interval). A source whose last reading is older than
// staleAfter is excluded from the sum but doesn't block other sources from
// reporting or being summed.
type SolarAggregator struct {
	log        logr.Logger
	mc         mqttclient.Client
	sources    []SolarSource
	staleAfter time.Duration

	mu   sync.Mutex
	last map[string]SolarReading
}

// NewSolarAggregator builds an aggregator over the given sources. Call Start
// to begin consuming readings and publishing the aggregate.
func NewSolarAggregator(log logr.Logger, mc mqttclient.Client, staleAfter time.Duration, sources ...SolarSource) *SolarAggregator {
	return &SolarAggregator{
		log:        log,
		mc:         mc,
		sources:    sources,
		staleAfter: staleAfter,
		last:       make(map[string]SolarReading),
	}
}

// Start launches one forwarder goroutine per registered source. Every
// goroutine exits when ctx is done (or its source's channel closes on its
// own), so Start never leaks goroutines past ctx's lifetime.
func (a *SolarAggregator) Start(ctx context.Context) {
	for _, src := range a.sources {
		go a.forward(ctx, src)
	}
}

func (a *SolarAggregator) forward(ctx context.Context, src SolarSource) {
	ch := src.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case reading, ok := <-ch:
			if !ok {
				return
			}
			a.record(ctx, reading)
		}
	}
}

// record updates the last-known reading for reading.Source, recomputes the
// sum over all non-stale sources, and publishes the result.
//
// Publish-ordering caveat: the payload is built under a.mu but published
// (network I/O) outside it, so two sources reporting concurrently can race:
// each builds its own payload under the lock, but the two publishes can then
// land in either order, momentarily leaving the retained topic showing the
// older of the two totals. Harmless with a single source (today: just Beem)
// and intentionally not fixed here — holding the mutex across a network
// publish would block every other source's forwarder goroutine for the
// duration of an MQTT round-trip, which is worse. Whoever adds a second
// SolarSource should be aware of this before relying on strict ordering.
func (a *SolarAggregator) record(ctx context.Context, reading SolarReading) {
	a.mu.Lock()
	a.last[reading.Source] = reading
	payload := a.buildPayloadLocked()
	a.mu.Unlock()

	a.publish(ctx, payload)
}

// buildPayloadLocked computes the current aggregate payload from a.last.
// Callers must hold a.mu.
func (a *SolarAggregator) buildPayloadLocked() SolarAvailablePayload {
	now := time.Now()
	var total float64
	sources := make([]SolarSourceDebug, 0, len(a.last))
	for name, reading := range a.last {
		stale := now.Sub(reading.TS) > a.staleAfter
		if !stale {
			total += reading.Watts
		}
		sources = append(sources, SolarSourceDebug{
			Name:  name,
			Watts: reading.Watts,
			Stale: stale,
		})
	}
	// Deterministic ordering: map iteration order is randomized, and a
	// stable Sources order makes published payloads easier to diff/test.
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })

	return SolarAvailablePayload{
		AvailableW: total,
		TS:         now.Unix(),
		Sources:    sources,
	}
}

func (a *SolarAggregator) publish(ctx context.Context, payload SolarAvailablePayload) {
	b, err := json.Marshal(payload)
	if err != nil {
		a.log.Error(err, "Solar aggregator: failed to marshal payload")
		return
	}
	if err := a.mc.Publish(ctx, SolarAvailableTopic, b, mqttclient.AtLeastOnce, true /*retain*/, "solar-aggregator"); err != nil {
		a.log.Error(err, "Solar aggregator: failed to publish", "topic", SolarAvailableTopic)
		return
	}
	a.log.V(1).Info("Solar aggregator: published",
		"topic", SolarAvailableTopic,
		"available_w", payload.AvailableW,
		"sources", len(payload.Sources),
	)
}
