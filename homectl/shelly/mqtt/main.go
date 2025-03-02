package mqtt

import (
	"context"
	"encoding/json"
	"hlog"
	"mymqtt"

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
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		before, after := hopts.SplitArgs(args)
		return shelly.Foreach(cmd.Context(), log, before, options.Via, setupOneDevice, after)
	},
}

func setupOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, string(mqtt.GetConfig), nil)
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

	out, err = device.CallE(ctx, via, string(mqtt.GetStatus), nil)
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

	mc, err := mymqtt.GetClientE(ctx)
	if err != nil {
		log.Error(err, "Unable to get MQTT client to reach device")
		return nil, err
	}
	config.Server = mc.BrokerUrl().String()

	configStr, _ = json.Marshal(config)
	log.Info("new MQTT config", "config", string(configStr))

	out, err = device.CallE(ctx, via, string(mqtt.SetConfig), config)
	if err != nil {
		log.Error(err, "Unable to set MQTT config")
		return nil, err
	}
	res := out.(*mqtt.ConfigResults)
	if res.Result.RestartRequired {
		device.CallE(ctx, via, string(shelly.Reboot), nil)
	}
	return out, nil
}
