package script

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(backfillCtl)
	backfillCtl.Flags().BoolVar(&backfillDryRun, "dry-run", false, "Only report what would be marked: reads the script list, version markers and installed code, writes nothing")
}

var backfillDryRun bool

var backfillCtl = &cobra.Command{
	Use:   "backfill-markers DEVICE [SCRIPT]",
	Short: "Record the missing version markers of scripts already running the packaged code, without uploading anything",
	Long: `Record the version marker (KVS key script/<name>) of scripts that are already on the
given Shelly device(s) with exactly the code this binary would upload, but have
no marker. Nothing is uploaded, stopped or started.

This is an explicit operator command: it is not run by the package postinst and
'script update' does not call it. Use it once on devices whose scripts were pushed
before markers could be read, so that 'script update' stops re-pushing them.

Each script gets one line:
  marked          no marker, installed code matches the packaged code (as is or
                  minified): the marker was written and read back
  would-mark      same, with --dry-run (nothing written)
  already-marked  a marker exists (new or legacy key); left alone
  differs         installed code matches neither form of the packaged code; nothing
                  written ('script update' will upload it)
  skipped         pool-pump.js (never read), not packaged, marker unreadable, or
                  code could not be read; nothing written
  failed          the marker write or its read-back failed

--dry-run only reads (Script.List, KVS.Get, Script.GetCode). Reading the code of a
large script over MQTT can be slow: give the command a generous --wait.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		scriptArgs := []string{}
		if len(args) > 1 {
			scriptArgs = []string{args[1]}
		}
		if backfillDryRun {
			fmt.Println("Dry run: no marker will be written.")
		}
		_, err := myhome.Foreach(cmd.Context(), log, args[0], options.Via, doBackfill, scriptArgs)
		return err
	},
}

func doBackfill(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	opts := mhscript.BackfillOptions{LocalDir: localScriptsDirEffective(), DryRun: backfillDryRun}
	if len(args) > 0 {
		opts.Target = args[0]
	}

	report, err := mhscript.BackfillDeviceMarkers(ctx, log, via, sd, opts)
	if err != nil {
		return report, err
	}
	fmt.Print(formatBackfillReport(report, opts.Target))

	var failed []string
	for _, s := range report.Scripts {
		if s.Outcome == mhscript.OutcomeFailed {
			failed = append(failed, fmt.Sprintf("%s: %s", s.Script, s.Reason))
		}
	}
	if len(failed) > 0 {
		return report, fmt.Errorf("%d script(s) failed: %s", len(failed), strings.Join(failed, "; "))
	}
	return report, nil
}

// formatBackfillReport renders one device's report, one device-prefixed line
// per script (see formatReport).
func formatBackfillReport(r *mhscript.DeviceScriptsReport, target string) string {
	var b strings.Builder
	if len(r.Scripts) == 0 {
		if target != "" {
			fmt.Fprintf(&b, "%s: %s is not loaded (skipped)\n", r.Device, target)
		} else {
			fmt.Fprintf(&b, "%s: no scripts loaded\n", r.Device)
		}
		return b.String()
	}
	for _, s := range r.Scripts {
		fmt.Fprintf(&b, "%s: %s %s", r.Device, s.Script, s.Outcome)
		switch s.Outcome {
		case mhscript.OutcomeMarked, mhscript.OutcomeWouldMark:
			fmt.Fprintf(&b, " (%s -> %s)", short(s.OldVersion), short(s.NewVersion))
		case mhscript.OutcomeAlreadyMarked:
			fmt.Fprintf(&b, " (%s)", short(s.OldVersion))
		}
		if s.Reason != "" {
			fmt.Fprintf(&b, ": %s", s.Reason)
		}
		if s.Source == "local" {
			b.WriteString(" [local source]")
		}
		b.WriteString("\n")
	}
	return b.String()
}
