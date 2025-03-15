package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"mymqtt"
	"reflect"

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
	config, ok := out.(*mqtt.Config)
	if !ok {
		log.Error(nil, "Invalid MQTT config type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid MQTT config type %T", out)
	}
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

	configReq := mqtt.SetConfigRequest{
		Config: *config,
	}

	payload, _ := json.Marshal(configReq)
	log.Info("new MQTT config", "config", string(configStr))

	out, err = device.CallE(ctx, via, string(mqtt.SetConfig), payload)
	if err != nil {
		log.Error(err, "Unable to set MQTT config")
		return nil, err
	}
	res, ok := out.(*mqtt.SetConfigResponse)
	if !ok {
		log.Error(nil, "Invalid MQTT set config response type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid MQTT set config response type %T", out)
	}
	if res.Result.RestartRequired {
		device.CallE(ctx, via, string(shelly.Reboot), nil)
	}
	return out, nil
}
