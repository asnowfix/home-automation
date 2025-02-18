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
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "List Shelly devices Key-Value Store",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, hopts.Devices, via, listKeys, args)
	},
}

func listKeys(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	var match string
	if len(args) > 0 {
		match = args[0]
	} else {
		match = "*" // default
	}
	return kvs.ListKeys(ctx, log, via, device, match)
}
