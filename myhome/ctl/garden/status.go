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

		resp, err := kvs.GetManyValues(ctx, hlog.Logger, via, sd, "script/garden/*")
		if err != nil {
			return fmt.Errorf("KVS.GetMany failed: %w", err)
		}
		if resp == nil || len(resp.Items) == 0 {
			fmt.Printf("No garden KVS entries found on %s.\n", sd.Name())
			fmt.Printf("Run 'ctl garden setup %s' first.\n", args[0])
			return nil
		}

		// Collect and sort entries for stable output
		type kv struct{ k, v string }
		entries := make([]kv, 0, len(resp.Items))
		for k, v := range resp.Items {
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
