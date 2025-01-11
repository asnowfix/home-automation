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
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "List Shelly devices Key-Value Store",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, options.MqttClient, strings.Split(options.DeviceNames, ","), via, listKeys, args)
	},
}

func listKeys(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	var match string
	if len(args) > 0 {
		match = args[0]
	} else {
		match = "*" // default
	}
	return kvs.ListKeys(via, device, match)
}
