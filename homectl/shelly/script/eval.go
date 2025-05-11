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
	Cmd.AddCommand(evalCtl)
	evalCtl.MarkFlagRequired("id")
}

var evalCtl = &cobra.Command{
	Use:   "eval",
	Short: "Evaluate the given JavaScript code on the given SHelly device(s)",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doEval, args[1:])
		return err
	},
}

func doEval(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return script.EvalInDevice(ctx, via, device, args[0], args[1])
}
