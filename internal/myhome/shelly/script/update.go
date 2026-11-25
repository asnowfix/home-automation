package script

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// This file is the business logic behind `myhome ctl shelly script update`,
// the command the package postinst runs against every known device
// (`script update '*'`). It reports one outcome per device and script, can
// plan without touching anything (--dry-run), and refuses to restart a
// guarded script (the pool pump) unless asked to. See issue #863.

// Outcome is what `script update` did, or would do, with one script on one
// device.
type Outcome string

const (
	// OutcomeUpToDate: the version marker matches and the script is loaded;
	// no code was sent.
	OutcomeUpToDate Outcome = "up-to-date"
	// OutcomeUpdated: new code was sent and the script was started.
	OutcomeUpdated Outcome = "updated"
	// OutcomeIndeterminate: the code is on the device (or did not need to
	// be sent) but starting the script did not confirm (#428).
	OutcomeIndeterminate Outcome = "indeterminate"
	// OutcomeWouldUpdate: dry run; the code would be sent.
	OutcomeWouldUpdate Outcome = "would-update"
	// OutcomeRefused: the code would be sent, but a guard refused it. Nothing
	// was changed on the device.
	OutcomeRefused Outcome = "refused"
	// OutcomeNotPackaged: the device runs a script this binary does not
	// carry (e.g. a hand-written one). Nothing to update; not a failure.
	OutcomeNotPackaged Outcome = "not-packaged"
	// OutcomeFailed: the update was attempted and did not complete.
	OutcomeFailed Outcome = "failed"
	// OutcomeMarkerRestored: the script had no version marker but its
	// installed code is the packaged one; the marker was recorded and read
	// back, no code was sent (#897).
	OutcomeMarkerRestored Outcome = "marker-restored"
	// OutcomeWouldMark: dry run of OutcomeMarkerRestored.
	OutcomeWouldMark Outcome = "would-mark"
	// OutcomeSkipped: the script has no version marker and its installed code
	// could not be read, so nothing was uploaded or written (#897). Not a
	// failure; the next run tries again.
	OutcomeSkipped Outcome = "skipped"
)

// ScriptUpdate reports the outcome for one script on one device.
type ScriptUpdate struct {
	Script     string  `json:"script"`
	Id         uint32  `json:"id"`
	Outcome    Outcome `json:"outcome"`
	Source     string  `json:"source,omitempty"`      // "embedded" or "local"
	OldVersion string  `json:"old_version,omitempty"` // marker before: sha1, "none" or "unknown"
	NewVersion string  `json:"new_version,omitempty"` // sha1 of the packaged code
	Reason     string  `json:"reason,omitempty"`
	Backup     string  `json:"backup,omitempty"`      // file holding the code that was replaced
	RolledBack bool    `json:"rolled_back,omitempty"` // the replaced code was put back after a failure
}

// DeviceScriptsReport reports every script `script update` considered on one
// device.
type DeviceScriptsReport struct {
	Device  string         `json:"device"`
	Id      string         `json:"id"`
	DryRun  bool           `json:"dry_run,omitempty"`
	Scripts []ScriptUpdate `json:"scripts"`
}

// Failed returns the scripts whose update was attempted and did not complete.
func (r *DeviceScriptsReport) Failed() []ScriptUpdate {
	var failed []ScriptUpdate
	for _, s := range r.Scripts {
		if s.Outcome == OutcomeFailed {
			failed = append(failed, s)
		}
	}
	return failed
}

// UpdateOptions configures UpdateDeviceScripts.
type UpdateOptions struct {
	// Target limits the update to one script name; "" means every loaded one.
	Target string
	// LocalDir is --local-scripts-dir (#457); "" means embedded only.
	LocalDir string
	Minify   bool
	Force    bool
	// DryRun reads the script list and version markers only (plus, for a
	// loaded script without a marker, its code with Script.GetCode, #897):
	// no Script.Stop, PutCode, KVS.Set or Script.Start.
	DryRun bool
	// IncludeGuarded allows uploading a guarded script (pool-pump.js). Even
	// then every relay must be off and the code being replaced is saved first.
	IncludeGuarded bool
	// BackupDir is where the replaced code of a guarded script is saved.
	// "" means the current directory.
	BackupDir string
}

// guardedScripts are scripts whose restart has consequences beyond the
// script: pool-pump.js drives the production pool pump, and a restart that
// fails (out_of_memory, 2026-11-21) leaves the pool unfiltered and every
// Schedule job a no-op. `script update` never uploads them unless asked to.
var guardedScripts = map[string]bool{
	"pool-pump.js": true,
}

// IsGuarded reports whether `script update` refuses to upload name by default.
func IsGuarded(name string) bool { return guardedScripts[name] }

// guardSettleChecks/guardSettleDelay bound how long a guarded script is
// watched after its start before it is declared healthy. An out_of_memory
// during init appears only after the script has started, not as a
// Script.Start error. Package vars so tests can shrink them.
var (
	guardSettleChecks = 3
	guardSettleDelay  = 10 * time.Second
)

// UpdateDeviceScripts updates the scripts loaded on device to the versions
// this binary carries (or that LocalDir provides), and reports one outcome per
// script. It returns an error only when the device's script list cannot be
// read; per-script failures are in the report (see Failed).
func UpdateDeviceScripts(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, opts UpdateOptions) (*DeviceScriptsReport, error) {
	report := &DeviceScriptsReport{Device: device.Name(), Id: device.Id(), DryRun: opts.DryRun}

	loaded, err := pkgscript.ListLoaded(ctx, via, device)
	if err != nil {
		return report, fmt.Errorf("unable to list loaded scripts: %w", err)
	}

	for _, l := range loaded {
		if opts.Target != "" && l.Name != opts.Target {
			continue
		}
		report.Scripts = append(report.Scripts, updateOne(ctx, log, via, device, l, opts))
	}
	return report, nil
}

func updateOne(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, l pkgscript.Status, opts UpdateOptions) ScriptUpdate {
	su := ScriptUpdate{Script: l.Name, Id: l.Id}

	code, source, err := LoadScript(log, opts.LocalDir, l.Name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			su.Outcome = OutcomeNotPackaged
			su.Reason = "this binary does not carry this script"
		} else {
			su.Outcome = OutcomeFailed
			su.Reason = fmt.Sprintf("cannot read the script: %v", err)
		}
		return su
	}
	su.Source = source

	present := true // it is in the list just read
	plan := PlanUpload(ctx, log, via, device, l.Name, code, opts.Force, &present)
	su.OldVersion = plan.OldVersionLabel()
	su.NewVersion = plan.NewVersion
	su.Reason = plan.Reason

	guarded := IsGuarded(l.Name)
	switch {
	case plan.Upload:
	case plan.CatchUp == CatchUpUnreadable:
		// Missing marker and unreadable code (#897): leave the script alone,
		// not even started.
		su.Outcome = OutcomeSkipped
		return su
	case plan.CatchUp == CatchUpIdentical && opts.DryRun:
		su.Outcome = OutcomeWouldMark
		return su
	case plan.CatchUp == CatchUpIdentical:
		// Records the marker (with read-back) and starts the script, a no-op
		// on a running one, exactly like an up-to-date script.
		out, err := ExecuteUpload(ctx, log, via, device, l.Name, code, opts.Minify, plan)
		switch {
		case err != nil:
			su.Outcome = OutcomeFailed
			su.Reason = fmt.Sprintf("recording the marker did not complete: %v", err)
		case out.MarkerErr != nil:
			su.Outcome = OutcomeFailed
			su.Reason = fmt.Sprintf("the installed code is the packaged code, but its version marker %s was not recorded: %v", plan.Key, out.MarkerErr)
		default:
			su.Outcome = OutcomeMarkerRestored
			if out.Status == StatusIndeterminate {
				su.Reason = plan.Reason + "; starting it did not confirm"
			}
		}
		return su
	}
	if !plan.Upload {
		// A guarded script is left entirely alone by default, not even
		// started: a Script.Start on a stopped pump script is a change to
		// the pump.
		if opts.DryRun || (guarded && !opts.IncludeGuarded) {
			su.Outcome = OutcomeUpToDate
			return su
		}
		// Unchanged from before #863: an up-to-date script is still started
		// (a no-op on a running one).
		out, err := ExecuteUpload(ctx, log, via, device, l.Name, code, opts.Minify, plan)
		su.Outcome = OutcomeUpToDate
		if err == nil && out.Status == StatusIndeterminate {
			su.Reason = "up to date; starting it did not confirm"
		}
		return su
	}

	if guarded && !opts.IncludeGuarded {
		su.Outcome = OutcomeRefused
		su.Reason = fmt.Sprintf("%s is guarded and is never uploaded by default (%s); pass --include-pump to allow it", l.Name, plan.Reason)
		return su
	}
	if guarded {
		if err := checkRelaysOff(ctx, via, device); err != nil {
			su.Outcome = OutcomeRefused
			su.Reason = err.Error()
			return su
		}
	}

	if opts.DryRun {
		su.Outcome = OutcomeWouldUpdate
		return su
	}

	var previous string
	if guarded {
		previous, err = downloadCode(ctx, via, device, l.Id)
		if err == nil && previous == "" {
			err = errors.New("the script on the device is empty")
		}
		if err != nil {
			su.Outcome = OutcomeRefused
			su.Reason = fmt.Sprintf("cannot save the current code to roll back to: %v", err)
			return su
		}
		path, err := saveBackup(opts.BackupDir, device, l.Name, previous)
		if err != nil {
			su.Outcome = OutcomeRefused
			su.Reason = fmt.Sprintf("cannot save the current code to roll back to: %v", err)
			return su
		}
		su.Backup = path
	}

	out, err := ExecuteUpload(ctx, log, via, device, l.Name, code, opts.Minify, plan)
	var failure string
	switch {
	case err != nil:
		failure = fmt.Sprintf("upload did not complete: %v", err)
	case guarded:
		failure = checkRunning(ctx, via, device, l.Name)
	}

	if failure != "" {
		su.Outcome = OutcomeFailed
		su.Reason = failure
		if guarded {
			su.RolledBack, su.Reason = rollback(ctx, log, via, device, l.Name, previous, plan.Key, failure)
		}
		return su
	}

	switch {
	case out.MarkerErr != nil:
		// The code is there, but the next push will upload it again.
		su.Outcome = OutcomeFailed
		su.Reason = fmt.Sprintf("code uploaded but its version marker %s was not recorded: %v", plan.Key, out.MarkerErr)
	case out.Status == StatusIndeterminate:
		su.Outcome = OutcomeIndeterminate
		su.Reason = "code uploaded and confirmed; starting it did not confirm (check `script status` rather than re-uploading)"
	default:
		su.Outcome = OutcomeUpdated
		su.Reason = plan.Reason
	}
	if out.Id != 0 {
		su.Id = out.Id
	}
	return su
}

// checkRelaysOff returns nil only when every switch on the device reports its
// output off. A switch whose state cannot be read refuses, except a switch id
// the device answers does not exist, which ends the enumeration (a Pro1 has
// one switch, a Pro3 three).
func checkRelaysOff(ctx context.Context, via types.Channel, device types.Device) error {
	const maxSwitches = 4
	for id := 0; id < maxSwitches; id++ {
		st, err := sswitch.GetStatus(ctx, device, via, id)
		if err != nil {
			var rpcErr *types.RpcError
			if id > 0 && errors.As(err, &rpcErr) && rpcErr.IsNotFound() {
				return nil
			}
			return fmt.Errorf("refusing: cannot read the state of switch:%d: %v", id, err)
		}
		if st.Output {
			return fmt.Errorf("refusing: switch:%d is ON; upload only while every relay is off", id)
		}
	}
	return nil
}

// downloadCode reads a script's whole code with Script.GetCode, following
// `left` across chunks.
func downloadCode(ctx context.Context, via types.Channel, device types.Device, id uint32) (string, error) {
	var b strings.Builder
	var offset uint32
	for i := 0; i < 1000; i++ {
		out, err := device.CallE(ctx, via, string(pkgscript.GetCode), &pkgscript.GetCodeRequest{Id: id, Offset: offset})
		if err != nil {
			return "", err
		}
		res, ok := out.(*pkgscript.GetCodeResponse)
		if !ok {
			return "", fmt.Errorf("unexpected Script.GetCode response %T", out)
		}
		b.WriteString(res.Data)
		offset += uint32(len(res.Data))
		if res.Left == 0 {
			return b.String(), nil
		}
		if len(res.Data) == 0 {
			return "", fmt.Errorf("Script.GetCode returned no data with %d bytes left", res.Left)
		}
	}
	return "", errors.New("Script.GetCode did not finish")
}

// saveBackup writes the code being replaced to dir, named after the device,
// the script and the time, and returns its path.
func saveBackup(dir string, device types.Device, name, code string) (string, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	id := device.Id()
	if id == "" {
		id = device.Name()
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.bak.js", id, strings.TrimSuffix(name, ".js"), time.Now().UTC().Format("20060102T150405Z")))
	if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// checkRunning watches a just-started script for guardSettleChecks rounds and
// returns "" when it is running without errors every time, or why not.
func checkRunning(ctx context.Context, via types.Channel, device types.Device, name string) string {
	for i := 0; i < guardSettleChecks; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Sprintf("could not confirm the script is running: %v", ctx.Err())
			case <-time.After(guardSettleDelay):
			}
		}
		st, err := pkgscript.ScriptStatus(ctx, device, via, name)
		if err != nil {
			// Not conclusive (the device is often unresponsive right after a
			// start, #428): try the next round.
			if i == guardSettleChecks-1 {
				return fmt.Sprintf("could not confirm the script is running: %v", err)
			}
			continue
		}
		if len(st.Errors) > 0 {
			return fmt.Sprintf("script reports errors %v after the upload", st.Errors)
		}
		if !st.Running {
			return "script is not running after the upload"
		}
	}
	return ""
}

// rollback puts the saved code back after a failed update of a guarded
// script, deletes the version marker (the device no longer runs the packaged
// version) and starts it. It returns whether the code was put back, and the
// reason to report.
func rollback(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, name, previous, key, failure string) (bool, string) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), uploadCriticalTimeout)
	defer cancel()

	// The saved code is what the device ran: already in its final form, so it
	// is sent as is (never minified again).
	if _, err := pkgscript.Upload(cctx, via, device, name, []byte(previous), false); err != nil {
		var confErr *pkgscript.PostUploadConfigError
		if !errors.As(err, &confErr) {
			log.Error(err, "Rollback upload failed", "name", name, "device", device.Name())
			return false, fmt.Sprintf("%s; rollback FAILED: %v", failure, err)
		}
	}
	if _, err := kvs.DeleteKey(cctx, log, via, device, key); err != nil {
		log.Error(err, "Unable to delete version marker after rollback", "key", key, "device", device.Name())
	}
	if err := startWithRetry(ctx, log, via, device, name); err != nil {
		return true, fmt.Sprintf("%s; previous code restored, but starting it did not confirm: %v", failure, err)
	}
	if why := checkRunning(ctx, via, device, name); why != "" {
		return true, fmt.Sprintf("%s; previous code restored, but %s", failure, why)
	}
	return true, fmt.Sprintf("%s; previous code restored and running", failure)
}
