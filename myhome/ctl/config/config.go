package config

import (
	"fmt"
	"hlog"
	"myhome"

	"github.com/spf13/cobra"
)

var flags struct {
	Name       string
	EcoMode    bool
	EcoModeSet bool
}

var Cmd = &cobra.Command{
	Use:   "config <device_id|name|ip|mac>",
	Short: "Configure device settings in local database and on device",
	Long: `Configure device settings in the local database and optionally on the device itself.

This command updates the device name in the local database for all device types (Gen1 and Gen2+).
For Gen2+ devices, it can also update the device configuration (name, eco mode) on the device itself.

Gen1 devices only support local database updates since they don't have RPC configuration APIs.

Examples:
  # Rename a Gen1 device in local database (by device ID)
  myhome ctl config shellyht-EE45E9 --name "Living Room Sensor"

  # Rename a device by MAC address
  myhome ctl config 4c:eb:d6:ee:45:e9 --name "Kitchen Sensor"

  # Rename a Gen2+ device in database and on device
  myhome ctl config shelly1minig3-abc123 --name "Bedroom Light"

  # Set eco mode on Gen2+ device
  myhome ctl config shelly1minig3-abc123 --ecomode true

  # Update both name and eco mode
  myhome ctl config shelly1minig3-abc123 --name "Office Light" --ecomode false`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log := hlog.Logger

		identifier := args[0]

		// Validate that at least one flag is set
		if flags.Name == "" && !flags.EcoModeSet {
			return fmt.Errorf("at least one configuration option must be specified (--name or --ecomode)")
		}

		// Prepare ecoMode pointer (nil if not set)
		var ecoModePtr *bool
		if flags.EcoModeSet {
			ecoModePtr = &flags.EcoMode
		}

		// Call the configuration update function
		err := myhome.ConfigureDevice(ctx, log, identifier, flags.Name, ecoModePtr)
		if err != nil {
			return fmt.Errorf("failed to configure device: %w", err)
		}

		fmt.Printf("✓ Successfully configured device %s\n", identifier)
		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&flags.Name, "name", "n", "", "Set device name (updates local DB and Gen2+ devices)")
	Cmd.Flags().BoolVar(&flags.EcoMode, "ecomode", false, "Set eco mode on Gen2+ devices (true/false)")
	
	// Track if ecomode was explicitly set
	Cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		flags.EcoModeSet = cmd.Flags().Changed("ecomode")
		return nil
	}
}
