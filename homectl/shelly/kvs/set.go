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
	Cmd.AddCommand(setCtl)
}

var setCtl = &cobra.Command{
	Use:   "set",
	Short: "Set or update a key-value on the given shelly device(s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, options.MqttClient, strings.Split(options.DeviceNames, ","), via, setKeyValue, args)
	},
}

func setKeyValue(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	key := args[0]
	value := args[1]
	return kvs.SetKeyValue(via, device, key, value)
}
