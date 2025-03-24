package toggle

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var toggleSwitchId int

func init() {
	Cmd.Flags().IntVarP(&toggleSwitchId, "switch", "S", 0, "Use this switch ID.")
}

var Cmd = &cobra.Command{
	Use:   "toggle",
	Short: "Toggle switch devices",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		ctx := cmd.Context()
		out, err := myhome.TheClient.CallE(ctx, myhome.DeviceLookup, args[0])
		if err != nil {
			return err
		}
		devices, ok := out.(*myhome.Devices)
		if !ok {
			return fmt.Errorf("expected *myhome.Devices, got %T", out)
		}
		ids := make([]string, len(devices.Devices))
		for i, d := range devices.Devices {
			ids[i] = d.Id
		}

		return shelly.Foreach(ctx, log, ids, options.Via, toggleOneDevice, nil)
	},
}

func toggleOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	sr := make(map[string]interface{})
	sr["id"] = toggleSwitchId
	out, err := device.CallE(ctx, via, string(sswitch.Toggle), sr)
	if err != nil {
		log.Info("Failed to toggle device %s: %v", device.Id_, err)
		return nil, err
	}
	return out, err
}
