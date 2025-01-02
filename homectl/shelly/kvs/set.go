package kvs

import (
	"encoding/json"
	"fmt"
	"hlog"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/kvs"
	"devices/shelly/types"

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
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, setKeyValue, args)
	},
}

func setKeyValue(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(via, "KVS", "Set", &kvs.KeyValue{
		Key:   kvs.Key{Key: args[0]},
		Value: kvs.Value{Value: args[1]},
	})
	if err != nil {
		log.Error(err, "Unable to set", "key", args[0], "value", args[1])
		return nil, err
	}
	status := out.(*kvs.Status)
	s, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return status, nil
}
