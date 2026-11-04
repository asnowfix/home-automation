package script

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	myScript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

var (
	probeInterval time.Duration
	probeDuration time.Duration
	probeSettle   time.Duration
	probeCSVPath  string
	probeLabel    string
	probeTopJumps int
)

func init() {
	probeCtl.Flags().DurationVar(&probeInterval, "interval", 500*time.Millisecond, "delay between samples")
	probeCtl.Flags().DurationVar(&probeDuration, "duration", 2*time.Minute, "hard stop, whether or not the peak has settled")
	probeCtl.Flags().DurationVar(&probeSettle, "settle", 20*time.Second, "how long the peak must hold steady before the probe stops early (0 to always run for --duration)")
	probeCtl.Flags().StringVar(&probeCSVPath, "csv", "", "append the sample series to this CSV file (created with a header if absent)")
	probeCtl.Flags().StringVar(&probeLabel, "label", "", "name for this run in the CSV, e.g. 'baseline' or 'no-forecast' (defaults to the script name)")
	probeCtl.Flags().IntVar(&probeTopJumps, "top-jumps", 5, "how many of the largest peak increases to report (0 for all)")
	Cmd.AddCommand(probeCtl)
}

var probeCtl = &cobra.Command{
	Use:   "probe <device> <script-name>",
	Short: "Sample a script's heap until its peak settles, and report where the peak was reached",
	Long: `Repeatedly calls Script.GetStatus and reports the settled peak.

A single Script.GetStatus reading is misleading: on a Pro1 the peak keeps
climbing for tens of seconds after the script reports itself initialised,
because the post-init work (notably the Open-Meteo fetch and its JSON.parse)
allocates more than init does. Sampling once, too early, records a peak the
script has not reached yet.

This command samples until the peak stops moving, then prints when it was
reached and the largest increases along the way. It says so explicitly when the
peak had NOT settled, so an unfinished run is never mistaken for a measurement.

Use --csv with --label to accumulate several runs in one file and compare two
variants of a script on the same footing:

  myhome ctl shelly script probe mezzanine pool-pump.js --csv heap.csv --label baseline
  myhome ctl shelly script probe mezzanine pool-pump.js --csv heap.csv --label no-forecast

The device is only read from, never modified.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doProbe, []string{args[1]})
		return err
	},
}

func doProbe(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	scriptName := args[0]

	sample := func(ctx context.Context) (uint32, uint32, uint32, error) {
		status, err := script.ScriptStatus(ctx, sd, via, scriptName)
		if err != nil {
			return 0, 0, 0, err
		}
		if !status.Running {
			return 0, 0, 0, fmt.Errorf("script %q is not running on %s", scriptName, sd.Name())
		}
		return status.MemUsed, status.MemPeak, status.MemFree, nil
	}

	opts := myScript.HeapProbeOptions{Interval: probeInterval, Max: probeDuration, Settle: probeSettle}

	fmt.Printf("Probing %s on %s every %s, up to %s (settle %s)...\n",
		scriptName, sd.Name(), probeInterval, probeDuration, probeSettle)

	samples, err := myScript.ProbeHeap(ctx, opts, sample, nil)
	if err != nil {
		// Report what was collected: a truncated series still says something,
		// as long as it is not mistaken for a settled measurement.
		if len(samples) > 0 {
			fmt.Printf("\n%s\n", myScript.SummarizeHeap(samples, probeSettle, probeTopJumps))
		}
		return nil, err
	}

	summary := myScript.SummarizeHeap(samples, probeSettle, probeTopJumps)
	fmt.Printf("\n%s\n", summary)

	if probeCSVPath != "" {
		label := probeLabel
		if label == "" {
			label = scriptName
		}
		if err := appendHeapCSV(probeCSVPath, label, samples); err != nil {
			return nil, err
		}
		fmt.Printf("appended %d samples labelled %q to %s\n", len(samples), label, probeCSVPath)
	}

	return summary, nil
}

// appendHeapCSV writes the series to path, appending to an existing file so
// several runs accumulate for comparison. WriteHeapCSV always emits a header,
// which is what we want for a new file; for an append we skip it by writing to
// a fresh buffer and dropping the first line.
func appendHeapCSV(path, label string, samples []myScript.HeapSample) error {
	_, statErr := os.Stat(path)
	exists := statErr == nil

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	if !exists {
		return myScript.WriteHeapCSV(f, label, samples)
	}

	var buf headerStrippingWriter
	buf.to = f
	return myScript.WriteHeapCSV(&buf, label, samples)
}

// headerStrippingWriter drops everything up to and including the first newline,
// so an appended run does not repeat the CSV header.
type headerStrippingWriter struct {
	to            interface{ Write([]byte) (int, error) }
	headerSkipped bool
}

func (w *headerStrippingWriter) Write(p []byte) (int, error) {
	if w.headerSkipped {
		return w.to.Write(p)
	}
	for i, b := range p {
		if b == '\n' {
			w.headerSkipped = true
			if _, err := w.to.Write(p[i+1:]); err != nil {
				return 0, err
			}
			return len(p), nil
		}
	}
	// Header not complete yet; consume without forwarding.
	return len(p), nil
}
