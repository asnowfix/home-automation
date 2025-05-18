package script

import (
	"context"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(statusCtl)
}

var statusCtl = &cobra.Command{
	Use:   "status",
	Short: "Report status of all scripts loaded on the given Shelly device(s)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doStatus, options.Args(args))
		return err
	},
}

func doStatus(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := script.DeviceStatus(ctx, device, via)
	if err != nil {
		hlog.Logger.Error(err, "Unable to get scripts status")
		return nil, err
	}
	options.PrintResult(out)
	return out, nil
}
