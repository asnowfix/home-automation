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

	"homectl/options"
)

func init() {
	Cmd.AddCommand(getManyCtl)
}

var getManyCtl = &cobra.Command{
	Use:   "get-many",
	Short: "List Shelly devices Key-Value Store",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		match := "*"
		if len(args) == 2 {
			match = args[1]
		}
		return shelly.Foreach(cmd.Context(), log, []string{args[0]}, options.Via, getMany, []string{match})
	},
}

func getMany(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	kv, err := kvs.GetManyValues(ctx, log, via, device, args[0])
	if err != nil {
		log.Error(err, "Unable to get many key-values")
		return nil, err
	}
	if options.Flags.Json {
		s, err := json.Marshal(kv)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(kv)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}
	return nil, nil
}
