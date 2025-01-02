package script

import (
	"devices/shelly"
	"devices/shelly/script"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/shelly/options"
	"strings"

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
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, doStartStop, []string{"Start"})
	},
}

func init() {
	Cmd.AddCommand(stopCtl)
	stopCtl.MarkFlagRequired("id")
}

var stopCtl = &cobra.Command{
	Use:   "start",
	Short: "Stop a script loaded on the given Shelly device(s)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, doStartStop, []string{"Stop"})
	},
}

func doStartStop(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*shelly.Device, error) {
	operation := args[0]
	out, err := device.CallE(via, "Script", operation, &script.Id{
		Id: uint32(flags.Id),
	})
	if err != nil {
		log.Error(err, "Unable to run operation on script", "id", flags.Id, "operation", args[0])
		return nil, err
	}
	response := out.(*script.FormerStatus)
	s, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return device, nil
}
