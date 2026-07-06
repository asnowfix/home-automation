package pool

import (
	"context"
	"fmt"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [device-identifier]",
	Short: "Display pool pump system status",
	Long: `Show the current status of pool pump devices.

Without arguments, queries all devices running pool-pump.js.
With a device identifier pattern, queries only matching devices.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Discover all pool-pump devices dynamically
		devices, err := getPoolDevices(ctx)
		if err != nil {
			return fmt.Errorf("failed to discover pool pump devices: %w", err)
		}
		if len(devices) == 0 {
			fmt.Println("No devices running pool-pump.js.")
			fmt.Println("Run 'ctl pool add <device-identifier>' to add devices.")
			return nil
		}

		// Filter by pattern if provided
		var pattern string
		if len(args) > 0 {
			pattern = args[0]
		}

		via := types.ChannelMqtt

		fmt.Println("Pool Pump Mesh Status")
		fmt.Println("=====================")
		fmt.Println()

		found := false
		for _, sd := range devices {
			if pattern != "" && !strings.Contains(sd.Id(), pattern) && !strings.Contains(sd.Name(), pattern) {
				continue
			}

			found = true
			prefID, _ := getKVSValue(ctx, sd, via, "script/pool-pump/preferred")
			prefSpeed, _ := getKVSValue(ctx, sd, via, "script/pool-pump/speed")

			marker := ""
			if prefID == sd.Id() {
				marker = " [ACTIVE]"
			}

			fmt.Printf("• %s (%s)%s\n", sd.Name(), sd.Id(), marker)
			fmt.Printf("  Preferred: %s  Speed: %s\n", prefID, prefSpeed)
			fmt.Println()
		}

		if !found {
			if pattern != "" {
				fmt.Printf("No devices match pattern: %s\n", pattern)
			} else {
				fmt.Println("No devices found in mesh.")
			}
		}

		printFiltrationStatus(ctx)

		return nil
	},
}

// printFiltrationStatus prints today's turnover rate and water-supply status
// for the configured pool device, via the myhome.PoolGetStatus RPC method
// (registered by the daemon; see myhome/daemon/pool_rpc.go). This is a
// best-effort addition: if the daemon has pool tracking disabled or is
// unreachable, it prints a short note instead of failing the whole command.
func printFiltrationStatus(ctx context.Context) {
	out, err := myhome.TheClient.CallE(ctx, myhome.PoolGetStatus, nil)
	if err != nil {
		fmt.Println("Filtration Status")
		fmt.Println("=================")
		fmt.Printf("  (unavailable: %v)\n\n", err)
		return
	}
	status, ok := out.(*myhome.PoolGetStatusResult)
	if !ok {
		return
	}

	fmt.Println("Filtration Status")
	fmt.Println("=================")
	fmt.Printf("  Turnover today: %.2f of %.1f x/day (%s runtime)\n",
		status.TurnoverAchieved, status.TurnoverTarget, formatRuntime(status.RuntimeSec))
	if status.WaterSupplyActive {
		fmt.Println("  Water supply:   active (pump paused)")
	} else {
		fmt.Println("  Water supply:   OK")
	}
	fmt.Println()
}

// formatRuntime renders a duration in seconds as "1h23m" (or "45m", "30s"
// for durations under an hour/minute).
func formatRuntime(sec int64) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	mins := sec / 60
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh%02dm", mins/60, mins%60)
}
