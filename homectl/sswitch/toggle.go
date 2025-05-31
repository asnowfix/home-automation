package sswitch

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"
	"reflect"
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
		op := "toggle"
		if len(args) == 2 {
			op = args[1]
		}
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, toggleOneDevice, []string{op})
		return err
	},
}

func toggleOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
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
		out, err = sd.CallE(ctx, via, sswitch.Toggle.String(), &sswitch.ToggleRequest{Id: toggleSwitchId})
	case "on":
		out, err = sd.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: toggleSwitchId, On: !offValue(ctx, log, via, device)})
	case "off":
		out, err = sd.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: toggleSwitchId, On: offValue(ctx, log, via, device)})
	default:
		return nil, fmt.Errorf("unknown operation %s", args[0])
	}

	if err != nil {
		err = fmt.Errorf("failed to run %s device %s: %v", op, sd.Id(), err)
		log.Info("Failed to run %s device %s: %v", sd.Id(), err)
		return nil, err
	}

	return out, err
}

func offValue(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device) bool {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return false
	}
	out, err := sd.CallE(ctx, via, kvs.Get.String(), sswitch.SwitchedOffKey)
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
