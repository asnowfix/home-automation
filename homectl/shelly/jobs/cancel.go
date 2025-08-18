package jobs

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/types"
	"reflect"
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
		myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, cancelOneDeviceJob, options.Args(args))
		return nil
	},
}

func cancelOneDeviceJob(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	if cancelFlag.all {
		out, err := schedule.CancelAllJobs(ctx, log, via, sd)
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return out, nil
	} else {
		out, err := schedule.CancelJob(ctx, log, via, sd, cancelFlag.id)
		if err != nil {
			log.Error(err, "Unable to cancel Scheduled Job: %v", err)
			return nil, err
		}
		return out, nil
	}
}
