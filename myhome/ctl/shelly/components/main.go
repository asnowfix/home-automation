package components

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/shelly"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var Cmd = &cobra.Command{
	Use:   "components",
	Short: "Give Shelly components list, config & status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doList, options.Args(args))
		return err
	},
}

func doList(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	components, err := shelly.DoGetComponents(ctx, sd)
	if err != nil {
		return nil, err
	}

	// Now show the result config
	if options.Flags.Json {
		s, err := json.Marshal(components)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(components)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}

	return components, nil
}
