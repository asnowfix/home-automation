package script

import (
	"hlog"
	"homectl/shelly/options"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(listCtl)
}

var listCtl = &cobra.Command{
	Use:   "list",
	Short: "Report status of every scripts loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, doList, args)
	},
}

func doList(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	return script.List(device, via)
}
