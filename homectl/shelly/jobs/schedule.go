package jobs

import (
	"homectl/shelly/options"
	"strings"

	"hlog"
	"schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/types"
)

var scheduleCtl = &cobra.Command{
	Use:   "schedule",
	Short: "Configure Shelly devices scheduled jobs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, scheduleOneDeviceJobs, args)
	},
}

func scheduleOneDeviceJobs(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return schedule.ScheduleJobs(via, device)
}
