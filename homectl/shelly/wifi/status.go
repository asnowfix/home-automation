package wifi

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

	"pkg/shelly"
	"pkg/shelly/types"
	"pkg/shelly/wifi"

	"homectl/options"
)

func init() {
	Cmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Shelly devices WiFi status",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, oneDeviceStatus, options.Args(args))
		return err
	},
}

func oneDeviceStatus(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, wifi.GetStatus.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get WiFi status")
		return nil, err
	}
	status, ok := out.(*wifi.Status)
	if !ok {
		log.Error(nil, "Invalid WiFi status type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid WiFi status type %T", out)
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
