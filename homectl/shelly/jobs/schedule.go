package jobs

import (
	"context"
	"homectl/options"
	"myhome"

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
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, scheduleOneDeviceJobs, options.Args(args))
		return err
	},
}

func scheduleOneDeviceJobs(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return schedule.ScheduleJobs(ctx, log, via, device)
}
