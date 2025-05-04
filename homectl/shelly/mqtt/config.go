package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"mymqtt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/types"

	"homectl/options"
)

func init() {
	Cmd.AddCommand(configCmd)
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Get & set Shelly devices MQTT configuration",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, configOneDevice, options.Args(args))
		return err
	},
}

func configOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, mqtt.GetConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT config")
		return nil, err
	}
	config, ok := out.(*mqtt.Config)
	if !ok {
		log.Error(nil, "Invalid MQTT config type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid MQTT config type %T (should be *mqtt.Config)", out)
	}

	// Update config from flags
	config.Enable = true
	config.RpcNotifs = true
	config.StatusNotifs = true

	mc, err := mymqtt.GetClientE(ctx)
	if err != nil {
		log.Error(err, "Unable to get MQTT client to reach device")
		return nil, err
	}
	config.Server = mc.BrokerUrl().String()

	out, err = device.CallE(ctx, via, mqtt.SetConfig.String(), mqtt.SetConfigRequest{
		Config: *config,
	})
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

	out, err = device.CallE(ctx, via, mqtt.GetConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT config")
		return nil, err
	}
	config, ok = out.(*mqtt.Config)
	if !ok {
		log.Error(nil, "Invalid MQTT config type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid MQTT config type %T (should be *mqtt.Config)", out)
	}

	// Print config
	if options.Flags.Json {
		s, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(config)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}

	return out, nil
}
