package follow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var (
	shellyFlagSwitchID   string
	shellyFlagFollowID   string
	shellyFlagFollowMode string
	shellyFlagAutoOff    int
)

var Cmd = &cobra.Command{
	Use:   "follow <follower-device> [followed-device]",
	Short: "Configure Shelly device to follow another Shelly device, or list followed devices",
	Long: `Configure Shelly device to follow another Shelly device status.

When called with only <follower-device>, lists the currently followed Shelly devices.

Two follow modes when configuring:
1. activation-only: Follower turns on when followed device activates, then uses auto-off timeout.
   Automatically enabled when --auto-off is provided.
2. full: Follower mirrors both activation and deactivation of the followed device (default).

Examples:
  # List followed Shelly devices
  myhome ctl shelly follow hallway-light

  # Full mirror mode (default) - follows both ON and OFF
  myhome ctl shelly follow hallway-light office-switch

  # Activation-only mode - turns on for 5 minutes when motion detected (--auto-off implies activation-only)
  myhome ctl shelly follow hallway-light motion-sensor --auto-off=300`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return listFollowedShellyDevices(cmd.Context(), args[0])
		}

		followerDevice := args[0]
		followedDevice := args[1]

		// Lookup the followed device to get its ID
		followedDevices, err := myhome.TheClient.LookupDevices(cmd.Context(), followedDevice)
		if err != nil {
			return fmt.Errorf("failed to lookup followed device: %w", err)
		}
		if len(*followedDevices) == 0 {
			return fmt.Errorf("followed device not found: %q", followedDevice)
		}
		followedDeviceId := (*followedDevices)[0].Id()
		hlog.Logger.Info("Lookup followed device", "identifier", followedDevice, "device", followedDeviceId)
		if followedDeviceId == "" {
			return fmt.Errorf("invalid followed device ID: %q", followedDevice)
		}

		// Build JSON payload for status-listener.js
		payload, err := buildShellyFollowPayload(shellyFollowOptions{
			switchID:      shellyFlagSwitchID,
			followID:      shellyFlagFollowID,
			followMode:    shellyFlagFollowMode,
			autoOff:       shellyFlagAutoOff,
			autoOffSet:    cmd.Flags().Changed("auto-off"),
			followModeSet: cmd.Flags().Changed("follow-mode"),
		})
		if err != nil {
			return err
		}

		valueBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		kvKey := "follow/status/" + followedDeviceId

		// Set KVS configuration
		_, err = myhome.Foreach(cmd.Context(), hlog.Logger, followerDevice, options.Via, doSetKVS, []string{kvKey, string(valueBytes)})
		if err != nil {
			return err
		}

		// Upload and start the status-listener.js script
		fmt.Printf("\nUploading status-listener.js script...\n")
		longCtx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		_, err = myhome.Foreach(longCtx, hlog.Logger, followerDevice, options.Via, uploadScript, []string{"status-listener.js"})
		if err != nil {
			return fmt.Errorf("failed to upload script: %w", err)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&shellyFlagSwitchID, "switch-id", "switch:0", "Local switch ID to control, e.g. switch:0")
	Cmd.Flags().StringVar(&shellyFlagFollowID, "follow-id", "switch:0", "Remote input ID to monitor: switch:X (mirror state) or input:X (toggle on button press)")
	Cmd.Flags().StringVar(&shellyFlagFollowMode, "follow-mode", "full", "Follow mode: 'activation-only' (turn on with timeout) or 'full' (mirror on/off)")
	Cmd.Flags().IntVar(&shellyFlagAutoOff, "auto-off", 0, "Seconds before auto turn off (only for activation-only mode, 0 to disable)")
}

var UnfollowCmd = &cobra.Command{
	Use:   "unfollow <follower-device> <followed-device>",
	Short: "Remove Shelly follow configuration and status-listener script",
	Long: `Remove follow configuration between two Shelly devices.

Deletes the KVS key that configures the follow relationship and stops/deletes
the status-listener.js script from the follower device.

Examples:
  myhome ctl shelly unfollow hallway-light office-switch`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		followerDevice := args[0]
		followedDevice := args[1]

		followedDevices, err := myhome.TheClient.LookupDevices(cmd.Context(), followedDevice)
		if err != nil {
			return fmt.Errorf("failed to lookup followed device: %w", err)
		}
		if len(*followedDevices) == 0 {
			return fmt.Errorf("followed device not found: %q", followedDevice)
		}
		followedDeviceId := (*followedDevices)[0].Id()
		if followedDeviceId == "" {
			return fmt.Errorf("invalid followed device ID: %q", followedDevice)
		}

		kvKey := "follow/status/" + followedDeviceId

		_, err = myhome.Foreach(cmd.Context(), hlog.Logger, followerDevice, options.Via, doDeleteKVS, []string{kvKey})
		if err != nil {
			return err
		}

		fmt.Printf("\nRemoving status-listener.js script...\n")
		_, err = myhome.Foreach(cmd.Context(), hlog.Logger, followerDevice, options.Via, deleteScript, []string{"status-listener.js"})
		if err != nil {
			return fmt.Errorf("failed to remove script: %w", err)
		}

		return nil
	},
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

// doDeleteKVS is a helper function for deleting KVS entries on Shelly devices
func doDeleteKVS(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
	}
	key := args[0]
	log.Info("Deleting follow config", "key", key, "device", sd.Id())
	return kvs.DeleteKey(ctx, log, via, sd, key)
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

// listFollowedShellyDevices lists the Shelly devices followed by the given follower device
func listFollowedShellyDevices(ctx context.Context, followerDevice string) error {
	log := hlog.Logger
	_, err := myhome.Foreach(ctx, log, followerDevice, options.Via, func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
		sd, ok := device.(*shelly.Device)
		if !ok {
			return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
		}

		resp, err := kvs.GetManyValues(ctx, log, via, sd, "follow/status/*")
		if err != nil {
			return nil, fmt.Errorf("failed to list followed Shelly devices: %w", err)
		}

		if len(resp.Items) == 0 {
			fmt.Printf("No followed Shelly devices on %s\n", sd.Name())
			return nil, nil
		}

		fmt.Printf("Followed Shelly devices on %s:\n", sd.Name())
		for key, value := range resp.Items {
			deviceId := strings.TrimPrefix(key, "follow/status/")
			fmt.Printf("  %s: %v\n", deviceId, value)
		}
		return nil, nil
	}, []string{})
	return err
}

// deleteScript is a helper function to stop and delete scripts on Shelly devices
func deleteScript(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
	}
	scriptName := args[0]
	fmt.Printf(". Removing %s from %s...\n", scriptName, sd.Name())
	out, err := mhscript.DeleteWithVersion(ctx, log, via, sd, scriptName)
	if err != nil {
		fmt.Printf("✗ %v\n", err)
		return nil, err
	}
	fmt.Printf("✓ Successfully removed %s from %s\n", scriptName, sd.Name())
	return out, nil
}

type shellyFollowOptions struct {
	switchID      string
	followID      string
	followMode    string
	autoOff       int
	autoOffSet    bool
	followModeSet bool
}

// buildShellyFollowPayload constructs the KVS payload for status-listener.js.
// Follow mode is inferred: activation-only when autoOffSet+autoOff>0, explicit
// when followModeSet, otherwise full.
func buildShellyFollowPayload(opts shellyFollowOptions) (map[string]any, error) {
	payload := map[string]any{
		"switch_id": opts.switchID,
		"follow_id": opts.followID,
	}

	var followMode string
	switch {
	case opts.autoOffSet && opts.autoOff > 0:
		followMode = "activation-only"
		payload["auto_off"] = opts.autoOff
	case opts.followModeSet:
		if opts.followMode != "activation-only" && opts.followMode != "full" {
			return nil, fmt.Errorf("invalid follow-mode: %q (must be 'activation-only' or 'full')", opts.followMode)
		}
		followMode = opts.followMode
		if opts.autoOffSet {
			payload["auto_off"] = opts.autoOff
		}
	default:
		followMode = "full"
	}
	payload["follow_mode"] = followMode
	return payload, nil
}
