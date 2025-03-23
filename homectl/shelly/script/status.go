package script

import (
	"context"
	"hlog"
	"homectl/options"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(statusCtl)
	statusCtl.MarkFlagRequired("id")
}

var statusCtl = &cobra.Command{
	Use:   "status",
	Short: "Report status of a script loaded on the given Shelly device(s)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := options.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, before, options.Via, doStatus, after)
	},
}

func doStatus(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, string(script.GetStatus), &script.Id{
		Id: flags.Id,
	})
	if err != nil {
		log.Error(err, "Unable to get status for script", "id", flags.Id)
		return nil, err
	}
	response := out.(*script.Status)
	return response, nil
}
