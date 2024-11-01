package toggle

import (
	"devices/shelly"
	"devices/shelly/types"
	"hlog"
	"log"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "toggle",
	Short: "Toggle switch devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init()

		ch := types.ChannelHttp
		if !useHttpChannel {
			ch = types.ChannelMqtt
		}
		return shelly.Foreach(args, ch, func(via types.Channel, device *shelly.Device) (*shelly.Device, error) {
			_, err := device.CallE(ch, "Switch", "Toggle", nil)
			if err != nil {
				log.Default().Printf("Failed to toggle device %s: %v", device.Id_, err)
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
