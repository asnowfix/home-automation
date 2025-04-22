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
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, deleteKeys, options.Args(args))
		return err
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
