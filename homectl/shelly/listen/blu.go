package listen

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"tools"
)

var (
	bluFlagAutoOff    int
	bluFlagIllumMin   int
	bluFlagIllumMax   int
	bluFlagSwitchID   string
	bluFlagNextSwitch string
)

var bluCmd = &cobra.Command{
	Use:   "blu <device> <blu-mac>",
	Short: "Configure follow of a Shelly BLU MAC on a device (KVS entry for blu-listener.js)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		mac := tools.NormalizeMac(args[1])
		if mac == "" {
			return fmt.Errorf("invalid BLU MAC address: %q", args[1])
		}

		// Build JSON payload with defaults and optional fields
		payload := make(map[string]any)
		payload["switch_id"] = bluFlagSwitchID
		payload["auto_off"] = bluFlagAutoOff
		if cmd.Flags().Changed("illuminance-min") {
			payload["illuminance_min"] = bluFlagIllumMin
		}
		payload["illuminance_max"] = bluFlagIllumMax
		if cmd.Flags().Changed("next-switch") && strings.TrimSpace(bluFlagNextSwitch) != "" {
			payload["next_switch"] = bluFlagNextSwitch
		}

		valueBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		kvKey := "follow/shelly-blu/" + mac

		_, err = myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doSetKVS, []string{kvKey, string(valueBytes)})
		return err
	},
}

func init() {
	// Defaults as requested: auto_off=300s, illuminance_min=10, no default for illuminance_max or next_switch, switch_id=switch:0
	bluCmd.Flags().IntVar(&bluFlagAutoOff, "auto-off", 300, "Seconds before auto turning off (default 300)")
	bluCmd.Flags().IntVar(&bluFlagIllumMin, "illuminance-min", 0, "Minimum illuminance (lux) to trigger (default 0)")
	bluCmd.Flags().IntVar(&bluFlagIllumMax, "illuminance-max", 10, "Maximum illuminance (lux) to trigger (default 10)")
	bluCmd.Flags().StringVar(&bluFlagSwitchID, "switch-id", "switch:0", "Switch ID to operate, e.g. switch:0")
	bluCmd.Flags().StringVar(&bluFlagNextSwitch, "next-switch", "", "Optional next switch ID to turn on after auto-off (unset by default)")
}

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
