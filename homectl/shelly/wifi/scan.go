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
	Cmd.AddCommand(scanCmd)
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Show Shelly devices WiFi scan results",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, oneDeviceScan, options.Args(args))
		return err
	},
}

func oneDeviceScan(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	out, err := device.CallE(ctx, via, wifi.Scan.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get WiFi scan results")
		return nil, err
	}
	result, ok := out.(*wifi.ScanResult)
	if !ok {
		log.Error(nil, "Invalid WiFi scan results type", "type", reflect.TypeOf(out))
		return nil, fmt.Errorf("invalid WiFi scan results type %T", out)
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
