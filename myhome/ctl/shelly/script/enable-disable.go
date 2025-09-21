package script

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"
	"strconv"

	"myhome/ctl/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(enableCtl)
	Cmd.AddCommand(disableCtl)
}

var enableCtl = &cobra.Command{
	Use:   "enable",
	Short: "Enable an existing script on the given Shelly device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doEnableDisable, []string{"true"})
		return err
	},
}

var disableCtl = &cobra.Command{
	Use:   "disable",
	Short: "Disable an existing script on the given Shelly device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doEnableDisable, []string{"false"})
		return err
	},
}

func doEnableDisable(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	scriptName := args[0]
	enable, err := strconv.ParseBool(args[1])
	if err != nil {
		log.Error(err, "Invalid enable/disable argument", "arg", args[1])
		return nil, err
	}

	return script.EnableDisable(ctx, via, sd, scriptName, enable)
}
