package script

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// Package logger
var log = hlog.GetLogger("myhome/ctl/shelly/script")

func init() {
	Cmd.AddCommand(updateCtl)
	// Flag to disable minification on update
	updateCtl.Flags().BoolVar(&updateNoMinify, "no-minify", false, "Do not minify scripts before upload")
	// Flag to force re-upload even if version hash matches
	updateCtl.Flags().BoolVar(&updateForce, "force", false, "Force re-upload even if version hash matches")
	updateCtl.Flags().BoolVar(&updateDryRun, "dry-run", false, "Only report what would be uploaded: reads the script list and version markers, changes nothing")
	updateCtl.Flags().BoolVar(&updateIncludePump, "include-pump", false, "Allow uploading pool-pump.js (refused by default); even then every relay must be off, and the replaced code is saved and restored on failure")
	updateCtl.Flags().StringVar(&updateBackupDir, "backup-dir", ".", "Directory where the code replaced on a guarded script (pool-pump.js) is saved")
}

var updateCtl = &cobra.Command{
	Use:   "update DEVICE [SCRIPT]",
	Short: "Update all scripts (or a specific script) loaded on the given Shelly device(s) by re-uploading available versions",
	Long: `Update the scripts loaded on the given Shelly device(s) to the versions this binary carries.

A script is uploaded only when its version marker (KVS key script/<name>, the
sha1 of the packaged source) differs from the packaged version, or is missing,
or the script is not loaded. Every device and script gets one line:
up-to-date, updated (old -> new), would-update (--dry-run), refused, not-packaged
or failed, with the reason.

pool-pump.js is guarded: it is never uploaded unless --include-pump is given,
and then only while every relay of the device is off. Its current code is saved
to --backup-dir first and put back if the upload or the restart fails.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		scriptArgs := []string{}
		if len(args) > 1 {
			scriptArgs = []string{args[1]}
		}
		if updateDryRun {
			fmt.Println("Dry run: nothing will be uploaded, stopped or started.")
		}
		_, err := myhome.Foreach(cmd.Context(), log, device, options.Via, doUpdate, scriptArgs)
		return err
	},
}

var (
	updateNoMinify    bool
	updateForce       bool
	updateDryRun      bool
	updateIncludePump bool
	updateBackupDir   string
)

func doUpdate(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	opts := mhscript.UpdateOptions{
		// Resolve each script: a same-named file in --local-scripts-dir shadows
		// the embedded copy (unless --no-local-scripts is set). This is also
		// the fleet-upload path the .deb postinst runs unattended
		// (`script update '*'`), so the default here MUST stay disabled — see
		// issue #457.
		LocalDir:       localScriptsDirEffective(),
		Minify:         !updateNoMinify,
		Force:          updateForce,
		DryRun:         updateDryRun,
		IncludeGuarded: updateIncludePump,
		BackupDir:      updateBackupDir,
	}
	if len(args) > 0 {
		opts.Target = args[0]
	}

	report, err := mhscript.UpdateDeviceScripts(ctx, log, via, sd, opts)
	if err != nil {
		// Printed by Foreach's summary, with the reason (#863).
		return report, err
	}

	// Devices run in parallel: print each device's block in one write so the
	// lines of two devices never interleave.
	fmt.Print(formatReport(report, opts.Target))

	if failed := report.Failed(); len(failed) > 0 {
		names := make([]string, 0, len(failed))
		for _, f := range failed {
			names = append(names, fmt.Sprintf("%s: %s", f.Script, f.Reason))
		}
		return report, fmt.Errorf("%d script(s) failed: %s", len(failed), strings.Join(names, "; "))
	}
	return report, nil
}

// formatReport renders one device's report: one line per script, each
// prefixed with the device name so the output stays greppable once collected
// in a log file (the postinst case).
func formatReport(r *mhscript.DeviceScriptsReport, target string) string {
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
		case mhscript.OutcomeUpToDate:
			fmt.Fprintf(&b, " (%s)", short(s.NewVersion))
		case mhscript.OutcomeUpdated, mhscript.OutcomeWouldUpdate, mhscript.OutcomeIndeterminate:
			fmt.Fprintf(&b, " (%s -> %s)", short(s.OldVersion), short(s.NewVersion))
		}
		if s.Outcome != mhscript.OutcomeUpToDate || strings.Contains(s.Reason, "did not confirm") {
			if s.Reason != "" {
				fmt.Fprintf(&b, ": %s", s.Reason)
			}
		}
		if s.Source == "local" {
			b.WriteString(" [local source]")
		}
		if s.Backup != "" {
			fmt.Fprintf(&b, " [previous code saved to %s]", s.Backup)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// short abbreviates a sha1 for display; "none" and "unknown" pass through.
func short(v string) string {
	if len(v) == 40 {
		return v[:7]
	}
	if v == "" {
		return "?"
	}
	return v
}
