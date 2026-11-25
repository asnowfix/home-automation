package script

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// This file is the business logic behind `myhome ctl shelly script
// backfill-markers` (#897): record the version marker of scripts that are
// already on a device with exactly the packaged code but were pushed before
// markers existed (or before they were readable, #895), WITHOUT uploading or
// restarting anything. The marker value is always ScriptVersion of the
// packaged source, never a hash of the installed text; the installed text is
// only compared with what the uploader would have sent.

// Outcomes specific to the marker backfill; they share ScriptUpdate with
// `script update` (OutcomeFailed is reused).
const (
	// OutcomeMarked: no marker existed, the installed code matches the
	// packaged code, and the marker was written and read back.
	OutcomeMarked Outcome = "marked"
	// OutcomeWouldMark: dry run of OutcomeMarked; nothing was written.
	OutcomeWouldMark Outcome = "would-mark"
	// OutcomeAlreadyMarked: a marker exists (new or legacy key); left alone.
	OutcomeAlreadyMarked Outcome = "already-marked"
	// OutcomeDiffers: the installed code matches neither form of the
	// packaged code; nothing was written (`script update` will upload it).
	OutcomeDiffers Outcome = "differs"
	// OutcomeSkipped: not examined or not decidable (guarded script, not
	// packaged, marker unreadable, code unreadable); nothing was written.
	OutcomeSkipped Outcome = "skipped"
)

// BackfillOptions configures BackfillDeviceMarkers.
type BackfillOptions struct {
	// Target limits the backfill to one script name; "" means every loaded one.
	Target string
	// LocalDir is --local-scripts-dir (#457); "" means embedded only.
	LocalDir string
	// DryRun issues only Script.List, KVS.Get and Script.GetCode.
	DryRun bool
}

// BackfillDeviceMarkers records the missing version markers of the scripts
// loaded on device whose installed code is byte-for-byte what this binary
// would upload. It returns an error only when the script list cannot be read;
// per-script results are in the report.
func BackfillDeviceMarkers(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, opts BackfillOptions) (*DeviceScriptsReport, error) {
	report := &DeviceScriptsReport{Device: device.Name(), Id: device.Id(), DryRun: opts.DryRun}

	loaded, err := pkgscript.ListLoaded(ctx, via, device)
	if err != nil {
		return report, fmt.Errorf("unable to list loaded scripts: %w", err)
	}
	for _, l := range loaded {
		if opts.Target != "" && l.Name != opts.Target {
			continue
		}
		report.Scripts = append(report.Scripts, backfillOne(ctx, log, via, device, l, opts))
	}
	return report, nil
}

func backfillOne(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, l pkgscript.Status, opts BackfillOptions) ScriptUpdate {
	su := ScriptUpdate{Script: l.Name, Id: l.Id}

	// The in-service pump is never even read.
	if IsGuarded(l.Name) {
		su.Outcome = OutcomeSkipped
		su.Reason = fmt.Sprintf("%s is guarded: not read, no marker written", l.Name)
		return su
	}

	src, source, err := LoadScript(log, opts.LocalDir, l.Name)
	if err != nil {
		su.Outcome = OutcomeSkipped
		if errors.Is(err, fs.ErrNotExist) {
			su.Reason = "this binary does not carry this script"
		} else {
			su.Reason = fmt.Sprintf("cannot read the script: %v", err)
		}
		return su
	}
	su.Source = source
	su.NewVersion = ScriptVersion(src)

	res := ResolveVersionMarker(ctx, log, via, device, l.Name)
	switch res.State {
	case markerRead:
		su.Outcome = OutcomeAlreadyMarked
		su.OldVersion = res.Version
		su.Reason = "a marker exists; left alone"
		if res.FromLegacy {
			su.Reason = fmt.Sprintf("a marker exists under the legacy key %s; left alone (`script update` migrates it)", res.LegacyKey)
		}
		return su
	case markerUnknown:
		su.Outcome = OutcomeSkipped
		su.OldVersion = "unknown"
		su.Reason = "the version marker could not be read; no marker written"
		return su
	}
	su.OldVersion = "none"

	installed, err := downloadCode(ctx, via, device, l.Id)
	if err != nil {
		su.Outcome = OutcomeSkipped
		su.Reason = fmt.Sprintf("could not read the installed code: %v", err)
		return su
	}
	if !matchesPackaged(installed, src) {
		su.Outcome = OutcomeDiffers
		su.Reason = "the installed code is neither the packaged source nor its minified form; `script update` will upload it"
		return su
	}

	if opts.DryRun {
		su.Outcome = OutcomeWouldMark
		return su
	}
	if _, err := kvs.SetKeyValue(ctx, log, via, device, res.Key, su.NewVersion); err != nil {
		su.Outcome = OutcomeFailed
		su.Reason = fmt.Sprintf("cannot write the marker %s: %v", res.Key, err)
		return su
	}
	got, err := kvs.GetValue(ctx, log, via, device, res.Key)
	if err != nil || got == nil || got.Value != su.NewVersion {
		su.Outcome = OutcomeFailed
		su.Reason = fmt.Sprintf("the marker %s does not read back (error: %v)", res.Key, err)
		return su
	}
	su.Outcome = OutcomeMarked
	return su
}

// matchesPackaged reports whether installed is, byte for byte, what the
// uploader sends for src: the source as is (--no-minify) or its prepared
// minified form. A minifier that cannot process src leaves only the first.
func matchesPackaged(installed string, src []byte) bool {
	if installed == string(src) {
		return true
	}
	min, err := pkgscript.PrepareCode(src, true)
	return err == nil && installed == string(min)
}
