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
	id  int
	all bool
}

func init() {
	cancelCtl.Flags().IntVarP(&cancelFlag.id, "id", "i", -1, "Scheduled job ID to cancel.")
	cancelCtl.Flags().BoolVarP(&cancelFlag.all, "all", "a", false, "Cancel every scheduled job Id on given device(s).")
	cancelCtl.MarkFlagsMutuallyExclusive("id", "all")
}

var cancelCtl = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel scheduled jobs on Shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)

		via := types.ChannelHttp
		if !options.UseHttpChannel {
			via = types.ChannelMqtt
		}

		shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, cancelOneDeviceJob, args)
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
		return nil, fmt.Errorf("No job ID provided to cancel")
	} else {
		out, err := schedule.CancelJob(ctx, log, via, device, uint32(cancelFlag.id))
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return out, nil
	}
}
