package sys

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var flags struct {
	EcoMode        bool
	Name           string
	DebugMqtt      bool
	DebugWebSocket bool
	DebugUdp       string
}

func init() {
	Cmd.AddCommand(configCmd)

	configCmd.Flags().BoolVarP(&flags.EcoMode, "ecomode", "E", false, "Set eco mode")
	configCmd.Flags().StringVarP(&flags.Name, "name", "N", "", "Device name")

	configCmd.Flags().BoolVarP(&flags.DebugMqtt, "debug-mqtt", "M", false, "Activate debug traces over MQTT")
	configCmd.Flags().BoolVarP(&flags.DebugWebSocket, "debug-ws", "W", false, "Activate debug traces over WebSocket")
	configCmd.Flags().StringVarP(&flags.DebugUdp, "debug-udp", "U", "", "Activate debug traces over UDP (format: <host>:<port>)")
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Get or set Shelly device configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, oneDeviceConfig, options.Args(args))
		return err
	},
}

func oneDeviceConfig(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, system.GetConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get config", "device", device.String())
		return nil, err
	}
	config, ok := out.(*system.Config)
	if !ok {
		log.Error(nil, "Invalid config type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid config type %T (should be *shelly.Config)", out)
	}

	var changed bool = false
	// if configCmd.Flags().Changed("ecomode") && flags.EcoMode != config.Device.EcoMode {
	// 	config.Device.EcoMode = flags.EcoMode
	// 	changed = true
	// }

	// if configCmd.Flags().Changed("name") && flags.Name != "" && flags.Name != config.Device.Name {
	// 	config.Device.Name = flags.Name
	// 	changed = true
	// }

	// if configCmd.Flags().Changed("debug-mqtt") && flags.DebugMqtt != config.Debug.Mqtt.Enable {
	// 	config.Debug.Mqtt.Enable = flags.DebugMqtt
	// 	changed = true
	// }

	// if configCmd.Flags().Changed("debug-ws") && flags.DebugWebSocket != config.Debug.WebSocket.Enable {
	// 	config.Debug.WebSocket.Enable = flags.DebugWebSocket
	// 	changed = true
	// }

	// if configCmd.Flags().Changed("debug-udp") && flags.DebugUdp != "" && flags.DebugUdp != config.RpcUdp.DestinationAddress+":"+strconv.Itoa(int(config.RpcUdp.ListenPort)) {
	// 	config.RpcUdp.DestinationAddress = strings.Split(flags.DebugUdp, ":")[0]
	// 	port, err := strconv.Atoi(strings.Split(flags.DebugUdp, ":")[1])
	// 	if err != nil {
	// 		log.Error(err, "Invalid debug UDP address", "address", flags.DebugUdp)
	// 		return nil, err
	// 	}
	// 	config.RpcUdp.ListenPort = uint16(port)
	// 	changed = true
	// }

	if changed {
		var req system.SetConfigRequest
		req.Config = *config
		out, err := device.CallE(ctx, via, system.SetConfig.String(), &req)
		if err != nil {
			log.Error(err, "Unable to set config", "device", device.String())
			return nil, err
		}
		return out, nil
	}

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

	return config, nil
}
