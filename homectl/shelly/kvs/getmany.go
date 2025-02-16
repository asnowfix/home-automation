package kvs

import (
	"context"
	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	hopts "homectl/options"
	"homectl/shelly/options"
)

func init() {
	Cmd.AddCommand(getManyCtl)
}

var getManyCtl = &cobra.Command{
	Use:   "get-many",
	Short: "List Shelly devices Key-Value Store",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)
		ctx, cancel := hopts.InterruptibleContext()
		defer cancel()

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(ctx, log, hopts.MqttClient, hopts.Devices, via, getMany, args)
	},
}

func getMany(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return kvs.GetMany(ctx, log, via, device)
}
