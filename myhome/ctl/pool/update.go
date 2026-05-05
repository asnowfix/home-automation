package pool

import (
	"fmt"
	"sort"

	"github.com/asnowfix/home-automation/hlog"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/go-shellies"
	"github.com/asnowfix/go-shellies/kvs"
	"github.com/asnowfix/go-shellies/types"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [device-identifier]",
	Short: "Check and update pool pump KVS config and script",
	Long: `Audits pool pump devices and:
  - Reports missing required KVS configuration keys
  - Reports (and optionally removes) stale KVS keys with the script/pool-pump/ prefix
  - Uploads pool-pump.js if the embedded version differs from what is on the device

Without arguments, operates on all devices currently running pool-pump.js.
With a device identifier, operates on that specific device only.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		removeStale, _ := cmd.Flags().GetBool("remove-stale")
		force, _ := cmd.Flags().GetBool("force")
		noMinify, _ := cmd.Flags().GetBool("no-minify")

		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)
		via := types.ChannelMqtt

		var devices []*shelly.Device
		if len(args) > 0 {
			dev, err := provider.GetDeviceByAny(ctx, args[0])
			if err != nil {
				return fmt.Errorf("device not found: %s: %w", args[0], err)
			}
			sd, err := provider.GetShellyDevice(ctx, dev)
			if err != nil {
				return fmt.Errorf("failed to get shelly device: %w", err)
			}
			devices = []*shelly.Device{sd}
		} else {
			var err error
			devices, err = getPoolDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to discover pool pump devices: %w", err)
			}
			if len(devices) == 0 {
				fmt.Println("No devices running pool-pump.js.")
				fmt.Println("Run 'ctl pool add <device-identifier>' to add devices.")
				return nil
			}
		}

		hasErrors := false
		for _, sd := range devices {
			fmt.Printf("Updating %s (%s)...\n", sd.Name(), sd.Id())

			result, err := service.UpdateDevice(ctx, via, sd, force, noMinify)
			if err != nil {
				fmt.Printf("  ✗ Error: %v\n", err)
				hasErrors = true
				continue
			}

			// Report missing KVS config keys
			if len(result.MissingKVS) > 0 {
				sort.Strings(result.MissingKVS)
				fmt.Printf("  ⚠ Missing KVS keys (%d):\n", len(result.MissingKVS))
				for _, k := range result.MissingKVS {
					fmt.Printf("      %s\n", k)
				}
			}

			// Report or remove stale KVS keys
			if len(result.StaleKVS) > 0 {
				sort.Strings(result.StaleKVS)
				if removeStale {
					fmt.Printf("  → Removing %d stale KVS key(s):\n", len(result.StaleKVS))
					for _, k := range result.StaleKVS {
						if _, delErr := kvs.DeleteKey(ctx, hlog.Logger, via, sd, k); delErr != nil {
							fmt.Printf("    ✗ %s: %v\n", k, delErr)
						} else {
							fmt.Printf("    ✓ %s deleted\n", k)
						}
					}
				} else {
					fmt.Printf("  ⚠ Stale KVS keys (%d, use --remove-stale to delete):\n", len(result.StaleKVS))
					for _, k := range result.StaleKVS {
						fmt.Printf("      %s\n", k)
					}
				}
			}

			// Water-supply input status
			if result.WaterSupplyFixed {
				fmt.Printf("  ✓ input:0 (water-supply) invert corrected\n")
			} else if result.WaterSupplyInvertOK {
				fmt.Printf("  ✓ input:0 (water-supply) invert OK\n")
			}

			// Script update status
			if result.ScriptUpdated {
				fmt.Printf("  ✓ pool-pump.js uploaded (new version)\n")
			} else {
				fmt.Printf("  ✓ pool-pump.js is up to date\n")
			}

			// Schedule reconciliation (Pro3 only)
			if result.DeviceType == "pro3" {
				if result.SchedulesReconciled {
					fmt.Printf("  ✓ schedules reconciled\n")
				} else {
					fmt.Printf("  ⚠ schedule reconciliation failed (see errors)\n")
				}
			}

			// Any errors from the service
			for _, e := range result.Errors {
				fmt.Printf("  ✗ %s\n", e)
				hasErrors = true
			}
		}

		if hasErrors {
			return fmt.Errorf("one or more devices had errors during update")
		}
		return nil
	},
}

func init() {
	poolCmd.AddCommand(updateCmd)
	updateCmd.Flags().Bool("remove-stale", false, "Delete stale KVS keys from devices")
	updateCmd.Flags().Bool("force", false, "Force script re-upload even if the version hash matches")
	updateCmd.Flags().Bool("no-minify", false, "Do not minify the script before uploading")
}
