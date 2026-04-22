package sys

import (
	"context"
	"fmt"
	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/go-shellies/devices"
	shellyapi "github.com/asnowfix/go-shellies"
	"github.com/asnowfix/go-shellies/shelly"
	"github.com/asnowfix/go-shellies/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(rebootCmd)
}

var rebootCmd = &cobra.Command{
	Use:   "reboot",
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
	return out, nil
}
