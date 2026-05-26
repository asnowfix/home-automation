package script

import (
	"context"
	"fmt"
	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"reflect"
	myScript "github.com/asnowfix/home-automation/internal/shelly/scripts"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(statusCtl)
}

var statusCtl = &cobra.Command{
	Use:   "status",
	Short: "Report status of all scripts loaded on the given Shelly device(s)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doStatus, options.Args(args))
		return err
	},
}

func doStatus(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	var err error
	if len(args) > 0 {
		// Single script status - return as-is
		out, err := script.ScriptStatus(ctx, sd, via, args[0])
		if err != nil {
			hlog.Logger.Error(err, "Unable to get script status")
			return nil, err
		}
		options.PrintResult(out, sd.Name())
		return out, nil
	}

	// All scripts status - enhance with version information
	enhancedStatuses, err := myScript.DeviceStatusWithVersion(ctx, log, sd, via)
	if err != nil {
		hlog.Logger.Error(err, "Unable to get scripts status")
		return nil, err
	}

	options.PrintResult(enhancedStatuses, sd.Name())
	return enhancedStatuses, nil
}
