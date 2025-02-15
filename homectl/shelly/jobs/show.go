package jobs

import (
	"hlog"
	hopts "homectl/options"
	"homectl/shelly/options"
	"schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/types"
)

var showCtl = &cobra.Command{
	Use:   "show",
	Short: "Show Shelly devices scheduled jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, showOneDeviceJobs, args)
	},
}

func showOneDeviceJobs(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := schedule.ShowJobs(via, device)
	if err != nil {
		log.Error(err, "Unable to set Scheduled JobSpec: %v", err)
		return nil, err
	}

	jobs := out.(*schedule.Scheduled)
	log.Info("Scheduled", "jobs", jobs)

	return jobs, nil
}
