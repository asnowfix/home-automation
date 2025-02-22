package toggle

import (
	"context"
	"hlog"
	hopts "homectl/options"
	"pkg/shelly"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var toggleSwitchId int

func init() {
	Cmd.Flags().IntVarP(&toggleSwitchId, "switch", "S", 0, "Use this switch ID.")
}

var Cmd = &cobra.Command{
	Use:   "toggle",
	Short: "Toggle switch devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger

		ch := types.ChannelHttp
		if !useHttpChannel {
			ch = types.ChannelMqtt
		}
		return shelly.Foreach(cmd.Context(), log, hopts.MqttClient, hopts.Devices, ch, func(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
			sr := make(map[string]interface{})
			sr["id"] = toggleSwitchId
			out, err := device.CallE(ctx, ch, string(sswitch.Toggle), sr)
			if err != nil {
				log.Info("Failed to toggle device %s: %v", device.Id_, err)
				return nil, err
			}
			return out, err
		}, args)
	},
}

var useHttpChannel bool

func init() {
	Cmd.Flags().BoolVarP(&useHttpChannel, "http", "H", false, "Use HTTP channel to communicate with Shelly devices")
}
