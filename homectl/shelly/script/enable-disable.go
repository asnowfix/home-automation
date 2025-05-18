package script

import (
	"context"
	"hlog"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"strconv"

	"homectl/options"

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

func doEnableDisable(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	scriptName := args[0]
	enable, err := strconv.ParseBool(args[1])
	if err != nil {
		log.Error(err, "Invalid enable/disable argument", "arg", args[1])
		return nil, err
	}

	return script.EnableDisable(ctx, via, device, scriptName, enable)
}
