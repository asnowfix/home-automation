package kvs

import (
	"context"
	"hlog"
	"myhome"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	"homectl/options"
)

func init() {
	Cmd.AddCommand(getCtl)
}

var getCtl = &cobra.Command{
	Use:   "get",
	Short: "Get values from Shelly devices Key-Value Store",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		match := "*"
		if len(args) == 2 {
			match = args[1]
		}
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, get, []string{match})
		return err
	},
}

func get(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := kvs.GetManyValues(ctx, log, via, device, args[0])
	if err != nil {
		log.Error(err, "Unable to get many key-values")
		return nil, err
	}
	options.PrintResult(out)
	return out, nil
}
