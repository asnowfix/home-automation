package pool

import (
	"fmt"
	"github.com/asnowfix/home-automation/hlog"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"

	"github.com/spf13/cobra"
)

var purgeCmd = &cobra.Command{
	Use:   "purge <controller-device-identifier>",
	Short: "Purge pool pump setup from controller and bootstrap devices",
	Long: `Remove pool pump scripts and configuration from both controller and bootstrap devices.

This command will:
  - Stop the pump on both devices
  - Remove all KVS configuration keys
  - Stop the scripts
  - Delete the scripts from both devices`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		controllerID := args[0]

		// Create pool service
		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)

		fmt.Printf("Purging pool pump setup...\n")

		if err := service.Purge(ctx, controllerID); err != nil {
			return fmt.Errorf("failed to purge pool pump setup: %w", err)
		}

		fmt.Printf("✓ Pool pump setup purged\n")
		return nil
	},
}
