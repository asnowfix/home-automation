package script

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(evalCtl)
	evalCtl.MarkFlagRequired("id")
}

var evalCtl = &cobra.Command{
	Use:   "eval <device-name-or-ip> <script-name> <javascript-code>",
	Short: "Evaluate the given JavaScript code on the given Shelly device(s)",
	Long: `Evaluate JavaScript code in the context of a running script on a Shelly device.

This uses Script.Eval to execute code within the script's context, allowing you to:
- Inspect script variables and state
- Call script functions
- Debug script behavior

Examples:
  # Check a variable value in heater.js
  myhome ctl shelly script eval radiateur-salon-hiver heater.js "CONFIG.setpoint"
  
  # Call a function
  myhome ctl shelly script eval radiateur-salon-hiver heater.js "getFilteredTemp()"
  
  # Inspect state
  myhome ctl shelly script eval radiateur-salon-hiver heater.js "STATE"`,
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doEval, args[1:])
		return err
	},
}

func doEval(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	return script.EvalInDevice(ctx, via, sd, args[0], args[1])
}
