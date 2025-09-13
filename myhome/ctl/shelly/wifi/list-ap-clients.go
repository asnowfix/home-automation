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

	"pkg/devices"
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

func oneDeviceListApClients(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	out, err := sd.CallE(ctx, via, wifi.ListAPClients.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get WiFi Access Point clients")
		return nil, err
	}
	result, ok := out.(*wifi.ListAPClientsResult)
	if !ok {
		log.Error(nil, "Invalid WiFi Access Point clients type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid WiFi Access Point clients type %T", out)
	}

	clients := make([]wifi.APClient, len(result.APClients))
	for i, client := range result.APClients {
		devices, err := myhome.TheClient.LookupDevices(ctx, client.MAC)
		if err == nil {
			device := (*devices)[0]
			client.Name = device.Name()
			client.Id = device.Id()
		}
		clients[i] = client
	}
	if options.Flags.Json {
		s, err := json.Marshal(clients)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(clients)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(s))
	}

	return clients, nil
}
