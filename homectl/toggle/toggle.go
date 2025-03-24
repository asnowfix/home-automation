package toggle

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"
	"strconv"

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
		devices, err := myhome.TheClient.LookupDevices(ctx, args[0])
		if err != nil {
			return err
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
		out, err = device.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: toggleSwitchId, On: !offValue(ctx, log, via, device)})
	case "off":
		out, err = device.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: toggleSwitchId, On: offValue(ctx, log, via, device)})
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

func offValue(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device) bool {
	out, err := device.CallE(ctx, via, kvs.Get.String(), sswitch.SwitchedOffKey)
	if err != nil {
		log.Info("Unable to get value", "key", sswitch.SwitchedOffKey, "reason", err)
		return false
	}
	kv, ok := out.(*kvs.Value)
	if !ok {
		log.Error(err, "Invalid value", "key", sswitch.SwitchedOffKey, "value", out)
		return false
	}
	off, err := strconv.ParseBool(kv.Value)
	if err != nil {
		log.Error(err, "Invalid value", "key", sswitch.SwitchedOffKey, "value", kv.Value)
		return false
	}
	return off
}
