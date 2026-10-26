package daemon

import (
	"context"

	"github.com/asnowfix/home-automation/pkg/beem"
)

// beemSolarSource adapts beem.Watcher.PowerCh to the generic SolarSource
// interface, so SolarAggregator doesn't need to know anything about the Beem
// REST API or its polling loop.
type beemSolarSource struct {
	watcher *beem.Watcher
}

// newBeemSolarSource wraps an already-constructed (and started) beem.Watcher
// as a SolarSource.
func newBeemSolarSource(w *beem.Watcher) *beemSolarSource {
	return &beemSolarSource{watcher: w}
}

func (b *beemSolarSource) Name() string { return "beem" }

func (b *beemSolarSource) Subscribe(ctx context.Context) <-chan SolarReading {
	out := make(chan SolarReading, 4)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case sample, ok := <-b.watcher.PowerCh:
				if !ok {
					return
				}
				select {
				case out <- SolarReading{Source: "beem", Watts: sample.SolarW, TS: sample.TS}:
				default: // non-blocking, matches beem.Watcher's own drop-on-full policy
				}
			}
		}
	}()
	return out
}
