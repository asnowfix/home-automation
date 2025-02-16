package script

import (
	"context"
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
	Cmd.AddCommand(statusCtl)
	statusCtl.MarkFlagRequired("id")
}

var statusCtl = &cobra.Command{
	Use:   "status",
	Short: "Report status of a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, doStatus, args)
	},
}

func doStatus(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, "Script", "GetStatus", &script.Id{
		Id: uint32(flags.Id),
	})
	if err != nil {
		log.Error(err, "Unable to get status for script", "id", flags.Id)
		return nil, err
	}
	response := out.(*script.Status)
	return response, nil
}
