package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/types"

	"homectl/options"
)

func init() {
	Cmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Shelly devices MQTT status",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, oneDeviceStatus, options.Args(args))
		return err
	},
}

func oneDeviceStatus(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	out, err := sd.CallE(ctx, via, mqtt.GetStatus.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get MQTT status")
		return nil, err
	}
	status, ok := out.(*mqtt.Status)
	if !ok {
		log.Error(nil, "Invalid MQTT status type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid MQTT status type %T", out)
	}
	if options.Flags.Json {
		s, err := json.Marshal(status)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(status)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}

	return out, nil
}
