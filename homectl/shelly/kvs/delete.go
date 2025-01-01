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
	Cmd.AddCommand(deleteCtl)
}

var deleteCtl = &cobra.Command{
	Use:   "delete",
	Short: "Delete existing key-values from given shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, deleteKeys, args)
	},
}

func deleteKeys(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*shelly.Device, error) {
	out, err := device.CallE(via, "KVS", "Delete", &kvs.Key{
		Key: args[0],
	})
	if err != nil {
		log.Error(err, "Unable to delete key:"+args[0])
		return nil, err
	}
	status := out.(*kvs.Status)
	s, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return device, nil
}
