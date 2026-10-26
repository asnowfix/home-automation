package script

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Heap probing for Shelly scripts.
//
// A Shelly device runs every script in one shared JS heap of roughly 23 KB, and
// what decides whether a script survives is `mem_peak`, not its file size or
// its steady-state `mem_used`. Peak is also not reached at startup: on
// filtration-hiver (Pro1) it keeps climbing for ~30 s after the script reports
// itself initialised, because the post-init work — the Open-Meteo fetch and its
// JSON.parse above all — allocates far more than init does.
//
// That makes a single `Script.GetStatus` call actively misleading: sample too
// early and you record a peak the script has not reached yet, and conclude a
// change is free when it is not. These helpers sample until the peak stops
// moving, then report when it stopped and where the largest jumps were, so two
// variants of a script can be compared on the same footing.
//
// The sampling loop is deliberately decoupled from RPC: callers pass a
// SampleFunc, so the summarising logic is testable without a device.

// HeapSample is one observation of a script's memory, timed from the start of
// the probe rather than from an absolute clock so runs are comparable.
type HeapSample struct {
	At   time.Duration
	Used uint32
	Peak uint32
	Free uint32
}

// SampleFunc reads one script's memory counters. It maps onto Shelly's
// Script.GetStatus, whose mem_used/mem_peak/mem_free are exactly these three.
type SampleFunc func(ctx context.Context) (used, peak, free uint32, err error)

// HeapProbeOptions bounds a probe. The defaults (see DefaultHeapProbeOptions)
// suit a script that has just been uploaded and restarted.
type HeapProbeOptions struct {
	// Interval between samples.
	Interval time.Duration
	// Max is the hard stop, whether or not the peak has settled.
	Max time.Duration
	// Settle is how long the peak must hold steady before the probe calls it
	// settled and returns early. Zero means "never settle, always run to Max".
	Settle time.Duration
}

// DefaultHeapProbeOptions samples twice a second for up to two minutes and
// treats the peak as settled once it has not moved for 20 s. The settle window
// is deliberately shorter than the ~30 s climb seen on a Pro1 but long enough
// to span the gaps between the task queue's 200 ms ticks and one HTTP fetch.
func DefaultHeapProbeOptions() HeapProbeOptions {
	return HeapProbeOptions{
		Interval: 500 * time.Millisecond,
		Max:      2 * time.Minute,
		Settle:   20 * time.Second,
	}
}

func (o HeapProbeOptions) withDefaults() HeapProbeOptions {
	d := DefaultHeapProbeOptions()
	if o.Interval <= 0 {
		o.Interval = d.Interval
	}
	if o.Max <= 0 {
		o.Max = d.Max
	}
	if o.Settle < 0 {
		o.Settle = 0
	}
	return o
}

// ProbeHeap samples until the peak settles, the budget runs out, or the context
// is cancelled. A sampling error aborts the probe: a half-finished series would
// silently under-report the peak, which is the one mistake this package exists
// to prevent.
//
// now is injectable so tests can drive the clock; pass nil for time.Now.
func ProbeHeap(ctx context.Context, opts HeapProbeOptions, sample SampleFunc, now func() time.Time) ([]HeapSample, error) {
	opts = opts.withDefaults()
	if now == nil {
		now = time.Now
	}

	start := now()
	var (
		samples      []HeapSample
		highest      uint32
		lastRiseAt   time.Duration
		sawAnySample bool
	)

	for {
		used, peak, free, err := sample(ctx)
		if err != nil {
			return samples, fmt.Errorf("sampling script heap after %d sample(s): %w", len(samples), err)
		}

		at := now().Sub(start)
		samples = append(samples, HeapSample{At: at, Used: used, Peak: peak, Free: free})

		if !sawAnySample || peak > highest {
			highest = peak
			lastRiseAt = at
			sawAnySample = true
		}

		if at >= opts.Max {
			return samples, nil
		}
		// Settled: the peak has held steady long enough that further waiting is
		// unlikely to change the verdict.
		if opts.Settle > 0 && at-lastRiseAt >= opts.Settle {
			return samples, nil
		}

		select {
		case <-ctx.Done():
			return samples, ctx.Err()
		case <-time.After(opts.Interval):
		}
	}
}

// HeapJump is one increase of the peak: the moment the script allocated more
// than it ever had before, and by how much.
type HeapJump struct {
	At    time.Duration
	Delta uint32
	Peak  uint32
}

// HeapSummary is the comparable result of a probe. SettledPeak is the number to
// quote when comparing two variants of a script; TimeToPeak says whether the
// cost was paid at init or afterwards, which is what tells you where to look.
type HeapSummary struct {
	Samples     int
	Duration    time.Duration
	SettledPeak uint32
	TimeToPeak  time.Duration
	FinalUsed   uint32
	MinFree     uint32
	// Settled reports whether the peak stopped moving well before the probe
	// ended. When false, the peak may still have been climbing and the number
	// is a lower bound, not a measurement.
	Settled bool
	Jumps   []HeapJump
}

// SummarizeHeap reduces a sample series. topJumps caps how many of the largest
// peak increases are kept (<= 0 keeps all of them).
//
// settleFor should match the probe's Settle so that Settled means the same
// thing in the summary as it did in the loop.
func SummarizeHeap(samples []HeapSample, settleFor time.Duration, topJumps int) HeapSummary {
	if len(samples) == 0 {
		return HeapSummary{}
	}

	s := HeapSummary{
		Samples:  len(samples),
		Duration: samples[len(samples)-1].At,
		MinFree:  samples[0].Free,
	}

	var (
		highest uint32
		jumps   []HeapJump
	)
	for i, sample := range samples {
		if i == 0 || sample.Peak > highest {
			delta := sample.Peak - highest
			if i == 0 {
				delta = sample.Peak
			}
			jumps = append(jumps, HeapJump{At: sample.At, Delta: delta, Peak: sample.Peak})
			highest = sample.Peak
			s.TimeToPeak = sample.At
		}
		if sample.Free < s.MinFree {
			s.MinFree = sample.Free
		}
	}

	s.SettledPeak = highest
	s.FinalUsed = samples[len(samples)-1].Used
	s.Settled = settleFor > 0 && s.Duration-s.TimeToPeak >= settleFor

	sort.SliceStable(jumps, func(i, j int) bool { return jumps[i].Delta > jumps[j].Delta })
	if topJumps > 0 && len(jumps) > topJumps {
		jumps = jumps[:topJumps]
	}
	s.Jumps = jumps

	return s
}

// String renders a summary for the CLI. It states plainly when the peak had not
// settled, because quoting an unsettled peak as a measurement is the failure
// mode this whole package guards against.
func (s HeapSummary) String() string {
	if s.Samples == 0 {
		return "no samples"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "peak      %d bytes\n", s.SettledPeak)
	fmt.Fprintf(&b, "reached   %s after probe start\n", s.TimeToPeak.Round(100*time.Millisecond))
	fmt.Fprintf(&b, "min free  %d bytes\n", s.MinFree)
	fmt.Fprintf(&b, "final use %d bytes\n", s.FinalUsed)
	fmt.Fprintf(&b, "samples   %d over %s\n", s.Samples, s.Duration.Round(100*time.Millisecond))

	if s.Settled {
		fmt.Fprintf(&b, "settled   yes (peak steady for %s before the probe ended)\n",
			(s.Duration - s.TimeToPeak).Round(100*time.Millisecond))
	} else {
		fmt.Fprintf(&b, "settled   NO — the peak was still moving when the probe ended, "+
			"so %d is a lower bound, not a measurement. Re-run with a longer --duration.\n", s.SettledPeak)
	}

	if len(s.Jumps) > 0 {
		b.WriteString("largest peak increases:\n")
		for _, j := range s.Jumps {
			fmt.Fprintf(&b, "  +%-6d at %-8s (peak %d)\n",
				j.Delta, j.At.Round(100*time.Millisecond), j.Peak)
		}
	}

	return b.String()
}

// WriteHeapCSV writes a sample series as CSV so two runs can be diffed or
// plotted. label identifies the variant being measured (for example
// "v0.11.x-baseline" or "no-forecast"), and is repeated on every row so several
// runs can simply be concatenated.
func WriteHeapCSV(w io.Writer, label string, samples []HeapSample) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"label", "at_ms", "mem_used", "mem_peak", "mem_free"}); err != nil {
		return err
	}
	for _, s := range samples {
		row := []string{
			label,
			strconv.FormatInt(s.At.Milliseconds(), 10),
			strconv.FormatUint(uint64(s.Used), 10),
			strconv.FormatUint(uint64(s.Peak), 10),
			strconv.FormatUint(uint64(s.Free), 10),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
