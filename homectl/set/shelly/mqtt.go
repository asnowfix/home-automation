package shelly

import (
	"encoding/json"
	"log"
	"reflect"

	hlog "homectl/log"

	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/mqtt"
)

var flags struct {
	server string
}

func init() {
	mqttCmd.Flags().StringVarP(&flags.server, "server", "s", "", "MQTT server hostname.")
}

var mqttCmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Set Shelly devices MQTT configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init()
		shelly.Init()
		if len(args) > 0 {
			log.Default().Printf("Looking for Shelly device %v", args[0])
			device, err := shelly.NewDevice(args[0])
			if err != nil {
				return err
			}
			setOne(device)
		} else {
			log.Default().Printf("Looking for any Shelly device")
			devices, err := shelly.NewMdnsDevices()
			if err != nil {
				return err
			}
			log.Default().Printf("Found %v devices '%v'\n", len(*devices), reflect.TypeOf(*devices))
			for _, device := range *devices {
				setOne(device)
			}
		}
		return nil
	},
}

func setOne(device *shelly.Device) error {
	config := shelly.CallMethod(device, "Mqtt", "GetConfig", nil).(*mqtt.Configuration)
	configStr, _ := json.Marshal(config)
	log.Default().Printf("config (BEFORE): %v", string(configStr))

	config.Enable = len(flags.server) > 0
	config.Server = flags.server
	config.RpcNotifs = len(flags.server) > 0
	config.StatusNotifs = len(flags.server) > 0

	configStr, _ = json.Marshal(config)
	log.Default().Printf("config (AFTER): %v", string(configStr))

	res := shelly.CallMethod(device, "Mqtt", "SetConfig", config).(*mqtt.ConfigResults)
	if res.Result.RestartRequired {
		_, err := shelly.CallMethodE(device, "Shelly", "Reboot", config)
		return err
	}
	return nil
}
