package pool

import (
	"context"
	"fmt"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/spf13/cobra"
)

// getAllKnownDevices returns all myhome.Device entries from the server
func getAllKnownDevices(ctx context.Context) ([]*myhome.Device, error) {
	devices, err := myhome.TheClient.LookupDevices(ctx, "*")
	if err != nil {
		return nil, err
	}
	var result []*myhome.Device
	for _, d := range *devices {
		mac := ""
		if d.Mac() != nil {
			mac = d.Mac().String()
		}
		result = append(result, &myhome.Device{
			DeviceSummary: myhome.DeviceSummary{
				DeviceIdentifier: myhome.DeviceIdentifier{
					Manufacturer_: d.Manufacturer(),
					Id_:           d.Id(),
				},
				MAC:   mac,
				Host_: d.Host(),
				Name_: d.Name(),
			},
		})
	}
	return result, nil
}

// getPoolDevices returns all Shelly devices currently running pool-pump.js,
// discovered dynamically from the server's device registry.
func getPoolDevices(ctx context.Context) ([]*shelly.Device, error) {
	provider := &poolProvider{}

	allDevices, err := getAllKnownDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}

	var poolDevices []*shelly.Device
	via := types.ChannelMqtt

	for _, dev := range allDevices {
		sd, err := provider.GetShellyDevice(ctx, dev)
		if err != nil {
			continue
		}
		status, err := pkgscript.ScriptStatus(ctx, sd, via, "pool-pump.js")
		if err != nil || status == nil {
			continue
		}
		poolDevices = append(poolDevices, sd)
	}

	return poolDevices, nil
}

// getKVSValue retrieves a single KVS value from a device
func getKVSValue(ctx context.Context, sd *shelly.Device, via types.Channel, key string) (string, error) {
	val, err := kvs.GetValue(ctx, hlog.Logger, via, sd, key)
	if err != nil || val == nil {
		return "", err
	}
	return val.Value, nil
}

var addCmd = &cobra.Command{
	Use:   "add <device-identifier>",
	Short: "Add a device to the pool pump mesh",
	Long: `Upload pool-pump.js script to a device and configure it with KVS values.

Membership is defined by the script running on a device — no local registry is used.
Schedules are only created on Pro3 devices (detected by switch count).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deviceID := args[0]

		provider := &poolProvider{}
		dev, err := provider.GetDeviceByAny(ctx, deviceID)
		if err != nil {
			return fmt.Errorf("device not found: %s: %w", deviceID, err)
		}
		sd, err := provider.GetShellyDevice(ctx, dev)
		if err != nil {
			return fmt.Errorf("failed to get shelly device: %w", err)
		}

		// Controllers do not coordinate, so there are no peers to discover.
		// An already-configured device is still worth reading for its speed, so
		// adding a second controller inherits the setting rather than silently
		// resetting it to the default.
		existingDevices, _ := getPoolDevices(ctx)

		currentSpeed := "eco"
		via := types.ChannelMqtt
		if len(existingDevices) > 0 {
			if speed, err := getKVSValue(ctx, existingDevices[0], via, "script/pool-pump/speed"); err == nil && speed != "" {
				currentSpeed = speed
			}
		}

		service := mhscript.NewPoolService(hlog.Logger, provider)
		opts := mhscript.SetupOptions{
			PreferredSpeed:       currentSpeed,
			NightRunDurationMs:   int(DefaultNightRunDuration.Milliseconds()),
			EcoSpeed:             DefaultEcoSpeed,
			DaySpeed:             DefaultDaySpeed,
			MaxSpeed:             DefaultMaxSpeed,
			TemperatureThreshold: DefaultTemperatureThreshold,
			PoolVolume:           DefaultPoolVolume,
			Turnover:             DefaultTurnover,
			MaxFlowRate:          DefaultMaxFlowRate,
			MaxRpm:               DefaultMaxRpm,
			EcoRpm:               DefaultEcoRpm,
			DayRpm:               DefaultDayRpm,
			MaxTemp:              DefaultMaxTemp,

			SolarEnabled:         DefaultSolarEnabled,
			SolarStartThresholdW: DefaultSolarStartThresholdW,
			SolarStopThresholdW:  DefaultSolarStopThresholdW,
			SolarStartDelayMs:    DefaultSolarStartDelayMs,
			SolarStopDelayMs:     DefaultSolarStopDelayMs,
			SolarMinTurnover:     DefaultSolarMinTurnover,
			SolarMaxTurnover:     DefaultSolarMaxTurnover,
			SolarStaleMs:         DefaultSolarStaleMs,

			OverrideMs: DefaultOverrideMs,
		}

		fmt.Printf("Configuring %s as a pool pump controller...\n", dev.Name())
		if err := service.AddDevice(ctx, via, sd, dev.Id(), opts); err != nil {
			return fmt.Errorf("failed to add device: %w", err)
		}

		fmt.Printf("✓ %s configured as a pool pump controller (speed: %s)\n", dev.Name(), currentSpeed)
		return nil
	},
}

var preferredCmd = &cobra.Command{
	Use:   "speed <speed>",
	Short: "Set the speed the pump controller starts at",
	Long: `Sets the preferred_speed KVS value on every configured pool pump controller.

Speed values:
  eco - Low speed (Pro3 only)
  day - Day speed (Pro3 only)
  max - Maximum speed (the only stage a Pro1 has)

A Pro1 is on/off, so any speed resolves to its single stage.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		speed := args[0]

		validSpeeds := map[string]bool{"eco": true, "day": true, "max": true}
		if !validSpeeds[speed] {
			return fmt.Errorf("invalid speed: %s (must be eco, day or max)", speed)
		}

		devices, err := getPoolDevices(ctx)
		if err != nil {
			return fmt.Errorf("failed to discover pool pump devices: %w", err)
		}
		if len(devices) == 0 {
			return fmt.Errorf("no devices running pool-pump.js. Run 'ctl pool add <device>' first")
		}

		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)
		via := types.ChannelMqtt

		fmt.Printf("Setting pump speed to %s on %d controller(s)...\n", speed, len(devices))

		for _, sd := range devices {
			if err := service.SetSpeed(ctx, via, sd, speed); err != nil {
				fmt.Printf("  ⚠ Failed to update %s: %v\n", sd.Name(), err)
				continue
			}
			fmt.Printf("  ✓ Updated %s\n", sd.Name())
		}

		fmt.Printf("\n✓ Pump speed set to %s\n", speed)
		return nil
	},
}

var removeCmd = &cobra.Command{
	Use:   "remove <device-identifier>",
	Short: "Remove a device from the pool pump mesh",
	Long:  `Stop the pool-pump.js script and clear its KVS values on the specified device.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deviceID := args[0]

		provider := &poolProvider{}
		dev, err := provider.GetDeviceByAny(ctx, deviceID)
		if err != nil {
			return fmt.Errorf("device not found: %s: %w", deviceID, err)
		}
		sd, err := provider.GetShellyDevice(ctx, dev)
		if err != nil {
			return fmt.Errorf("failed to get shelly device: %w", err)
		}

		service := mhscript.NewPoolService(hlog.Logger, provider)
		via := types.ChannelMqtt
		if err := service.RemoveDevice(ctx, via, sd); err != nil {
			return fmt.Errorf("failed to remove device: %w", err)
		}

		fmt.Printf("✓ Device %s removed from pool pump mesh\n", dev.Name())
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all devices in the pool pump mesh",
	Long:  `Display all devices currently running pool-pump.js and their KVS state.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		devices, err := getPoolDevices(ctx)
		if err != nil {
			return fmt.Errorf("failed to discover pool pump devices: %w", err)
		}

		if len(devices) == 0 {
			fmt.Println("No devices running pool-pump.js.")
			fmt.Println("Run 'ctl pool add <device-identifier>' to add devices.")
			return nil
		}

		via := types.ChannelMqtt
		fmt.Printf("Pool pump mesh: %d devices\n\n", len(devices))

		for i, sd := range devices {
			prefID, _ := getKVSValue(ctx, sd, via, "script/pool-pump/preferred")
			prefSpeed, _ := getKVSValue(ctx, sd, via, "script/pool-pump/speed")

			marker := ""
			if prefID == sd.Id() {
				marker = " [PREFERRED]"
			}

			fmt.Printf("%d. %s (%s)%s\n", i+1, sd.Name(), sd.Id(), marker)
			fmt.Printf("   Preferred: %s  Speed: %s\n", prefID, prefSpeed)
		}

		return nil
	},
}

func init() {
	// Add subcommands to poolCmd
	poolCmd.AddCommand(addCmd)
	poolCmd.AddCommand(preferredCmd)
	poolCmd.AddCommand(removeCmd)
	poolCmd.AddCommand(listCmd)
	// Note: startCmd, stopCmd, statusCmd, purgeCmd are registered in their respective files

	// Flags for add command
	addCmd.Flags().Bool("force", false, "Force re-upload even if version hash matches")
	addCmd.Flags().Bool("no-minify", false, "Do not minify script before upload")
}
