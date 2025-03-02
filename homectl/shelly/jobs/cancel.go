package jobs

import (
	"context"
	"fmt"
	"hlog"
	hopts "homectl/options"
	"homectl/shelly/options"
	"pkg/shelly"
	"pkg/shelly/types"
	"schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var cancelFlag struct {
	id  uint32
	all bool
}

func init() {
	cancelCtl.Flags().Uint32VarP(&cancelFlag.id, "id", "i", 0, "Scheduled job ID to cancel.")
	cancelCtl.Flags().BoolVarP(&cancelFlag.all, "all", "a", false, "Cancel every scheduled job Id on given device(s).")
	cancelCtl.MarkFlagsMutuallyExclusive("id", "all")
}

var cancelCtl = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel scheduled jobs on Shelly devices",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := hopts.SplitArgs(args)
		shelly.Foreach(cmd.Context(), log, before, options.Via, cancelOneDeviceJob, after)
		return nil
	},
}

func cancelOneDeviceJob(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	if cancelFlag.all {
		out, err := schedule.CancelAllJobs(ctx, log, via, device)
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return out, nil
	} else if cancelFlag.id < 0 {
		return nil, fmt.Errorf("no job ID provided to cancel")
	} else {
		out, err := schedule.CancelJob(ctx, log, via, device, cancelFlag.id)
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return out, nil
	}
}
