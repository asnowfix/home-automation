package jobs

import (
	"context"
	"homectl/options"

	"hlog"
	"schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/types"
)

var scheduleCtl = &cobra.Command{
	Use:   "schedule",
	Short: "Configure Shelly devices scheduled jobs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := options.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, before, options.Via, scheduleOneDeviceJobs, after)
	},
}

func scheduleOneDeviceJobs(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return schedule.ScheduleJobs(ctx, log, via, device)
}
