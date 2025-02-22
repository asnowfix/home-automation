package script

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	hopts "homectl/options"
	"homectl/shelly/options"
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
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, hopts.Devices, via, doEval, args)
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
