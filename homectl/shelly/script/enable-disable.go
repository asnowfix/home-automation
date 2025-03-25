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
	Short: "Enable (creating it if necessary) a named JavaScript script on the given Shelly device(s)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doEnableDisable, []string{"true"})
	},
}

var disableCtl = &cobra.Command{
	Use:   "disable",
	Short: "Disable (creating it if necessary) a named JavaScript script on the given Shelly device(s)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doEnableDisable, []string{"false"})
	},
}

func doEnableDisable(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	enable, err := strconv.ParseBool(args[0])
	if err != nil {
		log.Error(err, "Invalid enable/disable argument", "arg", args[0])
		return nil, err
	}
	return script.EnableDisable(ctx, via, device, flags.Name, flags.Id, enable)
}
