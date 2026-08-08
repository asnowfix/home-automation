package script

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeClock advances by a fixed step every time it is read, so ProbeHeap's
// timing is deterministic and the tests do not sleep.
type fakeClock struct {
	now  time.Time
	step time.Duration
}

func (c *fakeClock) Now() time.Time {
	t := c.now
	c.now = c.now.Add(c.step)
	return t
}

// scriptedSampler replays a fixed sequence of peaks, holding the last value
// once exhausted — the shape of a script whose heap climbs and then flattens.
func scriptedSampler(peaks []uint32) SampleFunc {
	i := 0
	return func(context.Context) (uint32, uint32, uint32, error) {
		p := peaks[len(peaks)-1]
		if i < len(peaks) {
			p = peaks[i]
		}
		i++
		// used/free are derived so the summary has something coherent to report.
		return p / 2, p, 23030 - p, nil
	}
}

func TestProbeHeap_StopsOnceThePeakSettles(t *testing.T) {
	// Peak climbs for four samples then holds. With a 1 s step and a 3 s settle
	// window the probe should stop three samples after the last rise, rather
	// than running the full budget.
	clock := &fakeClock{now: time.Unix(0, 0), step: time.Second}
	opts := HeapProbeOptions{Interval: time.Nanosecond, Max: time.Minute, Settle: 3 * time.Second}

	samples, err := ProbeHeap(context.Background(), opts,
		scriptedSampler([]uint32{100, 200, 300, 400}), clock.Now)
	if err != nil {
		t.Fatalf("ProbeHeap: %v", err)
	}

	// ProbeHeap reads the clock once at start and once per sample, so the 4th
	// sample (the last rise) lands at t=4s; samples at 5s, 6s and 7s then hold
	// at 400, and 7s - 4s = 3s reaches the settle window.
	if len(samples) != 7 {
		t.Fatalf("expected the probe to stop 3s after the last rise (7 samples), got %d: %v", len(samples), samples)
	}

	s := SummarizeHeap(samples, opts.Settle, 0)
	if s.SettledPeak != 400 {
		t.Errorf("SettledPeak = %d, want 400", s.SettledPeak)
	}
	if s.TimeToPeak != 4*time.Second {
		t.Errorf("TimeToPeak = %v, want 4s", s.TimeToPeak)
	}
	if !s.Settled {
		t.Errorf("expected Settled = true, got summary %+v", s)
	}
}

func TestProbeHeap_StopsAtMaxWhenThePeakKeepsClimbing(t *testing.T) {
	// A peak that never stops rising is the dangerous case: the probe must
	// still terminate, and the summary must refuse to call the result settled.
	clock := &fakeClock{now: time.Unix(0, 0), step: time.Second}
	opts := HeapProbeOptions{Interval: time.Nanosecond, Max: 4 * time.Second, Settle: 2 * time.Second}

	n := uint32(0)
	climbing := func(context.Context) (uint32, uint32, uint32, error) {
		n += 100
		return n / 2, n, 23030 - n, nil
	}

	samples, err := ProbeHeap(context.Background(), opts, climbing, clock.Now)
	if err != nil {
		t.Fatalf("ProbeHeap: %v", err)
	}

	s := SummarizeHeap(samples, opts.Settle, 0)
	if s.Settled {
		t.Errorf("a peak still climbing at the deadline must not be reported as settled: %+v", s)
	}
	if got := s.String(); !strings.Contains(got, "lower bound") {
		t.Errorf("an unsettled summary must say the number is a lower bound, got:\n%s", got)
	}
}

func TestProbeHeap_SamplingErrorAborts(t *testing.T) {
	// A partial series would under-report the peak, so an error must surface
	// rather than being smoothed over.
	clock := &fakeClock{now: time.Unix(0, 0), step: time.Second}
	boom := errors.New("rpc timeout")
	calls := 0
	sample := func(context.Context) (uint32, uint32, uint32, error) {
		calls++
		if calls == 3 {
			return 0, 0, 0, boom
		}
		return 50, 100, 200, nil
	}

	samples, err := ProbeHeap(context.Background(), HeapProbeOptions{Interval: time.Nanosecond, Max: time.Minute, Settle: time.Hour}, sample, clock.Now)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the sampling error to propagate, got %v", err)
	}
	if len(samples) != 2 {
		t.Errorf("expected the samples taken before the failure to be returned, got %d", len(samples))
	}
}

func TestSummarizeHeap_RanksJumpsBySize(t *testing.T) {
	samples := []HeapSample{
		{At: 0, Used: 100, Peak: 1000, Free: 22030},
		{At: time.Second, Used: 150, Peak: 1200, Free: 21830},     // +200
		{At: 2 * time.Second, Used: 900, Peak: 6200, Free: 16830}, // +5000, the interesting one
		{At: 3 * time.Second, Used: 400, Peak: 6300, Free: 16730}, // +100
		{At: 4 * time.Second, Used: 400, Peak: 6300, Free: 16730}, // flat
	}

	s := SummarizeHeap(samples, 2*time.Second, 2)

	if len(s.Jumps) != 2 {
		t.Fatalf("expected the top 2 jumps, got %d", len(s.Jumps))
	}
	if s.Jumps[0].Delta != 5000 || s.Jumps[0].At != 2*time.Second {
		t.Errorf("largest jump = +%d at %v, want +5000 at 2s", s.Jumps[0].Delta, s.Jumps[0].At)
	}
	if s.MinFree != 16730 {
		t.Errorf("MinFree = %d, want 16730", s.MinFree)
	}
	// Peak last rose at 3s and the series ends at 4s, which is short of the 2s
	// settle window.
	if s.Settled {
		t.Errorf("expected Settled = false (only 1s of steady peak), got %+v", s)
	}
}

func TestSummarizeHeap_Empty(t *testing.T) {
	s := SummarizeHeap(nil, time.Second, 3)
	if s.Samples != 0 {
		t.Errorf("expected an empty summary, got %+v", s)
	}
	if got := s.String(); got != "no samples" {
		t.Errorf("String() = %q, want %q", got, "no samples")
	}
}

func TestWriteHeapCSV_RoundTripsLabelAndRows(t *testing.T) {
	var b strings.Builder
	err := WriteHeapCSV(&b, "baseline", []HeapSample{
		{At: 0, Used: 1, Peak: 2, Free: 3},
		{At: 1500 * time.Millisecond, Used: 4, Peak: 5, Free: 6},
	})
	if err != nil {
		t.Fatalf("WriteHeapCSV: %v", err)
	}

	want := "label,at_ms,mem_used,mem_peak,mem_free\nbaseline,0,1,2,3\nbaseline,1500,4,5,6\n"
	if b.String() != want {
		t.Errorf("CSV =\n%q\nwant\n%q", b.String(), want)
	}
}
