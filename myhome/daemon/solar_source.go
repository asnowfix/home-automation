package daemon

import (
	"context"
	"time"
)

// SolarReading is a single instantaneous power reading from a solar-energy
// source (e.g. a Beem PnP kit, or some other inverter brand in the future).
type SolarReading struct {
	Source string
	Watts  float64
	TS     time.Time
}

// SolarSource is a generic solar-energy source that SolarAggregator can sum
// over. Implementations adapt a specific vendor watcher/API (e.g.
// pkg/beem.Watcher, via beemSolarSource) to this interface so the aggregator
// never needs to know how many sources exist or where they come from.
type SolarSource interface {
	// Name identifies the source (e.g. "beem"). Used as the key in
	// SolarAggregator's last-reading map and in SolarSourceDebug.Name.
	Name() string

	// Subscribe returns a channel of readings tied to ctx's lifetime: the
	// returned channel is closed once ctx is done (or the underlying source
	// stops on its own).
	Subscribe(ctx context.Context) <-chan SolarReading
}
