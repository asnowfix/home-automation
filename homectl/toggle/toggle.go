package toggle

import (
	"devices/shelly"
	"devices/shelly/types"
	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var toggleSwitchId int

func init() {
	Cmd.Flags().IntVarP(&toggleSwitchId, "switch", "S", -1, "Use this switch ID.")
}

var Cmd = &cobra.Command{
	Use:   "toggle",
	Short: "Toggle switch devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()

		ch := types.ChannelHttp
		if !useHttpChannel {
			ch = types.ChannelMqtt
		}
		return shelly.Foreach(log, args, ch, func(log logr.Logger, via types.Channel, device *shelly.Device) (*shelly.Device, error) {
			sr := make(map[string]interface{})
			sr["id"] = toggleSwitchId
			_, err := device.CallE(ch, "Switch", "Toggle", sr)
			if err != nil {
				log.Info("Failed to toggle device %s: %v", device.Id_, err)
				return nil, err
			}
			return device, err
		})
	},
}

var useHttpChannel bool

func init() {
	Cmd.Flags().BoolVarP(&useHttpChannel, "http", "H", false, "Use HTTP channel to communicate with Shelly devices")
}
