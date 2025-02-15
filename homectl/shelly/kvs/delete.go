package kvs

import (
	"hlog"

	hopts "homectl/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	"homectl/shelly/options"
)

func init() {
	Cmd.AddCommand(deleteCtl)
}

var deleteCtl = &cobra.Command{
	Use:   "delete",
	Short: "Delete existing key-value from given shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, deleteKeys, args)
	},
}

func deleteKeys(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	key := args[0]
	return kvs.Delete(via, device, key)
}
