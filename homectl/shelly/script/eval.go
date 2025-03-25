package script

import (
	"context"
	"encoding/json"
	"fmt"
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
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doEval, options.Args(args))
	},
}

func doEval(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, string(script.Eval), &script.EvalRequest{
		Id:   script.Id{Id: flags.Id},
		Code: args[0],
	})
	if err != nil {
		log.Error(err, "Unable to eval script", "id", flags.Id)
		return nil, err
	}
	response := out.(*script.EvalResponse)
	s, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return response, nil
}
