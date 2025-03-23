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
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "List Shelly devices Key-Value Store",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := options.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, before, options.Via, listKeys, after)
	},
}

func listKeys(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	var match string
	if len(args) > 0 {
		match = args[0]
	} else {
		match = "*" // default
	}
	kis, err := kvs.ListKeys(ctx, log, via, device, match)
	if err != nil {
		log.Error(err, "Unable to list keys")
		return nil, err
	}
	if options.Flags.Json {
		s, err := json.Marshal(kis)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(kis)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}
	return nil, nil
}
