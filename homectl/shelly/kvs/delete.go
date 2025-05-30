package kvs

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"reflect"

	"homectl/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/devices"
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
		device := args[0]
		_, err := myhome.Foreach(cmd.Context(), log, device, options.Via, doDeleteKeys, options.Args(args))
		return err
	},
}

func doDeleteKeys(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	var match string
	if len(args) > 0 {
		match = args[0]
	} else {
		match = "*" // default
	}
	log.Info("Deleting keys matching", "match", match, "device", sd.Id())
	keys, err := kvs.ListKeys(ctx, log, via, sd, match)
	if err != nil {
		log.Error(err, "Unable to list keys")
		return nil, err
	}

	log.Info("Deleting keys", "keys", keys.Keys, "count", len(keys.Keys))

	for key := range keys.Keys {
		log.Info("Deleting key", "key", key, "device", sd.Id())
		_, err := doDeleteKey(ctx, log, via, sd, key)
		if err != nil {
			log.Error(err, "Unable to delete (skipping)", "key", key)
		}
	}
	return nil, nil
}

func doDeleteKey(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, key string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	out, err := kvs.DeleteKey(ctx, log, via, sd, key)
	if err != nil {
		log.Error(err, "Unable to delete", "key", key)
		return nil, err
	}
	options.PrintResult(out)
	return out, nil
}
