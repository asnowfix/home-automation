package jobs

import (
	"context"
	"hlog"
	"homectl/options"
	"schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/types"
)

var showCtl = &cobra.Command{
	Use:   "show",
	Short: "Show Shelly devices scheduled jobs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		return shelly.Foreach(cmd.Context(), log, args, options.Via, showOneDeviceJobs, args)
	},
}

func showOneDeviceJobs(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := schedule.ShowJobs(ctx, log, via, device)
	if err != nil {
		log.Error(err, "Unable to set Scheduled JobSpec: %v", err)
		return nil, err
	}

	jobs := out.(*schedule.Scheduled)
	log.Info("Scheduled", "jobs", jobs)

	return jobs, nil
}
