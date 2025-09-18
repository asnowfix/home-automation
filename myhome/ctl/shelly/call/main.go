package call

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/types"
)

var Cmd = &cobra.Command{
	Use:   "call <device-id> <method> [params-json]",
	Short: "Make direct RPC calls to Shelly devices",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		deviceId := args[0]
		method := args[1]
		var params interface{}
		
		if len(args) == 3 {
			if err := json.Unmarshal([]byte(args[2]), &params); err != nil {
				return fmt.Errorf("failed to parse params JSON: %v", err)
			}
		}
		
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, deviceId, options.Via, callOneDevice, []string{method, args[2]})
		return err
	},
}

func callOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	
	method := args[0]
	var params interface{}
	
	if len(args) > 1 && args[1] != "" {
		if err := json.Unmarshal([]byte(args[1]), &params); err != nil {
			return nil, fmt.Errorf("failed to parse params JSON: %v", err)
		}
	}
	
	log.Info("Calling method", "device", sd.Id(), "method", method, "params", params)
	
	// Make the RPC call
	result, err := sd.CallE(ctx, via, method, params)
	if err != nil {
		return nil, fmt.Errorf("RPC call failed: %v", err)
	}
	
	// Pretty print the result if we got one
	if result != nil {
		resultJson, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Printf("%+v\n", result)
			return result, nil
		}
		fmt.Println(string(resultJson))
		return result, nil
	}
	
	fmt.Println("Call completed successfully but returned no result")
	return nil, nil
}
