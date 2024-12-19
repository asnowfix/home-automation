package jobs

import (
	"devices/shelly"
	"devices/shelly/types"
	"fmt"
	"hlog"
	"homectl/shelly/options"
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
}

var cancelCtl = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel scheduled jobs on Shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelHttp
		if !options.UseHttpChannel {
			via = types.ChannelMqtt
		}

		shelly.Foreach(log, args, via, cancelOneDeviceJob)
		return nil
	},
}

func cancelOneDeviceJob(log logr.Logger, via types.Channel, device *shelly.Device) (*shelly.Device, error) {
	if cancelFlag.all {
		_, err := schedule.CancelAllJobs(via, device)
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return device, nil
	} else if cancelFlag.id < 0 {
		return nil, fmt.Errorf("No job ID provided to cancel")
	} else {
		_, err := schedule.CancelJob(via, device, uint32(cancelFlag.id))
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return device, nil
	}
}
