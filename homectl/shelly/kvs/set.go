package kvs

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	"homectl/options"
)

func init() {
	Cmd.AddCommand(setCtl)
}

var setCtl = &cobra.Command{
	Use:   "set",
	Short: "Set or update a key-value on the given shelly device(s)",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		ctx := cmd.Context()
		devices, err := myhome.TheClient.LookupDevices(ctx, args[0])
		if err != nil {
			return err
		}
		ids := make([]string, len(devices.Devices))
		for i, d := range devices.Devices {
			ids[i] = d.Id
		}

		return shelly.Foreach(ctx, log, ids, options.Via, setKeyValue, args[1:])
	},
}

func setKeyValue(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	key := args[0]
	value := args[1]
	log.Info("Setting key", "key", key, "value", value, "device", device.Host)
	status, err := kvs.SetKeyValue(ctx, log, via, device, key, value)
	if err != nil {
		log.Error(err, "Unable to set", "key", key, "value", value)
		return nil, err
	}
	if options.Flags.Json {
		s, err := json.Marshal(status)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(status)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}
	return nil, nil
}
