package setup

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"net"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/script"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"strconv"
	"time"

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
	Use:   `setup <device_name> <device_ip>`,
	Short: "Shelly devices features",
	Long: `Setup a new Shelly device with the specified settings.

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
        // Use a long-lived context decoupled from the global command timeout
        longCtx := options.CommandLineContext(context.Background(), hlog.Logger, 2*time.Minute)
		myhome.Foreach(longCtx, hlog.Logger, ip.String(), types.ChannelHttp, func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
			sd, ok := device.(*shellyapi.Device)
			if !ok {
				return nil, fmt.Errorf("expected types.Device, got %T", device)
			}
			// - set device name to args[0]
			_, err := system.SetName(ctx, sd, name)
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
			err = shelly.DoReboot(ctx, sd)
			if err != nil {
				return nil, err
			}

			// load watchdog.js as script #1
			// - Check if watchdog.js is already loaded as script #1
			loaded, err := script.ListLoaded(ctx, via, sd)
			if err != nil {
				return nil, err
			}
			ok = false
			for _, s := range loaded {
				if s.Name == "watchdog.js" {
					log.Info("watchdog.js is already loaded")
					if s.Running && s.Id == 1 {
						log.Info("watchdog.js is already running as script #1")
						continue
					}
					err := fmt.Errorf("watchdog.js is already loaded but not running as script #1 on device %s", sd.Id())
					log.Error(err, "watchdog.js improper configuration", "device", sd.Id(), "script_id", s.Id)
					return nil, err
				}
			}
			if !ok {
				// Not already in place: upload, ...
				_, err = script.Upload(ctx, via, sd, "watchdog.js")
				if err != nil {
					return nil, err
				}
				// ...enable (auto-restart at boot, ...
				_, err = script.EnableDisable(ctx, via, sd, "watchdog.js", true)
				if err != nil {
					return nil, err
				}
				// ...and start it.
				_, err = script.StartStopDelete(ctx, via, sd, "watchdog.js", script.Start)
				if err != nil {
					return nil, err
				}
			}

			return nil, nil
		}, []string{args[0]})

		return nil
	},
}
