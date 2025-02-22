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
	Cmd.AddCommand(startCtl)
	startCtl.MarkFlagRequired("id")
}

var startCtl = &cobra.Command{
	Use:   "start",
	Short: "Start a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, _ := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, before, options.Via, doStartStop, []string{"Start"})
	},
}

func init() {
	Cmd.AddCommand(stopCtl)
	stopCtl.MarkFlagRequired("id")
}

var stopCtl = &cobra.Command{
	Use:   "stop",
	Short: "Stop a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, _ := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, before, options.Via, doStartStop, []string{"Stop"})
	},
}

func init() {
	Cmd.AddCommand(deleteCtl)
	deleteCtl.MarkFlagRequired("id")
}

var deleteCtl = &cobra.Command{
	Use:   "delete",
	Short: "Delete a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, _ := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, before, options.Via, doStartStop, []string{"Delete"})
	},
}

func doStartStop(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	operation := args[0]
	return script.StartStopDelete(ctx, via, device, flags.Name, flags.Id, script.Verb(operation))
}
