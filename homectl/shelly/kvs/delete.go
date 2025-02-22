package kvs

import (
	"context"
	"hlog"

	hopts "homectl/options"
	"homectl/shelly/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

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
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, before, options.Via, deleteKeys, after)
	},
}

func deleteKeys(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	key := args[0]
	return kvs.DeleteKey(ctx, log, via, device, key)
}
