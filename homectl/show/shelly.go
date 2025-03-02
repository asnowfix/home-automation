package show

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"mymqtt"
	"net"
	"pkg/shelly"
	"pkg/shelly/types"
	"reflect"

	"homectl/options"

	"github.com/spf13/cobra"
)

var direct bool

func init() {
	showShellyCmd.PersistentFlags().BoolVarP(&direct, "direct", "d", false, "contact device directly, do not query the MyHome server")
}

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var out any
		var err error
		var device *myhome.Device

		identifier := args[0]
		log := hlog.Logger

		if direct {
			var via types.Channel
			var sd *shelly.Device
			ip := net.ParseIP(identifier)
			if ip != nil {
				sd = shelly.NewDeviceFromIp(cmd.Context(), log, ip)
				via = types.ChannelHttp
			} else {
				mc, err := mymqtt.GetClientE(cmd.Context())
				if err != nil {
					return err
				}
				sd = shelly.NewDeviceFromMqttId(cmd.Context(), log, identifier, mc)
				via = types.ChannelMqtt
			}
			var device myhome.Device
			device.UpdateFromShelly(cmd.Context(), sd, via)
		} else {
			out, err = myhome.TheClient.CallE(cmd.Context(), "device.show", identifier)
			device = out.(*myhome.Device)
		}
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		if options.Flags.Json {
			s, err := json.Marshal(device)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			fmt.Println(device)
		}
		return nil
	},
}
