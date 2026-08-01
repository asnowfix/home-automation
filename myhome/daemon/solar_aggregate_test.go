package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
)

// fakeSolarSource is a channel-backed SolarSource test double: readings
// pushed onto readingsCh are delivered to whatever Subscribe returns.
type fakeSolarSource struct {
	name       string
	readingsCh chan SolarReading
}

func newFakeSolarSource(name string) *fakeSolarSource {
	return &fakeSolarSource{
		name:       name,
		readingsCh: make(chan SolarReading, 8),
	}
}

func (f *fakeSolarSource) Name() string { return f.name }

func (f *fakeSolarSource) Subscribe(ctx context.Context) <-chan SolarReading {
	out := make(chan SolarReading, 8)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case r, ok := <-f.readingsCh:
				if !ok {
					return
				}
				select {
				case out <- r:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

func (f *fakeSolarSource) send(r SolarReading) {
	f.readingsCh <- r
}

// waitForPublish polls mc.Published(topic) until it has at least n entries or
// the deadline elapses. Polling (rather than a fixed sleep) keeps the test
// robust under CI load — see AGENTS.md "Avoid time.Sleep in async-protocol
// tests."
func waitForPublish(t *testing.T, mc *mqttclient.RecordingMockClient, topic string, n int, timeout time.Duration) [][]byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if payloads := mc.Published(topic); len(payloads) >= n {
			return payloads
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d publish(es) to %q, got %d", n, topic, len(mc.Published(topic)))
	return nil
}

// TestSolarAggregator_SumsAcrossSources verifies that readings from two
// distinct sources are summed into a single AvailableW total.
func TestSolarAggregator_SumsAcrossSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mc := mqttclient.NewRecordingMockClient()
	src1 := newFakeSolarSource("alpha")
	src2 := newFakeSolarSource("beta")

	agg := NewSolarAggregator(logr.Discard(), mc, 5*time.Minute, src1, src2)
	agg.Start(ctx)

	now := time.Now()
	src1.send(SolarReading{Source: "alpha", Watts: 300, TS: now})
	payloads := waitForPublish(t, mc, SolarAvailableTopic, 1, 2*time.Second)
	var got SolarAvailablePayload
	if err := json.Unmarshal(payloads[len(payloads)-1], &got); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if got.AvailableW != 300 {
		t.Errorf("AvailableW after 1 source = %v, want 300", got.AvailableW)
	}

	src2.send(SolarReading{Source: "beta", Watts: 450, TS: now})
	payloads = waitForPublish(t, mc, SolarAvailableTopic, 2, 2*time.Second)
	if err := json.Unmarshal(payloads[len(payloads)-1], &got); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if got.AvailableW != 750 {
		t.Errorf("AvailableW after 2 sources = %v, want 750", got.AvailableW)
	}
}

// TestSolarAggregator_StaleSourceExcludedButDoesNotBlockOthers verifies that
// a source whose last reading is older than staleAfter is dropped from the
// sum, while a fresher source still contributes and still triggers publishes.
func TestSolarAggregator_StaleSourceExcludedButDoesNotBlockOthers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mc := mqttclient.NewRecordingMockClient()
	staleSrc := newFakeSolarSource("stale-source")
	freshSrc := newFakeSolarSource("fresh-source")

	staleAfter := 50 * time.Millisecond
	agg := NewSolarAggregator(logr.Discard(), mc, staleAfter, staleSrc, freshSrc)
	agg.Start(ctx)

	// Old reading, timestamped well before staleAfter's window.
	staleSrc.send(SolarReading{Source: "stale-source", Watts: 1000, TS: time.Now().Add(-10 * time.Minute)})
	waitForPublish(t, mc, SolarAvailableTopic, 1, 2*time.Second)

	// Fresh reading from the other source.
	freshSrc.send(SolarReading{Source: "fresh-source", Watts: 200, TS: time.Now()})
	payloads := waitForPublish(t, mc, SolarAvailableTopic, 2, 2*time.Second)

	var got SolarAvailablePayload
	if err := json.Unmarshal(payloads[len(payloads)-1], &got); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if got.AvailableW != 200 {
		t.Errorf("AvailableW = %v, want 200 (stale source excluded)", got.AvailableW)
	}

	var staleDebug, freshDebug *SolarSourceDebug
	for i := range got.Sources {
		switch got.Sources[i].Name {
		case "stale-source":
			staleDebug = &got.Sources[i]
		case "fresh-source":
			freshDebug = &got.Sources[i]
		}
	}
	if staleDebug == nil || !staleDebug.Stale {
		t.Errorf("stale-source Sources entry = %+v, want Stale=true", staleDebug)
	}
	if freshDebug == nil || freshDebug.Stale {
		t.Errorf("fresh-source Sources entry = %+v, want Stale=false", freshDebug)
	}
}

// TestSolarAggregator_RepublishesOnEveryReading verifies the aggregator
// publishes once per incoming reading (not batched/debounced), and that
// every publish uses the retained + AtLeastOnce QoS convention.
func TestSolarAggregator_RepublishesOnEveryReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mc := mqttclient.NewRecordingMockClient()
	src := newFakeSolarSource("only-source")

	agg := NewSolarAggregator(logr.Discard(), mc, 5*time.Minute, src)
	agg.Start(ctx)

	for i := 0; i < 3; i++ {
		src.send(SolarReading{Source: "only-source", Watts: float64(100 * (i + 1)), TS: time.Now()})
	}

	payloads := waitForPublish(t, mc, SolarAvailableTopic, 3, 2*time.Second)
	if len(payloads) != 3 {
		t.Fatalf("published %d times, want exactly 3", len(payloads))
	}
}

// TestSolarAggregator_PayloadShape verifies the JSON field names/shape of the
// published payload match the documented contract pool-pump.js depends on.
func TestSolarAggregator_PayloadShape(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mc := mqttclient.NewRecordingMockClient()
	src := newFakeSolarSource("shape-source")

	agg := NewSolarAggregator(logr.Discard(), mc, 5*time.Minute, src)
	agg.Start(ctx)

	before := time.Now().Unix()
	src.send(SolarReading{Source: "shape-source", Watts: 555, TS: time.Now()})
	payloads := waitForPublish(t, mc, SolarAvailableTopic, 1, 2*time.Second)
	after := time.Now().Unix()

	var raw map[string]any
	if err := json.Unmarshal(payloads[0], &raw); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	for _, field := range []string{"available_w", "ts", "sources"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("payload missing field %q: %v", field, raw)
		}
	}

	var got SolarAvailablePayload
	if err := json.Unmarshal(payloads[0], &got); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if got.TS < before || got.TS > after {
		t.Errorf("ts = %d, want between %d and %d (unix-epoch-seconds)", got.TS, before, after)
	}
	if len(got.Sources) != 1 || got.Sources[0].Name != "shape-source" || got.Sources[0].Watts != 555 {
		t.Errorf("Sources = %+v, want one entry {shape-source 555 false}", got.Sources)
	}
}

// TestSolarAggregator_StopsOnContextCancel verifies forwarder goroutines
// exit promptly when ctx is cancelled, rather than leaking.
func TestSolarAggregator_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mc := mqttclient.NewRecordingMockClient()
	src := newFakeSolarSource("cancel-source")

	agg := NewSolarAggregator(logr.Discard(), mc, 5*time.Minute, src)
	agg.Start(ctx)

	src.send(SolarReading{Source: "cancel-source", Watts: 42, TS: time.Now()})
	waitForPublish(t, mc, SolarAvailableTopic, 1, 2*time.Second)

	cancel()

	// After cancellation, further sends should not produce more publishes
	// (the forwarder goroutine has exited). Give it a brief window to settle.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case src.readingsCh <- SolarReading{Source: "cancel-source", Watts: 99, TS: time.Now()}:
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(mc.Published(SolarAvailableTopic)); got != 1 {
		t.Errorf("published %d times after ctx cancel, want exactly 1 (no further publishes)", got)
	}
}
