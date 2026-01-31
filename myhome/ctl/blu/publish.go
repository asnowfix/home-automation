package blu

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	mhblu "internal/myhome/blu"
	mhscript "internal/myhome/shelly/script"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/ble"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var (
	clearFollows bool
)

var PublishCmd = &cobra.Command{
	Use:   "publish <gateway-device> [blu-device]",
	Short: "Enable BLE gateway and upload blu-publisher.js to publish BLU device events",
	Long: `Enable BLE gateway on a Shelly device and upload blu-publisher.js script
to publish events from BLU devices over MQTT.

When [blu-device] is provided, the script will only publish events from that specific device.
When [blu-device] is omitted, the script will publish ALL BLU events without filtering.

The [blu-device] can be specified as:
- MAC address: "e8:e0:7e:a6:0c:6f", "E8E07EA60C6F", "e8-e0-7e-a6-0c-6f"
- Device ID: "shellyblu-e8e07ea60c6f"
- Device name: "motion-sensor-hallway"

When called with "-" as <gateway-device>, lists all devices that publish the given BLU device.

Use --clear to remove all existing BLU follow entries before configuring.

This command:
1. Enables the BLE observer/gateway on the device
2. Optionally clears existing BLU follow entries (with --clear)
3. Configures the device to follow the specified BLU MAC (if provided)
4. Uploads blu-publisher.js script with version tracking`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		gatewayDevice := args[0]

		// If no blu-device provided, enable "publish all" mode
		if len(args) == 1 {
			return enablePublishAll(cmd.Context(), gatewayDevice, clearFollows)
		}

		bluDevice := args[1]

		// Resolve BLU device identifier to MAC address
		mac, err := mhblu.ResolveMac(cmd.Context(), bluDevice)
		if err != nil {
			return fmt.Errorf("failed to resolve BLU device %q: %w", bluDevice, err)
		}

		// If gateway-device is "-", list all devices publishing this BLU MAC
		if gatewayDevice == "-" {
			return listDevicesPublishingBlu(cmd.Context(), mac)
		}

		return addDevicePublishingBlu(cmd.Context(), gatewayDevice, mac, clearFollows)
	},
}

func init() {
	PublishCmd.Flags().BoolVar(&clearFollows, "clear", false, "Clear all existing BLU follow entries before configuring")
}

// enablePublishAll configures a Shelly device to publish ALL BLU events without filtering
// An empty follows map in KVS means "publish all"
func enablePublishAll(ctx context.Context, gatewayDevice string, clear bool) error {
	log := hlog.Logger

	// Enable BLE observer/gateway
	fmt.Printf("Enabling BLE gateway on %s...\n", gatewayDevice)
	_, err := myhome.Foreach(ctx, log, gatewayDevice, options.Via, enableBleGateway, []string{})
	if err != nil {
		return fmt.Errorf("failed to enable BLE gateway: %w", err)
	}

	// Clear all existing follow/shelly-blu/* KVS entries if --clear flag is set
	if clear {
		fmt.Printf("Clearing existing BLU follow entries...\n")
		_, err = myhome.Foreach(ctx, log, gatewayDevice, options.Via, clearBluFollows, []string{})
		if err != nil {
			return fmt.Errorf("failed to clear BLU follows: %w", err)
		}
	}

	// Upload blu-publisher.js script
	// Empty follows map = publish all BLU events
	fmt.Printf("Uploading blu-publisher.js script...\n")
	longCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err = myhome.Foreach(longCtx, log, gatewayDevice, options.Via, uploadScript, []string{"blu-publisher.js"})
	if err != nil {
		return fmt.Errorf("failed to upload script: %w", err)
	}

	fmt.Printf("✓ BLE gateway enabled and blu-publisher.js uploaded (publish-all mode)\n")
	return nil
}

// addDevicePublishingBlu configures a Shelly device to publish events from a BLU MAC address
func addDevicePublishingBlu(ctx context.Context, gatewayDevice, mac string, clear bool) error {
	log := hlog.Logger

	// Enable BLE observer/gateway
	fmt.Printf("Enabling BLE gateway on %s...\n", gatewayDevice)
	_, err := myhome.Foreach(ctx, log, gatewayDevice, options.Via, enableBleGateway, []string{})
	if err != nil {
		return fmt.Errorf("failed to enable BLE gateway: %w", err)
	}

	// Clear all existing follow/shelly-blu/* KVS entries if --clear flag is set
	if clear {
		fmt.Printf("Clearing existing BLU follow entries...\n")
		_, err = myhome.Foreach(ctx, log, gatewayDevice, options.Via, clearBluFollows, []string{})
		if err != nil {
			return fmt.Errorf("failed to clear BLU follows: %w", err)
		}
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

	// Enable BLE with observer using the ble module
	config := &ble.Config{
		Enable: true,
		Observer: &ble.Observer{
			Enable: true,
		},
	}

	resp, err := ble.DoSetConfig(ctx, via, sd, config)
	if err != nil {
		return nil, fmt.Errorf("failed to enable BLE observer: %w", err)
	}

	// Check if restart is required
	if resp.RestartRequired {
		fmt.Printf("  Note: Device restart required for BLE changes to take effect\n")
	}

	fmt.Printf("✓ BLE gateway enabled on %s\n", sd.Name())
	return resp, nil
}

// clearBluFollows deletes all follow/shelly-blu/* KVS entries to enable publish-all mode
func clearBluFollows(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
	}

	// List all keys with the follow/shelly-blu/ prefix
	kvsPrefix := "follow/shelly-blu/"
	keys, err := kvs.ListKeys(ctx, log, via, sd, kvsPrefix+"*")
	if err != nil {
		log.Info("No existing BLU follow entries to clear", "device", sd.Id())
		return nil, nil // Not an error if no keys exist
	}

	if keys == nil || len(keys.Keys) == 0 {
		fmt.Printf("  No existing BLU follow entries on %s\n", sd.Name())
		return nil, nil
	}

	// Delete each key
	for key := range keys.Keys {
		log.Info("Deleting BLU follow entry", "key", key, "device", sd.Id())
		_, err := kvs.DeleteKey(ctx, log, via, sd, key)
		if err != nil {
			log.Error(err, "Failed to delete key", "key", key)
			// Continue deleting other keys
		}
	}

	fmt.Printf("  Cleared %d BLU follow entries on %s\n", len(keys.Keys), sd.Name())
	return nil, nil
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
