package pool

import (
	"fmt"
	"github.com/asnowfix/home-automation/hlog"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start <controller-device-identifier> <eco|mid|high>",
	Short: "Start the pool pump at specified speed",
	Long: `Start the pool pump at the specified speed: eco, mid, or high.

The controller will automatically engage bootstrap if outdoor temperature is below threshold.`,
	Args:      cobra.ExactArgs(2),
	ValidArgs: []string{"eco", "mid", "high"},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		controllerDeviceID := args[0]
		speedArg := args[1]

		// Validate speed
		var speed mhscript.Speed
		switch speedArg {
		case "eco":
			speed = mhscript.SpeedEco
		case "mid":
			speed = mhscript.SpeedMid
		case "high":
			speed = mhscript.SpeedHigh
		default:
			return fmt.Errorf("invalid speed: %s (must be eco, mid, or high)", speedArg)
		}

		// Create pool service
		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)

		fmt.Printf("Starting pool pump at %s speed...\n", speed)

		if err := service.Start(ctx, controllerDeviceID, speed); err != nil {
			return fmt.Errorf("failed to start pump: %w", err)
		}

		fmt.Printf("✓ Pool pump started at %s speed\n", speed)
		return nil
	},
}
