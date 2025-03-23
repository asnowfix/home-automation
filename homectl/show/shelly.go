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
		ctx := cmd.Context()

		if direct {
			var sd *shelly.Device
			ip := net.ParseIP(identifier)
			if ip == nil || mynet.IsSameNetwork(log, ip) != nil {
				sd = shelly.NewDeviceFromMqttId(ctx, log, identifier, mymqtt.GetClient(cmd.Context()))
			} else {
				sd = shelly.NewDeviceFromIp(ctx, log, ip)
			}
			// TODO implement
			//sd := shelly.NewDeviceFromDeviceSummary(ctx, log, device)

			device, err = myhome.NewDeviceFromShellyDevice(ctx, log, sd)
			if err != nil {
				return err
			}
		} else {
			out, err = myhome.TheClient.CallE(ctx, myhome.DeviceShow, identifier)
			if err != nil {
				return err
			}
			var ok bool
			device, ok = out.(*myhome.Device)
			if !ok {
				return fmt.Errorf("expected myhome.Device, got %T", out)
			}
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
