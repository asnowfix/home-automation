package sswitch

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
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

var switchId int

func init() {
	Cmd.PersistentFlags().IntVarP(&switchId, "switch", "S", 0, "Use this switch ID.")

	Cmd.AddCommand(toggleCmd)
	Cmd.AddCommand(onCmd)
	Cmd.AddCommand(offCmd)
}

var Cmd = &cobra.Command{
	Use:   "switch",
	Short: "Switch devices on, off, or toggle",
	Args:  cobra.NoArgs,
}

var toggleCmd = &cobra.Command{
	Use:   "toggle <device-id>",
	Short: "Toggle device switch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, toggleOneDevice, []string{"toggle"})
		return err
	},
}

var onCmd = &cobra.Command{
	Use:   "on <device-id>",
	Short: "Turn device switch on",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, toggleOneDevice, []string{"on"})
		return err
	},
}

var offCmd = &cobra.Command{
	Use:   "off <device-id>",
	Short: "Turn device switch off",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, toggleOneDevice, []string{"off"})
		return err
	},
}

func toggleOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	var out any
	var err error

	switch args[0] {
	case "toggle":
		out, err = sd.CallE(ctx, via, sswitch.Toggle.String(), &sswitch.ToggleRequest{Id: switchId})
	case "on":
		out, err = sd.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: switchId, On: !offValue(ctx, log, via, device)})
	case "off":
		out, err = sd.CallE(ctx, via, sswitch.Set.String(), &sswitch.SetRequest{Id: switchId, On: offValue(ctx, log, via, device)})
	default:
		return nil, fmt.Errorf("unknown operation %s", args[0])
	}

	if err != nil {
		err = fmt.Errorf("failed to run %s device %s: %v", args[0], sd.Id(), err)
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
	out, err := sd.CallE(ctx, via, kvs.Get.String(), sswitch.NormallyClosedKey)
	if err != nil {
		log.Info("Unable to get value", "key", sswitch.NormallyClosedKey, "reason", err)
		return false
	}
	kv, ok := out.(*kvs.Value)
	if !ok {
		log.Error(err, "Invalid value", "key", sswitch.NormallyClosedKey, "value", out)
		return false
	}
	off, err := strconv.ParseBool(kv.Value)
	if err != nil {
		log.Error(err, "Invalid value", "key", sswitch.NormallyClosedKey, "value", kv.Value)
		return false
	}
	return off
}
