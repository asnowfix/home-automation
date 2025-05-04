package sys

import (
	"context"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/types"

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

func oneDeviceReboot(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, shelly.Reboot.String(), nil)
	if err != nil {
		log.Error(err, "Unable to reboot device", "device", device.String())
		return nil, err
	}
	return out, nil
}
