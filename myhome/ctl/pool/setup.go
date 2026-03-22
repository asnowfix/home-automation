package pool

import (
	"fmt"
	"hlog"
	mhscript "internal/myhome/shelly/script"
	"time"

	"github.com/spf13/cobra"
)

var setupFlags struct {
	BootstrapDeviceIdentifier string
	BootstrapHoursThreshold   float64
	BootstrapDuration         time.Duration
	NightRunDuration          time.Duration
	BootstrapPostDelay        time.Duration
	EcoSpeed                  int
	MidSpeed                  int
	HighSpeed                 int
	TemperatureThreshold      float64
	ForceUpload               bool
	NoMinify                  bool
}

var setupCmd = &cobra.Command{
	Use:   "setup <controller-device-identifier>",
	Short: "Setup pool pump scripts on controller and bootstrap devices",
	Long: `Upload pool-pump.js script to both controller and bootstrap devices and configure KVS settings.

The controller device manages the pump speeds and schedules and stores all configuration.
The bootstrap device provides high-speed startup assistance in cold weather and receives minimal config.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Controller device ID from positional argument
		controllerDeviceID := args[0]

		// Validate required flags
		if setupFlags.BootstrapDeviceIdentifier == "" {
			return fmt.Errorf("--bootstrap-device-identifier is required")
		}

		// Create pool service
		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)

		// Resolve controller and bootstrap devices to get IDs and names
		controllerDev, err := provider.GetDeviceByAny(ctx, controllerDeviceID)
		if err != nil {
			return fmt.Errorf("failed to resolve controller device %s: %w", controllerDeviceID, err)
		}

		bootstrapDev, err := provider.GetDeviceByAny(ctx, setupFlags.BootstrapDeviceIdentifier)
		if err != nil {
			return fmt.Errorf("failed to resolve bootstrap device %s: %w", setupFlags.BootstrapDeviceIdentifier, err)
		}

		opts := mhscript.SetupOptions{
			ControllerDeviceID:      controllerDeviceID,
			BootstrapDeviceID:       setupFlags.BootstrapDeviceIdentifier,
			BootstrapHoursThreshold: setupFlags.BootstrapHoursThreshold,
			BootstrapDurationMs:     int(setupFlags.BootstrapDuration.Milliseconds()),
			NightRunDurationMs:      int(setupFlags.NightRunDuration.Milliseconds()),
			BootstrapToSpeedDelayMs: int(setupFlags.BootstrapPostDelay.Milliseconds()),
			EcoSpeed:                setupFlags.EcoSpeed,
			MidSpeed:                setupFlags.MidSpeed,
			HighSpeed:               setupFlags.HighSpeed,
			TemperatureThreshold:    setupFlags.TemperatureThreshold,
			ForceUpload:             setupFlags.ForceUpload,
			NoMinify:                setupFlags.NoMinify,
		}

		fmt.Printf("Setting up pool pump system...\n")
		fmt.Printf("  Controller: %s → %s (%s)\n", controllerDeviceID, controllerDev.Name(), controllerDev.Id())
		fmt.Printf("  Bootstrap:  %s → %s (%s)\n", setupFlags.BootstrapDeviceIdentifier, bootstrapDev.Name(), bootstrapDev.Id())
		fmt.Printf("  Bootstrap threshold: %.1f hours since last run\n", setupFlags.BootstrapHoursThreshold)
		fmt.Printf("  Temperature threshold: %.1f°C for summer mode\n", setupFlags.TemperatureThreshold)
		fmt.Printf("\n")

		if err := service.Setup(ctx, opts); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}

		fmt.Printf("\n✓ Pool pump setup complete\n")
		return nil
	},
}

func init() {
	// Device identifiers
	setupCmd.Flags().StringVarP(&setupFlags.BootstrapDeviceIdentifier, "bootstrap-device-identifier", "b", "", "Bootstrap helper device identifier (name, IP, or ID)")

	// Operational parameters
	setupCmd.Flags().Float64Var(&setupFlags.BootstrapHoursThreshold, "bootstrap-hours-threshold", 6.0, "Hours since last run above which bootstrap is needed")
	setupCmd.Flags().DurationVar(&setupFlags.BootstrapDuration, "bootstrap-duration", 2*time.Minute, "Bootstrap duration (e.g., 2m, 120s)")
	setupCmd.Flags().DurationVar(&setupFlags.NightRunDuration, "night-run-duration", 1*time.Hour, "Night run duration (e.g., 1h, 3600s)")
	setupCmd.Flags().DurationVar(&setupFlags.BootstrapPostDelay, "bootstrap-post-delay", 500*time.Millisecond, "Delay after bootstrap before starting speed (e.g., 500ms, 5s)")
	setupCmd.Flags().Float64Var(&setupFlags.TemperatureThreshold, "temperature-threshold", 20.0, "Temperature threshold (°C) for summer mode (day schedule)")

	// Speed mappings
	setupCmd.Flags().IntVar(&setupFlags.EcoSpeed, "eco-speed", 0, "Controller switch ID for eco/low speed (0, 1, or 2) (default 0)")
	setupCmd.Flags().IntVar(&setupFlags.MidSpeed, "mid-speed", 1, "Controller switch ID for mid speed (0, 1, or 2)")
	setupCmd.Flags().IntVar(&setupFlags.HighSpeed, "high-speed", 2, "Controller switch ID for high speed (0, 1, or 2)")

	// Upload options
	setupCmd.Flags().BoolVar(&setupFlags.ForceUpload, "force", false, "Force re-upload even if version hash matches")
	setupCmd.Flags().BoolVar(&setupFlags.NoMinify, "no-minify", false, "Do not minify script before upload")

	// Mark required flags
	setupCmd.MarkFlagRequired("bootstrap-device-identifier")
}
