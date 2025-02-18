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
	Cmd.AddCommand(setCtl)
}

var setCtl = &cobra.Command{
	Use:   "set",
	Short: "Set or update a key-value on the given shelly device(s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, hopts.Devices, via, setKeyValue, args)
	},
}

func setKeyValue(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	key := args[0]
	value := args[1]
	return kvs.SetKeyValue(ctx, log, via, device, key, value)
}
