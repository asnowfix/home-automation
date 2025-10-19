package follow

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"time"

	"github.com/spf13/cobra"
)

var (
	shellyFlagSwitchID string
	shellyFlagFollowID string
)

var ShellyCmd = &cobra.Command{
	Use:   "shelly <follower-device> <followed-device>",
	Short: "Configure Shelly device to follow another Shelly device status",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		payload := make(map[string]any)
		payload["switch_id"] = shellyFlagSwitchID
		payload["follow_id"] = shellyFlagFollowID

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
	ShellyCmd.Flags().StringVar(&shellyFlagSwitchID, "switch-id", "switch:0", "Local switch ID to control, e.g. switch:0")
	ShellyCmd.Flags().StringVar(&shellyFlagFollowID, "follow-id", "switch:0", "Remote input ID to monitor: switch:X (mirror state) or input:X (toggle on button press)")
}
