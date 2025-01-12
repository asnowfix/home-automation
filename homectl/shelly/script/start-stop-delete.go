package script

import (
	"hlog"
	hopts "homectl/options"
	"homectl/shelly/options"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(startCtl)
	startCtl.MarkFlagRequired("id")
}

var startCtl = &cobra.Command{
	Use:   "start",
	Short: "Start a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, doStartStop, []string{"Start"})
	},
}

func init() {
	Cmd.AddCommand(stopCtl)
	stopCtl.MarkFlagRequired("id")
}

var stopCtl = &cobra.Command{
	Use:   "stop",
	Short: "Stop a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, doStartStop, []string{"Stop"})
	},
}

func init() {
	Cmd.AddCommand(deleteCtl)
	deleteCtl.MarkFlagRequired("id")
}

var deleteCtl = &cobra.Command{
	Use:   "delete",
	Short: "Delete a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, hopts.MqttClient, hopts.Devices, via, doStartStop, []string{"Delete"})
	},
}

func doStartStop(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	operation := args[0]
	return script.StartStopDelete(via, device, flags.Name, flags.Id, operation)
}
