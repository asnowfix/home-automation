package kvs

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	hopts "homectl/options"
	"homectl/shelly/options"
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
		return shelly.Foreach(cmd.Context(), log, []string{args[0]}, options.Via, setKeyValue, args[1:])
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
	if hopts.Flags.Json {
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
