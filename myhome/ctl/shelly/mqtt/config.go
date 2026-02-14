package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	mqttclient "myhome/mqtt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"pkg/devices"
	shellyapi "pkg/shelly"
	shellymqtt "pkg/shelly/mqtt"
	"pkg/shelly/shelly"
	"pkg/shelly/types"

	"myhome/ctl/options"
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

func configOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	out, err := sd.CallE(ctx, via, shellymqtt.GetConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT config")
		return nil, err
	}
	config, ok := out.(*shellymqtt.Config)
	if !ok {
		log.Error(nil, "Invalid MQTT config type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid MQTT config type %T (should be *mqtt.Config)", out)
	}

	// Update config from flags
	config.Enable = true
	config.RpcNotifs = true
	config.StatusNotifs = true

	mc, err := mqttclient.GetClientE(ctx)
	if err != nil {
		log.Error(err, "Unable to get MQTT client to reach device")
		return nil, err
	}
	config.Server = mc.BrokerUrl().String()

	out, err = sd.CallE(ctx, via, shellymqtt.SetConfig.String(), shellymqtt.SetConfigRequest{
		Config: *config,
	})
	if err != nil {
		log.Error(err, "Unable to set MQTT config")
		return nil, err
	}
	res, ok := out.(*shellymqtt.SetConfigResponse)
	if !ok {
		log.Error(nil, "Invalid MQTT set config response type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid MQTT set config response type %T", out)
	}
	if res.Result.RestartRequired {
		err := shelly.DoReboot(ctx, sd)
		if err != nil {
			log.Error(err, "Unable to reboot device")
			return nil, err
		}
	}

	out, err = sd.CallE(ctx, via, shellymqtt.GetConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT config")
		return nil, err
	}
	config, ok = out.(*shellymqtt.Config)
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
