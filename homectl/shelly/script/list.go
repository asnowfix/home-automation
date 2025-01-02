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

func doList(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*shelly.Device, error) {
	out, err := device.CallE(via, "Script", "List", nil)
	if err != nil {
		log.Error(err, "Unable to list scripts")
		return nil, err
	}
	response := out.(*script.ListResponse)
	s, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return device, nil
}
