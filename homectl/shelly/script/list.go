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
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "Report status of every scripts loaded on the given Shelly device(s)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doList, options.Args(args))
	},
}

func doList(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return script.ListAll(ctx, device, via)
}
