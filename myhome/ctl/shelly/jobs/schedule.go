package jobs

import (
	"context"
	"fmt"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"reflect"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/pkg/shelly/schedule"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

var scheduleCtl = &cobra.Command{
	Use:   "schedule",
	Short: "Configure Shelly devices scheduled jobs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, scheduleOneDeviceJobs, options.Args(args))
		return err
	},
}

func scheduleOneDeviceJobs(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	return schedule.ScheduleJobs(ctx, log, via, sd)
}
