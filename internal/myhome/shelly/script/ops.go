package script

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
)

// UploadNamedScript reads an embedded script file by name and uploads it with version tracking.
// This is the primary entry point for uploading scripts from the CLI commands.
// It reads the script from the embedded filesystem, uploads it with version tracking,
// and starts the script on the device.
func UploadNamedScript(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, scriptName string, minify bool, force bool) (uint32, error) {
	// Read the embedded script file
	buf, err := script.ReadEmbeddedFile(scriptName)
	if err != nil {
		return 0, fmt.Errorf("failed to read script %s: %w", scriptName, err)
	}

	// Upload with version tracking
	id, err := UploadWithVersion(ctx, log, via, device, scriptName, buf, minify, force)
	if err != nil {
		return 0, fmt.Errorf("failed to upload %s to %s: %w", scriptName, device.Name(), err)
	}

	return id, nil
}

// UploadStatus classifies the outcome of UploadWithVersionDetailed for
// reporting and exit-code purposes — see issue #428.
type UploadStatus int

const (
	// StatusUploaded means the script's code is on the device and the
	// post-upload confirmation (enabling it, starting it) succeeded.
	StatusUploaded UploadStatus = iota
	// StatusIndeterminate means the script's code is confirmed on the
	// device (every PutCode chunk was acknowledged), but a follow-up
	// confirmation step (Script.SetConfig or Script.Start) did not
	// complete — commonly because the device is still busy right after
	// the upload. This is NOT an upload failure: re-uploading is
	// unnecessary and wastes the operator's time. The caller should
	// re-check status rather than re-upload.
	StatusIndeterminate
	// StatusFailed means the code transfer itself did not complete: the
	// device does not reliably have the new script.
	StatusFailed
)

func (s UploadStatus) String() string {
	switch s {
	case StatusUploaded:
		return "uploaded"
	case StatusIndeterminate:
		return "indeterminate"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// classifyUpload turns the two possible failure points of an upload
// attempt — the chunked code transfer itself (chunkErr) and the best-effort
// post-upload confirmation (confirmErr, from enabling/starting the script)
// — into a status and an operator-facing message.
//
// It deliberately does not inspect what kind of error confirmErr is
// (context.DeadlineExceeded, a transport error, anything else): once
// chunkErr is nil the code is verified present on the device, so ANY
// confirmErr at that point is a confirmation gap, not evidence the upload
// failed. Only a non-nil chunkErr means the upload itself did not complete.
//
// Kept as a pure function, like shouldUpload above, so the decision is
// directly unit-testable without a device or a live MQTT broker.
func classifyUpload(chunkErr, confirmErr error) (UploadStatus, string) {
	if chunkErr != nil {
		return StatusFailed, fmt.Sprintf("upload failed: %v", chunkErr)
	}
	if confirmErr != nil {
		return StatusIndeterminate, fmt.Sprintf(
			"script code was uploaded and confirmed on the device, but could not confirm it started/enabled: %v (re-run the status check rather than re-uploading)",
			confirmErr,
		)
	}
	return StatusUploaded, "script uploaded and confirmed running"
}

// startAttempts/startRetryDelay bound the retry of the post-upload
// confirmation step (Script.Start). The device is often still busy for a
// few seconds right after PutCode/SetConfig — persisting the script and/or
// running its init — and does not answer RPCs in that window; see issue
// #428. A short bounded retry turns most of these into confirmed successes
// instead of an indeterminate result.
//
// Package-level vars, not consts, so tests can shrink the delay to keep the
// suite fast; production code never changes them. Any test that does must
// restore the original value in t.Cleanup.
var (
	startAttempts   = 3
	startRetryDelay = 2 * time.Second
)

// retryBounded calls op up to attempts times, waiting delay between
// attempts (subject to ctx cancellation), and returns nil on the first
// success or the last error once attempts are exhausted. op receives the
// 1-based attempt number for logging.
//
// Kept side-effect-free apart from calling op and waiting, so the
// attempt-count/backoff behavior is unit-testable without a device.
func retryBounded(ctx context.Context, attempts int, delay time.Duration, op func(attempt int) error) error {
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		err = op(attempt)
		if err == nil {
			return nil
		}
		if attempt < attempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return err
}

// startWithRetry attempts to start the script up to startAttempts times,
// pausing startRetryDelay between attempts (bounded, cancellable via ctx).
// It returns the last error, or nil on the first success.
func startWithRetry(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, name string) error {
	return retryBounded(ctx, startAttempts, startRetryDelay, func(attempt int) error {
		_, err := script.StartStopDelete(ctx, via, device, name, script.Start)
		if err != nil {
			log.Info("Script.Start did not confirm, will retry if attempts remain",
				"name", name, "device", device.Name(), "attempt", attempt, "maxAttempts", startAttempts, "error", err.Error())
		}
		return err
	})
}

// UploadWithVersion uploads a script and tracks its version in KVS.
// This is MyHome-specific business logic that combines script upload with
// version tracking. It keeps the original two-value contract for existing
// callers that only need a pass/fail signal: a post-upload confirmation gap
// (code confirmed delivered, but enabling/starting it did not confirm) is
// intentionally NOT surfaced as an error here, since re-uploading in
// response would be wasted work — see issue #428. Callers that want to
// report the distinction (e.g. the `script upload` CLI command) should call
// UploadWithVersionDetailed instead.
func UploadWithVersion(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, name string, code []byte, minify bool, force bool) (uint32, error) {
	id, status, err := UploadWithVersionDetailed(ctx, log, via, device, name, code, minify, force)
	if err != nil {
		return 0, err
	}
	if status == StatusIndeterminate {
		log.Info("Upload confirmation incomplete, but code delivery was confirmed; not reporting as an error", "name", name, "device", device.Name())
	}
	return id, nil
}

// UploadWithVersionDetailed does the work of UploadWithVersion but also
// reports an UploadStatus, letting the caller distinguish a fully confirmed
// upload from one where the code was delivered but a follow-up
// confirmation step (enabling/starting the script) did not respond. See
// issue #428.
func UploadWithVersionDetailed(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, name string, code []byte, minify bool, force bool) (uint32, UploadStatus, error) {
	// Calculate version hash
	h := sha1.New()
	h.Write(code)
	version := hex.EncodeToString(h.Sum(nil))

	// Use basename to get just the filename without any directory path
	basename := filepath.Base(name)
	kvsKey := fmt.Sprintf("script/%s", basename)

	// Read the script version from the KVS
	kvsVersion := ""
	res, err := kvs.GetValue(ctx, log, via, device, kvsKey)
	if err != nil || res == nil {
		log.Info("Unable to get KVS entry for script version (continuing)", "key", kvsKey)
		// Don't fail the upload if KVS fails, just log the error
	} else {
		kvsVersion = res.Value
		log.Info("Got KVS entry for script version", "key", kvsKey, "version", kvsVersion)
	}

	// The version marker records what was last uploaded, but it can outlive the
	// script itself: deleting a script from the device's web UI, with a raw
	// Script.Delete RPC, or by factory reset leaves the marker behind (only
	// DeleteWithVersion below removes it). Trusting the marker alone then skips
	// the upload and starts a script that is absent or empty, while reporting
	// success — see #449. So confirm the script is really on the device before
	// letting a matching version suppress the upload.
	present := scriptIsLoaded(ctx, log, via, device, basename)

	// Upload if forced, the version changed, or the device does not actually
	// have the script.
	var id uint32
	upload, reason := shouldUpload(force, version, kvsVersion, present)
	if upload {
		log.Info(reason, "name", name, "version", version, "key", kvsKey, "device", device.Name())

		// Upload the script using the generic pkg/shelly/script package
		id, err = script.Upload(ctx, via, device, name, code, minify)
		if err != nil {
			// A *script.PostUploadConfigError means every PutCode chunk
			// was acknowledged (the code is on the device) but the
			// follow-up SetConfig call did not confirm — a confirmation
			// gap, not an upload failure. Anything else means the chunk
			// transfer itself did not complete: genuinely failed.
			var confErr *script.PostUploadConfigError
			if errors.As(err, &confErr) {
				id = confErr.Id
				log.Info("Script code confirmed on device, but enabling it did not confirm; will still try to start it", "name", name, "device", device.Name(), "error", confErr.Error())
			} else {
				status, message := classifyUpload(err, nil)
				log.Error(err, message, "name", name, "device", device.Name())
				return 0, status, err
			}
		} else {
			// Create/update KVS entry with script version
			_, err = kvs.SetKeyValue(ctx, log, via, device, kvsKey, version)
			if err != nil {
				log.Error(err, "Unable to set KVS entry for script version", "key", kvsKey, "version", version, "device", device.Name())
				// Don't fail the upload if KVS fails, just log the error
			} else {
				log.Info("Set KVS entry for script version", "key", kvsKey, "version", version, "device", device.Name())
			}
		}
	} else {
		log.Info(reason, "name", name, "version", version)
	}

	// Now start the script, with a bounded retry: the device is often still
	// busy for a few seconds right after upload, and a bare timeout on the
	// first attempt is not evidence the upload failed (issue #428).
	confirmErr := startWithRetry(ctx, log, via, device, name)
	status, message := classifyUpload(nil, confirmErr)
	log.Info(message, "name", name, "device", device.Name())

	return id, status, nil
}

// shouldUpload decides whether the script's code must be sent to the device,
// and returns the reason for the log line.
//
// The subtle case is the last one. A matching version marker is not on its own
// evidence that the device still has the script: the marker lives in the
// device's KVS and outlives any deletion that does not go through
// DeleteWithVersion — the web UI, a raw Script.Delete RPC, another tool, a
// factory reset. Skipping the upload then leaves UploadWithVersion starting a
// script that is absent (an outright error) or, if something recreated it
// empty, one that runs and does nothing while reporting success (#449).
//
// Kept as a pure function so this decision is testable without a device.
func shouldUpload(force bool, wantVersion, kvsVersion string, present bool) (bool, string) {
	switch {
	case force:
		return true, "Force flag set, uploading script"
	case wantVersion != kvsVersion:
		return true, "Script version is different, uploading new one"
	case !present:
		return true, "KVS records this version but the script is not on the device, uploading anyway"
	default:
		return false, "Script version is the same and the script is present, skipping upload"
	}
}

// scriptIsLoaded reports whether the device currently has a script with this
// name. A lookup failure is reported as "present" so a transient RPC error
// cannot turn into a surprise re-upload: the version check then decides on its
// own, exactly as it did before.
func scriptIsLoaded(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, basename string) bool {
	loaded, err := script.ListLoaded(ctx, via, device)
	if err != nil {
		log.Info("Unable to list loaded scripts, assuming the script is present", "name", basename, "device", device.Name())
		return true
	}
	for _, l := range loaded {
		if l.Name == basename {
			return true
		}
	}
	return false
}

// DeleteWithVersion deletes a script and removes its version from KVS
// This is MyHome-specific business logic that combines script deletion with version cleanup
func DeleteWithVersion(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, name string) (any, error) {
	// Delete the script using the generic pkg/shelly/script package
	out, err := script.StartStopDelete(ctx, via, device, name, script.Delete)
	if err != nil {
		return nil, err
	}

	// Remove the version entry from KVS
	basename := filepath.Base(name)
	kvsKey := fmt.Sprintf("script/%s", basename)
	_, err = kvs.DeleteKey(ctx, log, via, device, kvsKey)
	if err != nil {
		log.Error(err, "Unable to delete KVS entry for script version", "key", kvsKey, "device", device.Name())
		// Don't fail the delete if KVS fails, just log the error
	} else {
		log.Info("Deleted KVS entry for script version", "key", kvsKey, "device", device.Name())
	}

	return out, nil
}
