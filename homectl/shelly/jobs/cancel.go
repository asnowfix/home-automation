package jobs

import (
	"fmt"
	"hlog"
	"homectl/shelly/options"
	"pkg/shelly"
	"pkg/shelly/types"
	"schedule"
	"strings"

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
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelHttp
		if !options.UseHttpChannel {
			via = types.ChannelMqtt
		}

		shelly.Foreach(log, options.MqttClient, strings.Split(options.DeviceNames, ","), via, cancelOneDeviceJob, args)
		return nil
	},
}

func cancelOneDeviceJob(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	if cancelFlag.all {
		out, err := schedule.CancelAllJobs(via, device)
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return out, nil
	} else if cancelFlag.id < 0 {
		return nil, fmt.Errorf("No job ID provided to cancel")
	} else {
		out, err := schedule.CancelJob(via, device, uint32(cancelFlag.id))
		if err != nil {
			log.Error(err, "Unable to cancel all Scheduled Jobs: %v", err)
			return nil, err
		}
		return out, nil
	}
}
