package show

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
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
				sd = shelly.NewHttpDevice(log, ip)
				via = types.ChannelHttp
			} else {
				sd = shelly.NewMqttDevice(log, identifier, options.MqttClient)
				via = types.ChannelMqtt
			}
			var device myhome.Device
			myhome.UpdateDeviceFromShelly(&device, sd, via)
		} else {
			out, err = options.MyHomeClient.CallE("device.show", identifier)
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
