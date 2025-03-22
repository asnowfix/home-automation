package kvs

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"

	hopts "homectl/options"
	"homectl/shelly/options"

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
	Short: "Delete existing key-value from given shelly devices",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		return shelly.Foreach(cmd.Context(), log, []string{args[0]}, options.Via, deleteKeys, []string{args[1]})
	},
}

func deleteKeys(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	key := args[0]
	s, err := kvs.DeleteKey(ctx, log, via, device, key)
	if err != nil {
		log.Error(err, "Unable to delete", "key", key)
		return nil, err
	}
	if hopts.Flags.Json {
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
