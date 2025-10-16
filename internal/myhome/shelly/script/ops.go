package script

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"pkg/shelly/kvs"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

// UploadWithVersion uploads a script and tracks its version in KVS
// This is MyHome-specific business logic that combines script upload with version tracking
func UploadWithVersion(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, name string, content []byte, minify bool, force bool) (uint32, error) {
	// Calculate version hash
	h := sha1.New()
	h.Write(content)
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

	// Upload if forced or version changed
	var id uint32
	if force || version != kvsVersion {
		if force {
			log.Info("Force flag set, uploading script", "name", name, "version", version)
		} else {
			log.Info("Script version is different, uploading new one", "name", name, "version", version)
		}
		
		// Upload the script using the generic pkg/shelly/script package
		id, err = script.UploadAndStart(ctx, via, device, name, content, minify)
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
		log.Info("Script version is the same, skipping upload", "name", name, "version", version)
		// Still need to start the script
		_, err = script.StartStopDelete(ctx, via, device, name, script.Start)
		if err != nil {
			log.Error(err, "Unable to start script", "name", name, "device", device.Name())
			return 0, err
		}
	}
	
	return id, nil
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
