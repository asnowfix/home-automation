package pool

import (
	"fmt"

	"github.com/asnowfix/home-automation/hlog"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <device-identifier>",
	Short: "Stop the pool pump",
	Long:  `Stop the pool pump on the specified device.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Device ID from positional argument
		deviceID := args[0]

		// Create pool service
		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)

		fmt.Printf("Stopping pool pump...\n")

		if err := service.Stop(ctx, deviceID); err != nil {
			return fmt.Errorf("failed to stop pump: %w", err)
		}

		fmt.Printf("✓ Pool pump stopped\n")
		return nil
	},
}

func init() {
	poolCmd.AddCommand(stopCmd)
	poolCmd.AddCommand(statusCmd)
	poolCmd.AddCommand(purgeCmd)
}
