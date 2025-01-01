package jobs

import (
	"homectl/shelly/options"
	"strings"

	"encoding/json"
	"fmt"
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

func scheduleOneDeviceJobs(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*shelly.Device, error) {
	jobs := make([]schedule.Job, 0)

	out, err := schedule.ScheduleJob(via, device, schedule.JobSpec{
		// daily cron timepec for sunrise - 1 hour
		Timespec: "@sunrise-1h",
		Enable:   true,
		Calls: []schedule.JobCall{{
			Method: "Shelly.Reboot",
			Params: nil,
		}},
	})
	if err != nil {
		log.Error(err, "Unable to schedule daily reboot")
	} else {
		jobs = append(jobs, *out.(*schedule.Job))
	}

	out, err = schedule.ScheduleJob(via, device, schedule.JobSpec{
		// weekly cron timpespec for Sunday at sunrise - 2 hours
		Timespec: "@sunrise-2h * * SUN",
		Enable:   true,
		Calls: []schedule.JobCall{{
			Method: "Shelly.Update",
			Params: map[string]string{
				"stage": "stable",
			},
		}},
	})
	if err != nil {
		log.Error(err, "Unable to schedule weekly device update (stable)")
	} else {
		jobs = append(jobs, *out.(*schedule.Job))
	}

	s, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	// If the list of scheduled jobs is empty, return an error
	if len(jobs) == 0 {
		return nil, fmt.Errorf("No jobs scheduled")
	} else {
		return device, nil
	}
}
