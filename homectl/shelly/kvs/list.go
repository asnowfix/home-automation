package kvs

import (
	"encoding/json"
	"fmt"
	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/kvs"
	"devices/shelly/types"

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
		return shelly.Foreach(log, args, via, listKeys)
	},
}

func listKeys(log logr.Logger, via types.Channel, device *shelly.Device) (*shelly.Device, error) {
	out, err := device.CallE(via, "KVS", "List", nil)
	if err != nil {
		log.Error(err, "Unable to List keys")
		return nil, err
	}
	keys := out.(*kvs.KeyItems)
	s, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return device, nil
}
