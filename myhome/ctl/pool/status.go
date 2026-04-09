package pool

import (
	"fmt"
	"github.com/asnowfix/home-automation/hlog"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/myhome/ctl/options"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <controller-device-identifier>",
	Short: "Display pool pump system status",
	Long:  `Show the current status of both controller and bootstrap devices, including active speeds, inputs, and environmental conditions.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Controller device ID from positional argument
		controllerDeviceID := args[0]

		// Create pool service
		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)

		// Get status (service will retrieve bootstrap device ID from controller's KVS)
		status, err := service.Status(ctx, controllerDeviceID)
		if err != nil {
			return fmt.Errorf("failed to get status: %w", err)
		}

		// Output in YAML or JSON format
		return options.PrintResult(status)
	},
}
