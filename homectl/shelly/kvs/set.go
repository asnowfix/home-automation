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
	Cmd.AddCommand(setCtl)
}

var setCtl = &cobra.Command{
	Use:   "set",
	Short: "Set or update a key-value on the given shelly device(s)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, before, options.Via, setKeyValue, after)
	},
}

func setKeyValue(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	key := args[0]
	value := args[1]
	return kvs.SetKeyValue(ctx, log, via, device, key, value)
}
