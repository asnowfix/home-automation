package new

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"net"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"github.com/jackpal/gateway"
)

// options for STA
var staEssid string
var staPasswd string

// options for AP
var apPasswd string

// options for MQTT
var mqttBroker string
var mqttPort int

func init() {
	Cmd.Flags().StringVar(&staEssid, "sta-essid", "", "STA ESSID")
	Cmd.Flags().StringVar(&staPasswd, "sta-passwd", "", "STA Password")
	Cmd.Flags().StringVar(&apPasswd, "ap-passwd", "", "AP Password")
	Cmd.Flags().StringVar(&mqttBroker, "mqtt-broker", "", "MQTT broker address")
	Cmd.Flags().IntVar(&mqttPort, "mqtt-port", 1883, "MQTT broker port")
	Cmd.AddCommand()
}

var Cmd = &cobra.Command{
	Use:   `new <device_name>`,
	Short: "Shelly devices features",
	Long: `Configure a new Shelly device with the specified settings.

Arguments:
  <device_name>    Name to assign to the Shelly device`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		var ip net.IP

		if len(args) > 1 {
			ip = net.ParseIP(args[1])
			if ip == nil {
				return fmt.Errorf("invalid IP address: %s", args[1])
			}
		} else {
			gateways, err := gateway.DiscoverGateways()
			if err != nil {
				return err
			}
			for _, g := range gateways {
				if g.String() == "192.168.33.1" || g.String() == "192.168.34.1" {
					ip = g.To4()
					break
				}
			}
			if ip == nil {
				return fmt.Errorf("no gateway is a shelly device to be configured")
			}
		}

		// If we are connected to a shelly device
		myhome.Foreach(cmd.Context(), hlog.Logger, ip.String(), types.ChannelHttp, func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
			sd, ok := device.(*shellyapi.Device)
			if !ok {
				return nil, fmt.Errorf("expected types.Device, got %T", device)
			}
			// - set device name to args[0]
			_, err := system.DoSetName(ctx, sd, name)
			if err != nil {
				return nil, err
			}
			// - set Wifi STA ESSID & passwd
			if staEssid != "" && staPasswd != "" {
				_, err = wifi.SetSta(ctx, sd, staEssid, staPasswd)
				if err != nil {
					return nil, err
				}
			}

			// - set Wifi AP password to arg[1]
			if apPasswd != "" {
				_, err = wifi.SetAp(ctx, sd, "", apPasswd)
				if err != nil {
					return nil, err
				}
			}
			// - configure MQTT server to arg[2] (if provided)
			if mqttBroker != "" {
				_, err = mqtt.SetServer(ctx, sd, mqttBroker+":"+strconv.Itoa(mqttPort))
				if err != nil {
					return nil, err
				}
			}
			// reboot device
			err = shelly.DoReboot(cmd.Context(), sd)
			if err != nil {
				return nil, err
			}
			return nil, nil
		}, []string{args[0]})

		return nil
	},
}
