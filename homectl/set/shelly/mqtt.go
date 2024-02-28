package shelly

import (
	"devices"
	"log"
	"reflect"

	hlog "homectl/log"

	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/mqtt"
)

var mqttCmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Set Shelly devices MQTT configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init()
		shelly.Init()

		if len(args) > 0 {
			for _, name := range args {
				log.Default().Printf("Looking for Shelly device %v", name)
				host, err := devices.Lookup(name)
				if err != nil {
					log.Default().Print(err)
					return err
				}
				device := shelly.NewDeviceFromIp(host.Ip()).Init()
				mqtt.Setup(device)
			}
		} else {
			log.Default().Printf("Looking for any Shelly device")
			devices, err := shelly.FindDevicesFromMdns()
			if err != nil {
				log.Default().Print(err)
				return err
			}
			log.Default().Printf("Found %v devices '%v'\n", len(devices), reflect.TypeOf(devices))
			for _, device := range devices {
				mqtt.Setup(device)
			}
		}
		return nil
	},
}
