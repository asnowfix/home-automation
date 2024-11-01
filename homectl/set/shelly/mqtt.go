package shelly

import (
	"encoding/json"
	"fmt"
	"hlog"
	"log"
	"mymqtt"

	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/mqtt"
	"devices/shelly/types"
)

var mqttCmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Set Shelly devices MQTT configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init()
		via := types.ChannelMqtt
		if useHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(args, via, setupOneDevice)
	},
}

func setupOneDevice(via types.Channel, device *shelly.Device) (*shelly.Device, error) {
	out, err := device.CallE(via, "Mqtt", "GetConfig", nil)
	if err != nil {
		log.Default().Printf("Unable to get MQTT config: %v", err)
		return nil, err
	}
	config := out.(*mqtt.Configuration)
	configStr, err := json.Marshal(config)
	if err != nil {
		log.Default().Printf("Unable to marshal MQTT config: %v", err)
		return nil, err
	}
	log.Default().Printf("initial MQTT config: %v", string(configStr))

	out, err = device.CallE(via, "Mqtt", "GetStatus", nil)
	if err != nil {
		log.Default().Printf("Unable to get MQTT status: %v", err)
		return nil, err
	}
	status := out.(*mqtt.Status)
	statusStr, _ := json.Marshal(status)
	log.Default().Printf("initial MQTT status: %v", string(statusStr))

	config.Enable = true
	config.RpcNotifs = true
	config.StatusNotifs = true
	broker := mymqtt.Broker(false)
	// Shelly MQTT Server is formatted like host:port
	config.Server = fmt.Sprintf("%s:%s", broker.Hostname(), broker.Port())

	configStr, _ = json.Marshal(config)
	log.Default().Printf("new MQTT config: %v", string(configStr))

	out, err = device.CallE(via, "Mqtt", "SetConfig", config)
	if err != nil {
		log.Default().Printf("Unable to set MQTT config: %v", err)
		return nil, err
	}
	res := out.(*mqtt.ConfigResults)
	if res.Result.RestartRequired {
		device.CallE(via, "Shelly", "Reboot", nil)
	}
	return device, nil
}
