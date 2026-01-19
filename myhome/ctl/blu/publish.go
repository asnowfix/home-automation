package blu

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	mhscript "internal/myhome/shelly/script"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"time"
	"tools"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var PublishCmd = &cobra.Command{
	Use:   "publish <gateway-device> <blu-mac>",
	Short: "Enable BLE gateway and upload blu-publisher.js to publish BLU device events",
	Long: `Enable BLE gateway on a Shelly device and upload blu-publisher.js script
to publish events from the specified BLU MAC address over MQTT.

When called with "-" as <gateway-device>, lists all devices that publish the given BLU MAC.

This command:
1. Enables the BLE observer/gateway on the device
2. Configures the device to follow the specified BLU MAC
3. Uploads blu-publisher.js script with version tracking`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		gatewayDevice := args[0]
		bluMac := args[1]

		mac := tools.NormalizeMac(bluMac)
		if mac == "" {
			return fmt.Errorf("invalid BLU MAC address: %q", bluMac)
		}

		// If gateway-device is "-", list all devices publishing this BLU MAC
		if gatewayDevice == "-" {
			return listDevicesPublishingBlu(cmd.Context(), mac)
		}

		return addDevicePublishingBlu(cmd.Context(), gatewayDevice, mac)
	},
}

// addDevicePublishingBlu configures a Shelly device to publish events from a BLU MAC address
func addDevicePublishingBlu(ctx context.Context, gatewayDevice, mac string) error {
	log := hlog.Logger

	// Enable BLE observer/gateway
	fmt.Printf("Enabling BLE gateway on %s...\n", gatewayDevice)
	_, err := myhome.Foreach(ctx, log, gatewayDevice, options.Via, enableBleGateway, []string{})
	if err != nil {
		return fmt.Errorf("failed to enable BLE gateway: %w", err)
	}

	// Set KVS configuration for the BLU MAC
	payload := map[string]any{
		"switch_id": "switch:0", // default, not used by publisher but keeps format consistent
	}
	valueBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	kvKey := "follow/shelly-blu/" + mac

	fmt.Printf("Configuring follow for BLU MAC %s...\n", mac)
	_, err = myhome.Foreach(ctx, log, gatewayDevice, options.Via, doSetKVS, []string{kvKey, string(valueBytes)})
	if err != nil {
		return err
	}

	// Upload blu-publisher.js script
	fmt.Printf("Uploading blu-publisher.js script...\n")
	longCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err = myhome.Foreach(longCtx, log, gatewayDevice, options.Via, uploadScript, []string{"blu-publisher.js"})
	if err != nil {
		return fmt.Errorf("failed to upload script: %w", err)
	}

	fmt.Printf("✓ BLE gateway enabled and blu-publisher.js uploaded for MAC %s\n", mac)
	return nil
}

// listDevicesPublishingBlu lists all Shelly devices that publish the given BLU MAC address
func listDevicesPublishingBlu(ctx context.Context, mac string) error {
	log := hlog.Logger
	kvKey := "follow/shelly-blu/" + mac

	// Query all known Shelly devices using "*" wildcard
	_, err := myhome.Foreach(ctx, log, "*", options.Via, func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
		sd, ok := device.(*shelly.Device)
		if !ok {
			return nil, nil // Skip non-Shelly devices
		}

		// Check if this device has the specific BLU MAC in KVS
		resp, err := kvs.GetValue(ctx, log, via, sd, kvKey)
		if err != nil {
			// Key not found means this device doesn't publish the BLU MAC
			return nil, nil
		}

		fmt.Printf("%s: %s\n", sd.Name(), resp.Value)
		return nil, nil
	}, []string{})
	return err
}

// enableBleGateway enables the BLE observer/gateway on a Shelly device
func enableBleGateway(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
	}

	// Enable BLE with observer using RPC call
	params := map[string]any{
		"config": map[string]any{
			"enable": true,
			"observer": map[string]any{
				"enable": true,
			},
		},
	}

	resp, err := sd.CallE(ctx, via, "BLE.SetConfig", params)
	if err != nil {
		return nil, fmt.Errorf("failed to enable BLE observer: %w", err)
	}

	// Check if restart is required
	if respMap, ok := resp.(map[string]any); ok {
		if restartRequired, ok := respMap["restart_required"].(bool); ok && restartRequired {
			fmt.Printf("  Note: Device restart required for BLE changes to take effect\n")
		}
	}

	fmt.Printf("✓ BLE gateway enabled on %s\n", sd.Name())
	return resp, nil
}

// doSetKVS is a helper function for setting KVS entries on Shelly devices
func doSetKVS(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
	}
	key := args[0]
	value := args[1]
	log.Info("Setting follow config", "key", key, "value", value, "device", sd.Id())
	return kvs.SetKeyValue(ctx, log, via, sd, key, value)
}

// uploadScript is a helper function to upload and start scripts on Shelly devices
func uploadScript(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
	}
	scriptName := args[0]
	fmt.Printf(". Uploading %s to %s...\n", scriptName, sd.Name())

	// Upload with version tracking using shared function (minify=true, force=false)
	id, err := mhscript.UploadNamedScript(ctx, log, via, sd, scriptName, true, false)
	if err != nil {
		fmt.Printf("✗ %v\n", err)
		return nil, err
	}
	fmt.Printf("✓ Successfully uploaded %s to %s (id: %d)\n", scriptName, sd.Name(), id)
	return id, nil
}
