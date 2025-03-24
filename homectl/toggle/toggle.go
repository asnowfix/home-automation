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
	Use:   "switch",
	Short: "Switch devices",
	Args:  cobra.RangeArgs(1, 2),
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

		op := "toggle"
		if len(args) == 2 {
			op = args[1]
		}
		return shelly.Foreach(ctx, log, ids, options.Via, toggleOneDevice, []string{op})
	},
}

func toggleOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	var op string
	if len(args) == 0 {
		op = "toggle"
	} else {
		op = args[0]
	}

	var out any
	var err error

	switch op {
	case "toggle":
		out, err = device.CallE(ctx, via, sswitch.Toggle.String(), &sswitch.ToggleRequest{Id: toggleSwitchId})
	case "on":
		out, err = device.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: toggleSwitchId, On: true})
	case "off":
		out, err = device.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: toggleSwitchId, On: false})
	default:
		return nil, fmt.Errorf("unknown operation %s", args[0])
	}

	if err != nil {
		err = fmt.Errorf("failed to run %s device %s: %v", op, device.Id_, err)
		log.Info("Failed to run %s device %s: %v", device.Id_, err)
		return nil, err
	}

	return out, err
}
