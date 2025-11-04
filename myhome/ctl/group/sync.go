package group

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	"github.com/spf13/cobra"
)

var (
	syncForce bool
)

func init() {
	Cmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "Update KVS values even if keys already exist")
}

var syncCmd = &cobra.Command{
	Use:   "sync <group-name>",
	Short: "Synchronize group KVS settings to all devices in the group",
	Long: `Synchronize group KVS key-value pairs to all devices in the group.

By default, this command only sets KVS keys that are missing on devices.
Use --force to update all KVS values to match the group definition, even if
the keys already exist with different values.

Examples:
  # Sync missing KVS keys to all devices
  myhome ctl group sync radiateurs

  # Force update all KVS values to match group definition
  myhome ctl group sync radiateurs --force`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		groupName := args[0]

		// Get group info
		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.GroupShow, groupName)
		if err != nil {
			return err
		}
		g, ok := out.(*myhome.Group)
		if !ok {
			return fmt.Errorf("expected myhome.Group, got %T", out)
		}

		groupKVS := g.GroupInfo.KeyValues()
		if len(groupKVS) == 0 {
			fmt.Printf("Group '%s' has no KVS key-value pairs to sync\n", groupName)
			return nil
		}

		fmt.Printf("Syncing group '%s' KVS to %d device(s)...\n", groupName, len(g.Devices))
		fmt.Printf("Group KVS: %v\n", groupKVS)
		if syncForce {
			fmt.Println("Mode: FORCE (will update existing keys)")
		} else {
			fmt.Println("Mode: ADD MISSING (will only add missing keys)")
		}
		fmt.Println()

		// Sync each device
		successCount := 0
		failCount := 0
		for _, deviceSummary := range g.Devices {
			deviceName := deviceSummary.Name()
			fmt.Printf("Processing %s...\n", deviceName)

			err := syncDeviceKVS(cmd.Context(), &g.GroupInfo, deviceSummary)
			if err != nil {
				fmt.Printf("  ✗ Failed: %v\n", err)
				failCount++
			} else {
				fmt.Printf("  ✓ Success\n")
				successCount++
			}
		}

		fmt.Printf("\nSync complete: %d succeeded, %d failed\n", successCount, failCount)
		if failCount > 0 {
			return fmt.Errorf("%d device(s) failed to sync", failCount)
		}
		return nil
	},
}

func syncDeviceKVS(ctx context.Context, gi *myhome.GroupInfo, deviceSummary devices.Device) error {
	log := hlog.Logger

	// Create Shelly device
	device, err := shelly.NewDeviceFromSummary(ctx, log, deviceSummary)
	if err != nil {
		return fmt.Errorf("unable to create device: %w", err)
	}

	// Cast to *shelly.Device to access KVS methods
	sd, ok := device.(*shelly.Device)
	if !ok {
		return fmt.Errorf("device is not a Shelly: %T", device)
	}

	groupKVS := gi.KeyValues()

	// Get current device KVS by listing all keys
	listResp, err := kvs.ListKeys(ctx, log, types.ChannelDefault, sd, "*")
	if err != nil {
		return fmt.Errorf("unable to list device KVS: %w", err)
	}

	// Build map of current keys
	currentKeys := make(map[string]string)
	if listResp != nil && listResp.Keys != nil {
		for key := range listResp.Keys {
			// Get the value for this key
			val, err := kvs.GetValue(ctx, log, types.ChannelDefault, sd, key)
			if err == nil && val != nil {
				currentKeys[key] = val.Value
			}
		}
	}

	// Sync each group KVS key
	updatedCount := 0
	skippedCount := 0
	for key, groupValue := range groupKVS {
		currentValue, exists := currentKeys[key]

		if !exists {
			// Key doesn't exist - always add it
			fmt.Printf("  + Adding %s=%s\n", key, groupValue)
			_, err := kvs.SetKeyValue(ctx, log, types.ChannelDefault, sd, key, groupValue)
			if err != nil {
				return fmt.Errorf("failed to set %s: %w", key, err)
			}
			updatedCount++
		} else if currentValue != groupValue {
			// Key exists with different value
			if syncForce {
				fmt.Printf("  ↻ Updating %s: %s → %s\n", key, currentValue, groupValue)
				_, err := kvs.SetKeyValue(ctx, log, types.ChannelDefault, sd, key, groupValue)
				if err != nil {
					return fmt.Errorf("failed to update %s: %w", key, err)
				}
				updatedCount++
			} else {
				fmt.Printf("  - Skipping %s=%s (current: %s, use --force to update)\n", key, groupValue, currentValue)
				skippedCount++
			}
		} else {
			// Key exists with same value
			fmt.Printf("  ✓ Already set %s=%s\n", key, groupValue)
			skippedCount++
		}
	}

	if updatedCount > 0 {
		fmt.Printf("  Updated %d key(s)", updatedCount)
		if skippedCount > 0 {
			fmt.Printf(", skipped %d", skippedCount)
		}
		fmt.Println()
	} else if skippedCount > 0 {
		fmt.Printf("  No changes needed (%d key(s) already set)\n", skippedCount)
	}

	return nil
}
