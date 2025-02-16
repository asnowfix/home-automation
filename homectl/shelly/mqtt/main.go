package mqtt

import (
	"context"
	"encoding/json"
	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/types"

	hopts "homectl/options"

	"homectl/shelly/options"
)

var Cmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Set Shelly devices MQTT configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		shelly.Init(log, hopts.Flags.MqttTimeout)
		ctx, cancel := hopts.InterruptibleContext()
		defer cancel()

		via := types.ChannelMqtt
		if options.UseHttpChannel {
			via = types.ChannelHttp
		}
		return shelly.Foreach(ctx, log, hopts.MqttClient, hopts.Devices, via, setupOneDevice, args)
	},
}

func setupOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, "Mqtt", "GetConfig", nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT config")
		return nil, err
	}
	config := out.(*mqtt.Config)
	configStr, err := json.Marshal(config)
	if err != nil {
		log.Info("Unable to marshal MQTT config: %v", err)
		return nil, err
	}
	log.Info("initial MQTT", "config", configStr)

	out, err = device.CallE(ctx, via, "Mqtt", "GetStatus", nil)
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
	config.Server = hopts.MqttClient.BrokerUrl().String()

	configStr, _ = json.Marshal(config)
	log.Info("new MQTT config", "config", string(configStr))

	out, err = device.CallE(ctx, via, "Mqtt", "SetConfig", config)
	if err != nil {
		log.Error(err, "Unable to set MQTT config")
		return nil, err
	}
	res := out.(*mqtt.ConfigResults)
	if res.Result.RestartRequired {
		device.CallE(ctx, via, "Shelly", "Reboot", nil)
	}
	return out, nil
}
