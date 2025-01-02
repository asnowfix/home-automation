package script

import (
	"devices/shelly"
	"devices/shelly/script"
	"devices/shelly/types"
	"hlog"
	"homectl/shelly/options"
	"strconv"
	"strings"

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
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, doEnableDisable, []string{"true"})
	},
}

var disableCtl = &cobra.Command{
	Use:   "disable",
	Short: "Disable (creating it if necessary) a named JavaScript script on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, doEnableDisable, []string{"false"})
	},
}

func doEnableDisable(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	enable, err := strconv.ParseBool(args[0])
	if err != nil {
		log.Error(err, "Invalid enable/disable argument", "arg", args[0])
		return nil, err
	}
	return script.EnableDisable(via, device, flags.Name, flags.Id, enable)
}
