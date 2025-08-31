package listenblu

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
	flagAutoOff    int
	flagIllumMin   int
	flagIllumMax   int
	flagSwitchID   string
	flagNextSwitch string
)

var Cmd = &cobra.Command{
	Use:   "listen-blu <device> <blu-mac>",
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
		payload["switch_id"] = flagSwitchID
		payload["auto_off"] = flagAutoOff
		payload["illuminance_min"] = flagIllumMin
		// Only include illuminance_max if the flag was explicitly provided
		if cmd.Flags().Changed("illuminance-max") {
			payload["illuminance_max"] = flagIllumMax
		}
		// Only include next_switch if explicitly provided (non-empty and flag set)
		if cmd.Flags().Changed("next-switch") && strings.TrimSpace(flagNextSwitch) != "" {
			payload["next_switch"] = flagNextSwitch
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
	Cmd.Flags().IntVar(&flagAutoOff, "auto-off", 300, "Seconds before auto turning off (default 300)")
	Cmd.Flags().IntVar(&flagIllumMin, "illuminance-min", 10, "Minimum illuminance (lux) to trigger (default 10)")
	Cmd.Flags().IntVar(&flagIllumMax, "illuminance-max", 0, "Maximum illuminance (lux) to trigger (unset by default)")
	Cmd.Flags().StringVar(&flagSwitchID, "switch-id", "switch:0", "Switch ID to operate, e.g. switch:0")
	Cmd.Flags().StringVar(&flagNextSwitch, "next-switch", "", "Optional next switch ID to turn on after auto-off (unset by default)")
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
