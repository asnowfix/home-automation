package script

import (
	"context"
	"hlog"
	hopts "homectl/options"
	"homectl/shelly/options"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "Report status of every scripts loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, before, options.Via, doList, after)
	},
}

func doList(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return script.ListAll(ctx, device, via)
}
