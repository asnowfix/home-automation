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
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, getMany, args)
	},
}

func getMany(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*shelly.Device, error) {
	out, err := device.CallE(via, "KVS", "GetMany", nil)
	if err != nil {
		log.Error(err, "Unable to get many key-values")
		return nil, err
	}
	kvs := out.(*kvs.KeyValueItems)
	s, err := json.Marshal(kvs)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return device, nil
}
