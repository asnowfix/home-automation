package mqtt

import (
	"encoding/json"
	"fmt"
	"hlog"
	"mymqtt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/mqtt"
	"devices/shelly/types"

	"homectl/shelly/options"
)

var Cmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Set Shelly devices MQTT configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(log, strings.Split(options.DeviceNames, ","), via, setupOneDevice, args)
	},
}

func setupOneDevice(log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(via, "Mqtt", "GetConfig", nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT config")
		return nil, err
	}
	config := out.(*mqtt.Configuration)
	configStr, err := json.Marshal(config)
	if err != nil {
		log.Info("Unable to marshal MQTT config: %v", err)
		return nil, err
	}
	log.Info("initial MQTT", "config", configStr)

	out, err = device.CallE(via, "Mqtt", "GetStatus", nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT status")
		return nil, err
	}
	status := out.(*mqtt.Status)
	statusStr, _ := json.Marshal(status)
	log.Info("initial MQTT status", "status", statusStr)

	config.Enable = true
	config.RpcNotifs = true
	config.StatusNotifs = true
	broker := mymqtt.Broker(log, false)
	// Shelly MQTT Server is formatted like host:port
	config.Server = fmt.Sprintf("%s:%s", broker.Hostname(), broker.Port())

	configStr, _ = json.Marshal(config)
	log.Info("new MQTT config", "config", string(configStr))

	out, err = device.CallE(via, "Mqtt", "SetConfig", config)
	if err != nil {
		log.Error(err, "Unable to set MQTT config")
		return nil, err
	}
	res := out.(*mqtt.ConfigResults)
	if res.Result.RestartRequired {
		device.CallE(via, "Shelly", "Reboot", nil)
	}
	return out, nil
}
