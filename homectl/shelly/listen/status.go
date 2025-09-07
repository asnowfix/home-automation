package listen

import (
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"strings"

	"github.com/spf13/cobra"
)

var (
	statusFlagSwitchID string
	statusFlagFollowID string
)

var statusCmd = &cobra.Command{
	Use:   "status <device> <remote-device-id>",
	Short: "Configure follow of a remote device status (KVS entry for status-listener.js)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		remoteDeviceId := strings.ToLower(strings.TrimSpace(args[1]))
		if remoteDeviceId == "" {
			return fmt.Errorf("invalid remote device ID: %q", args[1])
		}

		// Build JSON payload for status-listener.js
		payload := make(map[string]any)
		payload["switch_id"] = statusFlagSwitchID
		payload["follow_id"] = statusFlagFollowID

		valueBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		kvKey := "follow/status/" + remoteDeviceId

		_, err = myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doSetKVS, []string{kvKey, string(valueBytes)})
		return err
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusFlagSwitchID, "switch-id", "switch:0", "Local switch ID to control, e.g. switch:0")
	statusCmd.Flags().StringVar(&statusFlagFollowID, "follow-id", "switch:0", "Remote input ID to monitor: switch:X (mirror state) or input:X (toggle on button press)")
}
