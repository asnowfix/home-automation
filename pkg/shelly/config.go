package shelly

import (
	"context"
	"fmt"
	"pkg/shelly/system"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

// ConfigureDeviceSettings updates Shelly device configuration on the device itself (Gen2+ only).
// This function should only be called for Gen2+ devices.
// It updates the device name and/or eco mode on the device.
func ConfigureDeviceSettings(ctx context.Context, log logr.Logger, device *Device, name string, ecoMode *bool) error {
	log = log.WithName("ConfigureDeviceSettings")

	// Get current configuration
	config, err := system.GetConfig(ctx, types.ChannelDefault, device)
	if err != nil {
		return fmt.Errorf("failed to get device configuration: %w", err)
	}

	changed := false

	// Update name if specified
	if name != "" && config.Device != nil && config.Device.Name != name {
		log.Info("Updating device name on device", "old_name", config.Device.Name, "new_name", name)
		config.Device.Name = name
		changed = true
	}

	// Update eco mode if specified
	if ecoMode != nil && config.Device != nil && config.Device.EcoMode != *ecoMode {
		log.Info("Updating eco mode on device", "old_ecomode", config.Device.EcoMode, "new_ecomode", *ecoMode)
		config.Device.EcoMode = *ecoMode
		changed = true
	}

	// Apply changes if any
	if changed {
		_, err = system.SetConfig(ctx, types.ChannelDefault, device, config)
		if err != nil {
			return fmt.Errorf("failed to update device configuration: %w", err)
		}
		log.Info("Successfully updated device configuration on device", "id", device.Id())
	} else {
		log.Info("No changes to apply on device", "id", device.Id())
	}

	return nil
}
