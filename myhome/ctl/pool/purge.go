package pool

import (
	"fmt"

	"github.com/asnowfix/home-automation/hlog"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"

	"github.com/spf13/cobra"
)

var purgeCmd = &cobra.Command{
	Use:   "purge <device-identifier>",
	Short: "Purge pool pump setup from a device",
	Long: `Remove pool pump scripts and configuration from a device.

This command will:
  - Stop the pump
  - Remove all KVS configuration keys
  - Stop the script
  - Delete the script from the device`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deviceID := args[0]

		// Create pool service
		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)

		fmt.Printf("Purging pool pump setup...\n")

		if err := service.Purge(ctx, deviceID); err != nil {
			return fmt.Errorf("failed to purge pool pump setup: %w", err)
		}

		fmt.Printf("✓ Pool pump setup purged\n")
		return nil
	},
}
