package kvs

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"

	"homectl/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
)

func init() {
	Cmd.AddCommand(deleteCtl)
}

var deleteCtl = &cobra.Command{
	Use:   "delete",
	Short: "Delete matching key-value from given shelly devices",
	Args:  cobra.ExactArgs(2),
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
		return shelly.Foreach(ctx, log, ids, options.Via, deleteKeys, args[1:])
	},
}

func deleteKeys(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	var match string
	if len(args) > 0 {
		match = args[0]
	} else {
		match = "*" // default
	}
	keys, err := kvs.ListKeys(ctx, log, via, device, match)
	if err != nil {
		log.Error(err, "Unable to list keys")
		return nil, err
	}

	log.Info("Deleting", "keys", keys.Keys, "count", len(keys.Keys))

	for key := range keys.Keys {
		log.Info("Deleting key", "key", key, "device", device.Host)
		deleteKey(ctx, log, via, device, key)
	}
	return nil, nil
}

func deleteKey(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, key string) (any, error) {
	s, err := kvs.DeleteKey(ctx, log, via, device, key)
	if err != nil {
		log.Error(err, "Unable to delete", "key", key)
		return nil, err
	}
	if options.Flags.Json {
		s, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(s)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}
	return nil, nil
}
