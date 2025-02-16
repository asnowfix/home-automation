package jobs

import (
	"context"
	hopts "homectl/options"
	"homectl/shelly/options"

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
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)
		ctx, cancel := hopts.InterruptibleContext()
		defer cancel()

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(ctx, log, hopts.MqttClient, hopts.Devices, via, scheduleOneDeviceJobs, args)
	},
}

func scheduleOneDeviceJobs(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return schedule.ScheduleJobs(ctx, log, via, device)
}
