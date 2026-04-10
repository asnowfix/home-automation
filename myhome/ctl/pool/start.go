package pool

import (
	"fmt"

	"github.com/asnowfix/home-automation/hlog"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"

	"github.com/spf13/cobra"
)

var startFlags struct {
	Bootstrap bool
}

var startCmd = &cobra.Command{
	Use:   "start <controller-device-identifier> <eco|mid|high>",
	Short: "Start the pool pump at specified speed",
	Long: `Start the pool pump at the specified speed: eco, mid, or high.

The controller will automatically engage bootstrap if outdoor temperature is below threshold.
Use --bootstrap flag to force bootstrap startup regardless of temperature or time since last run.`,
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

		if startFlags.Bootstrap {
			fmt.Printf("Starting pool pump at %s speed with forced bootstrap...\n", speed)
		} else {
			fmt.Printf("Starting pool pump at %s speed...\n", speed)
		}

		if err := service.Start(ctx, controllerDeviceID, speed, startFlags.Bootstrap); err != nil {
			return fmt.Errorf("failed to start pump: %w", err)
		}

		if startFlags.Bootstrap {
			fmt.Printf("✓ Pool pump started at %s speed with bootstrap\n", speed)
		} else {
			fmt.Printf("✓ Pool pump started at %s speed\n", speed)
		}
		return nil
	},
}

func init() {
	startCmd.Flags().BoolVarP(&startFlags.Bootstrap, "bootstrap", "b", false, "Force bootstrap startup regardless of temperature or time since last run")
}
