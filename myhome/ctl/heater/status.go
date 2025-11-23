package heater

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <device>",
	Short: "Display heater script configuration on a Shelly device",
	Long:  "Show the current KVS configuration and script status for the heater.js script.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doStatus, nil)
		return err
	},
}

func doStatus(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	fmt.Printf("Heater configuration on %s:\n", sd.Name())

	// Get all KVS entries with script/heater/ prefix
	fmt.Printf("\n=== KVS Configuration ===\n")

	kvsConfig := make(map[string]string)
	for _, key := range heaterKVSKeys {
		value, err := kvs.GetValue(ctx, log, via, sd, key)
		if err != nil {
			// Key doesn't exist, skip it
			continue
		}
		if value != nil && value.Value != "" {
			kvsConfig[key] = value.Value
			fmt.Printf("  %s: %s\n", key, value.Value)
		}
	}

	if len(kvsConfig) == 0 {
		fmt.Printf("  (no heater configuration found)\n")
	}

	// Get script status
	fmt.Printf("\n=== Script Status ===\n")
	scriptName := "heater.js"
	status, err := pkgscript.ScriptStatus(ctx, sd, via, scriptName)
	if err != nil {
		if strings.Contains(err.Error(), "script not found") {
			fmt.Printf("  Script: not installed\n")
		} else {
			fmt.Printf("  Error getting script status: %v\n", err)
		}
	} else {
		fmt.Printf("  Script: %s\n", scriptName)
		fmt.Printf("  ID: %d\n", status.Id)
		fmt.Printf("  Running: %v\n", status.Running)
		if len(status.Errors) > 0 {
			fmt.Printf("  Errors: %v\n", status.Errors)
		}
		if status.ErrorMessage != "" {
			fmt.Printf("  Error Message: %s\n", status.ErrorMessage)
		}
	}

	// Get Script.storage values (cooling-rate, forecast-url, last-cheap-end)
	fmt.Printf("\n=== Script Storage (Runtime State) ===\n")
	// Note: Script.storage is internal to the script and not directly accessible via RPC
	// We can only show what's in KVS
	fmt.Printf("  (Script.storage values are internal to the script)\n")
	fmt.Printf("  These include: cooling-rate, forecast-url, last-cheap-end\n")

	result := map[string]interface{}{
		"device":      sd.Name(),
		"kvs_config":  kvsConfig,
		"script_name": scriptName,
	}

	if status != nil {
		result["script_status"] = map[string]interface{}{
			"id":      status.Id,
			"running": status.Running,
			"errors":  status.Errors,
		}
	}

	if options.Flags.Json {
		bytes, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(bytes))
	}

	return result, nil
}
