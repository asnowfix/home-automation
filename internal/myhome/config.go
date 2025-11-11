package myhome

import (
	"context"
	"fmt"
	"pkg/shelly"

	"github.com/go-logr/logr"
)

// DeviceConfigParams holds parameters for device configuration
type DeviceConfigParams struct {
	Identifier string
	Name       string
	EcoMode    *bool
}

// ConfigureDevice updates device configuration in the local database and optionally on the device itself.
// For Gen1 devices, only the local database is updated.
// For Gen2+ devices, both the local database and device configuration are updated.
func ConfigureDevice(ctx context.Context, log logr.Logger, identifier string, name string, ecoMode *bool) error {
	log = log.WithName("ConfigureDevice")

	// Get the device from the database using RPC
	result, err := TheClient.CallE(ctx, DeviceShow, identifier)
	if err != nil {
		return fmt.Errorf("device not found: %w", err)
	}

	device, ok := result.(*Device)
	if !ok {
		return fmt.Errorf("unexpected result type: %T", result)
	}

	log.Info("Found device", "id", device.Id(), "name", device.Name(), "manufacturer", device.Manufacturer())

	// Track if we made any changes
	modified := false

	// Update name in local database if specified
	if name != "" && name != device.Name() {
		log.Info("Updating device name in local database", "old_name", device.Name(), "new_name", name)
		device.WithName(name)
		modified = true
	}

	// Save to local database if modified
	if modified {
		// Use the device update RPC method
		_, err = TheClient.CallE(ctx, DeviceUpdate, device)
		if err != nil {
			return fmt.Errorf("failed to update device in local database: %w", err)
		}
		log.Info("Updated device in local database", "id", device.Id())
	}

	// For Gen2+ devices, also update the device configuration
	// Gen1 devices don't support RPC configuration APIs
	if device.Impl() != nil && !shelly.IsGen1Device(device.Id()) {
		log.Info("Updating device configuration on device itself (Gen2+)", "id", device.Id())
		// Get the Shelly device implementation
		sd, ok := device.Impl().(*shelly.Device)
		if !ok {
			log.Error(nil, "Device implementation is not a Shelly device", "type", fmt.Sprintf("%T", device.Impl()))
		} else {
			// Delegate to shelly-specific configuration
			err = shelly.ConfigureDeviceSettings(ctx, log, sd, name, ecoMode)
			if err != nil {
				// Log error but don't fail - local DB was already updated
				log.Error(err, "Failed to update device configuration on device (local DB was updated)", "id", device.Id())
				return fmt.Errorf("device updated in local database, but failed to update on device: %w", err)
			}
		}
	} else if shelly.IsGen1Device(device.Id()) {
		log.Info("Gen1 device - skipping device-side configuration (not supported)", "id", device.Id())
	} else {
		log.Info("Device implementation not loaded, skipping device-side configuration", "id", device.Id())
	}

	return nil
}
