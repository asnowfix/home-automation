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
	Cmd.AddCommand(createCtl)
}

var createCtl = &cobra.Command{
	Use:   "create",
	Short: "Create a named JavaScript script on the given Shelly device(s)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, doCreate, args)
	},
}

func doCreate(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (*shelly.Device, error) {
	name := args[0]

	out, err := device.CallE(via, "Script", "Create", &script.Configuration{
		Name:   name,
		Enable: false,
	})
	if err != nil {
		log.Error(err, "Unable to create script", "name", name)
		return nil, err
	}
	status := out.(*script.Status)
	s, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(s))

	return device, nil
}
