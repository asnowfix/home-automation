package follow

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"strings"
	"tools"

	"github.com/spf13/cobra"
)

var (
	bluFlagAutoOff    int
	bluFlagIllumMin   int
	bluFlagIllumMax   int
	bluFlagSwitchID   string
	bluFlagNextSwitch string
)

var BluCmd = &cobra.Command{
	Use:   "blu <follower-device> <blu-mac>",
	Short: "Configure Shelly device to follow a Shelly BLU device",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		followerDevice := args[0]
		mac := tools.NormalizeMac(args[1])
		if mac == "" {
			return fmt.Errorf("invalid BLU MAC address: %q", args[1])
		}

		// Build JSON payload with defaults and optional fields
		payload := make(map[string]any)
		payload["switch_id"] = bluFlagSwitchID
		payload["auto_off"] = bluFlagAutoOff
		if cmd.Flags().Changed("illuminance-min") && bluFlagIllumMin > 0 {
			payload["illuminance_min"] = bluFlagIllumMin
		}
		if cmd.Flags().Changed("illuminance-max") && bluFlagIllumMax > 0 {
			payload["illuminance_max"] = bluFlagIllumMax
		}
		if cmd.Flags().Changed("next-switch") && strings.TrimSpace(bluFlagNextSwitch) != "" {
			payload["next_switch"] = bluFlagNextSwitch
		}

		valueBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		kvKey := "follow/shelly-blu/" + mac

		_, err = myhome.Foreach(cmd.Context(), hlog.Logger, followerDevice, options.Via, doSetKVS, []string{kvKey, string(valueBytes)})
		return err
	},
}

func init() {
	// Defaults as requested: auto_off=300s, illuminance_min=10, no default for illuminance_max or next_switch, switch_id=switch:0
	BluCmd.Flags().IntVar(&bluFlagAutoOff, "auto-off", 300, "Seconds before auto turning off (default 300)")
	BluCmd.Flags().IntVar(&bluFlagIllumMin, "illuminance-min", 0, "Minimum illuminance (lux) to trigger (default 0)")
	BluCmd.Flags().IntVar(&bluFlagIllumMax, "illuminance-max", 10, "Maximum illuminance (lux) to trigger (default 10)")
	BluCmd.Flags().StringVar(&bluFlagSwitchID, "switch-id", "switch:0", "Switch ID to operate, e.g. switch:0")
	BluCmd.Flags().StringVar(&bluFlagNextSwitch, "next-switch", "", "Optional next switch ID to turn on after auto-off (unset by default)")
}
