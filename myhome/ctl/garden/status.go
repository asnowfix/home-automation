package garden

import (
	"fmt"
	"sort"
	"strings"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <device>",
	Short: "Show garden sprinkler status and per-zone deficits",
	Long: `Read all script/garden/* KVS keys from the device and display current
per-zone water deficits, today's planned watering, and active state.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		_, sd, err := getDeviceByAny(ctx, args[0])
		if err != nil {
			return fmt.Errorf("device lookup failed: %w", err)
		}
		via := types.ChannelMqtt

		// KVS.GetMany is bounded by MQTT message size (~22 items per call for
		// this device). Split into 4 targeted prefix calls and merge results.
		allItems := make(map[string]any)
		prefixes := []string{
			"script/garden/zone0*",
			"script/garden/zone1*",
			"script/garden/zone2*",
			"script/garden/*", // global config + plan keys (alphabetically first)
		}
		for _, prefix := range prefixes {
			r, rErr := kvs.GetManyValues(ctx, hlog.Logger, via, sd, prefix)
			if rErr != nil {
				return fmt.Errorf("KVS.GetMany(%s) failed: %w", prefix, rErr)
			}
			if r != nil {
				for k, v := range r.Items {
					allItems[k] = v
				}
			}
		}
		if len(allItems) == 0 {
			fmt.Printf("No garden KVS entries found on %s.\n", sd.Name())
			fmt.Printf("Run 'ctl garden setup %s' first.\n", args[0])
			return nil
		}

		// Collect and sort entries for stable output
		type kv struct{ k, v string }
		entries := make([]kv, 0, len(allItems))
		for k, v := range allItems {
			entries = append(entries, kv{k, fmt.Sprintf("%v", v)})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].k < entries[j].k })

		fmt.Printf("Garden sprinkler status — %s (%s)\n", sd.Name(), sd.Id())
		fmt.Println("═══════════════════════════════════════════")

		fmt.Println("\nWater balance:")
		for _, e := range entries {
			if strings.HasSuffix(e.k, "deficit") {
				fmt.Printf("  %-32s  %s mm\n", e.k, e.v)
			}
		}

		fmt.Println("\nToday's plan:")
		for _, e := range entries {
			if e.k == kvsPrefix+"last-plan-start" || e.k == kvsPrefix+"last-plan-zones" {
				fmt.Printf("  %-32s  %s\n", e.k, e.v)
			}
		}

		fmt.Println("\nAll KVS entries:")
		for _, e := range entries {
			fmt.Printf("  %-32s  %s\n", e.k, e.v)
		}
		return nil
	},
}

func init() {
	gardenCmd.AddCommand(statusCmd)
}
