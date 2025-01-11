package kvs

import (
	"hlog"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	"homectl/shelly/options"
)

func init() {
	Cmd.AddCommand(getManyCtl)
}

var getManyCtl = &cobra.Command{
	Use:   "get-many",
	Short: "List Shelly devices Key-Value Store",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, options.MqttClient, strings.Split(options.DeviceNames, ","), via, getMany, args)
	},
}

func getMany(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return kvs.GetMany(via, device)
}
