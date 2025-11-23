package heater

import (
	"context"
	"fmt"
	"hlog"
	mhscript "internal/myhome/shelly/script"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <device>",
	Short: "Remove heater script and configuration from a Shelly device",
	Long:  "Delete the heater.js script and all associated KVS configuration entries.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doDelete, nil)
		return err
	},
}

func doDelete(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	fmt.Printf("Removing heater configuration from %s...\n", sd.Name())

	// Delete each KVS entry
	fmt.Printf("\nDeleting KVS entries:\n")
	deletedKeys := []string{}
	for _, key := range heaterKVSKeys {
		fmt.Printf("  Deleting %s...\n", key)
		_, err := kvs.DeleteKey(ctx, log, via, sd, key)
		if err != nil {
			// Check if error is "key not found" which is acceptable
			if strings.Contains(err.Error(), "not found") {
				fmt.Printf("  ⊘ Key %s not found (already deleted)\n", key)
			} else {
				fmt.Printf("  ✗ Failed to delete %s: %v\n", key, err)
				log.Error(err, "Failed to delete KVS key", "key", key)
			}
		} else {
			fmt.Printf("  ✓ Deleted %s\n", key)
			deletedKeys = append(deletedKeys, key)
		}
	}

	// Delete the heater.js script
	fmt.Printf("\nDeleting heater.js script...\n")
	scriptName := "heater.js"
	out, err := mhscript.DeleteWithVersion(ctx, log, via, sd, scriptName)
	if err != nil {
		if strings.Contains(err.Error(), "script not found") {
			fmt.Printf("  ⊘ Script %s not found (already deleted)\n", scriptName)
		} else {
			fmt.Printf("  ✗ Failed to delete script %s: %v\n", scriptName, err)
			return nil, fmt.Errorf("failed to delete script: %w", err)
		}
	} else {
		fmt.Printf("  ✓ Deleted script %s\n", scriptName)
	}

	fmt.Printf("\n✓ Heater cleanup complete on %s\n", sd.Name())

	return map[string]interface{}{
		"device":       sd.Name(),
		"deleted_keys": deletedKeys,
		"script":       scriptName,
		"result":       out,
	}, nil
}
