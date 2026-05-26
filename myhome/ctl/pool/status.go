package pool

import (
	"fmt"
	"strings"

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

		return nil
	},
}
