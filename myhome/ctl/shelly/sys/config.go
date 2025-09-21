package sys

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var flags struct {
	EcoMode bool
	Name    string
}

func init() {
	Cmd.AddCommand(configCmd)

	configCmd.Flags().BoolVarP(&flags.EcoMode, "ecomode", "E", false, "Set eco mode")
	configCmd.Flags().StringVarP(&flags.Name, "name", "N", "", "Device name")
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

func oneDeviceConfig(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	config, err := system.GetConfig(ctx, via, sd)
	if err != nil {
		return nil, fmt.Errorf("unable to get config: %v", err)
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

	if changed {
		out, err := system.SetConfig(ctx, via, sd, config)
		if err != nil {
			return nil, fmt.Errorf("unable to set config: %v", err)
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
