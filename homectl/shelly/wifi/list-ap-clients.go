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
	Cmd.AddCommand(listApClientsCmd)
}

var listApClientsCmd = &cobra.Command{
	Use:   "list-clients",
	Short: "Show Shelly devices WiFi Access Point clients",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, oneDeviceListApClients, options.Args(args))
		return err
	},
}

func oneDeviceListApClients(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, wifi.ListAPClients.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get WiFi Access Point clients")
		return nil, err
	}
	result, ok := out.(*wifi.ListAPClientsResult)
	if !ok {
		log.Error(nil, "Invalid WiFi Access Point clients type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid WiFi Access Point clients type %T", out)
	}
	if options.Flags.Json {
		s, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(result)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}

	return out, nil
}
