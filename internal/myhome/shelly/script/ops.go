package script

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"path/filepath"

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

// UploadWithVersion uploads a script and tracks its version in KVS
// This is MyHome-specific business logic that combines script upload with version tracking
func UploadWithVersion(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, name string, code []byte, minify bool, force bool) (uint32, error) {
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
			return 0, err
		}

		// Create/update KVS entry with script version
		_, err = kvs.SetKeyValue(ctx, log, via, device, kvsKey, version)
		if err != nil {
			log.Error(err, "Unable to set KVS entry for script version", "key", kvsKey, "version", version, "device", device.Name())
			// Don't fail the upload if KVS fails, just log the error
		} else {
			log.Info("Set KVS entry for script version", "key", kvsKey, "version", version, "device", device.Name())
		}
	} else {
		log.Info(reason, "name", name, "version", version)
	}

	// Now start the script
	_, err = script.StartStopDelete(ctx, via, device, name, script.Start)
	if err != nil {
		log.Error(err, "Unable to start script", "name", name, "device", device.Name())
		return 0, err
	}

	return id, nil
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
