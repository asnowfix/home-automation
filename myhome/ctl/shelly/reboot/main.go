package reboot

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/shelly"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "reboot <device-name-or-ip>",
	Short: "Reboot Shelly device",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, oneDeviceReboot, options.Args(args))
		return err
	},
}

func oneDeviceReboot(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	out, err := sd.CallE(ctx, via, shelly.Reboot.String(), nil)
	if err != nil {
		log.Error(err, "Unable to reboot device", "device", sd.Id())
		return nil, err
	}
	log.Info("Device reboot initiated", "device", sd.Id())
	return out, nil
}
