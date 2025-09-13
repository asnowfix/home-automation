package script

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "Report status of every scripts loaded on the given Shelly device(s)",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		if len(args) == 0 {
			scripts, err := script.ListAvailable()
			if err != nil {
				return err
			}
			return options.PrintResult(scripts)
		}
		out, err := myhome.Foreach(cmd.Context(), log, args[0], options.Via, doList, options.Args(args))
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		outs := out.([]any)
		if len(outs) != 1 {
			return fmt.Errorf("expected 1 result, got %d", len(outs))
		}
		scripts := outs[0].([]script.Status)
		return options.PrintResult(scripts)
	},
}

func doList(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	return script.DeviceStatus(ctx, sd, via)
}
