package kvs

import (
	"context"
	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	hopts "homectl/options"
	"homectl/shelly/options"
)

func init() {
	Cmd.AddCommand(getManyCtl)
}

var getManyCtl = &cobra.Command{
	Use:   "get-many",
	Short: "List Shelly devices Key-Value Store",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, before, options.Via, getMany, after)
	},
}

func getMany(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return kvs.GetManyValues(ctx, log, via, device)
}
