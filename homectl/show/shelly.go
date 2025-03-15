package show

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"mymqtt"
	"mynet"
	"net"
	"pkg/shelly"
	"pkg/shelly/types"
	"reflect"

	"homectl/options"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var direct bool
var long bool

func init() {
	showShellyCmd.PersistentFlags().BoolVarP(&direct, "direct", "d", false, "contact device directly, do not query the MyHome server")
	showShellyCmd.PersistentFlags().BoolVarP(&direct, "long", "l", false, "long output")
}

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		shelly.Init(cmd.Context(), options.Flags.MqttTimeout)

		var out any
		var err error
		var device *myhome.Device

		identifier := args[0]
		log := hlog.Logger

		if direct {
			var via types.Channel
			var sd *shelly.Device
			ip := net.ParseIP(identifier)
			if ip == nil || mynet.IsSameNetwork(log, ip) != nil {
				sd = shelly.NewDeviceFromMqttId(cmd.Context(), log, identifier, mymqtt.GetClient(cmd.Context()))
			} else {
				sd = shelly.NewDeviceFromIp(cmd.Context(), log, net.IP(device.Host))
			}
			// TODO implement
			//sd := shelly.NewDeviceFromDeviceSummary(cmd.Context(), log, device)

			var device myhome.Device
			device.UpdateFromShelly(cmd.Context(), sd, via)
		} else {
			out, err = myhome.TheClient.CallE(cmd.Context(), myhome.DeviceShow, identifier)
			device = out.(*myhome.Device)
		}
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))

		var show any = device
		if !long {
			show = device.DeviceSummary
		}

		var s []byte
		if options.Flags.Json {
			s, err = json.Marshal(show)
		} else {
			s, err = yaml.Marshal(show)
		}
		if err != nil {
			return err
		}
		fmt.Println(string(s))
		return nil
	},
}
