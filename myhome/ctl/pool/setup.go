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

		// Get all currently running pool-pump devices to determine peer IDs
		existingDevices, _ := getPoolDevices(ctx)

		// Build full device ID list: new device first, then existing peers
		allIDs := []string{dev.Id()}
		for _, existing := range existingDevices {
			if existing.Id() != dev.Id() {
				allIDs = append(allIDs, existing.Id())
			}
		}

		// Simple heuristic: first = pro3, second = pro1
		var pro3ID, pro1ID string
		if len(allIDs) > 0 {
			pro3ID = allIDs[0]
		}
		if len(allIDs) > 1 {
			pro1ID = allIDs[1]
		}

		// Default preferred settings
		currentPreferred := pro3ID
		if currentPreferred == "" {
			currentPreferred = dev.Id()
		}
		currentSpeed := "eco"

		// If there are existing devices, read current preferred from one of them
		via := types.ChannelMqtt
		if len(existingDevices) > 0 {
			if pref, err := getKVSValue(ctx, existingDevices[0], via, "script/pool-pump/preferred"); err == nil && pref != "" {
				currentPreferred = pref
			}
			if speed, err := getKVSValue(ctx, existingDevices[0], via, "script/pool-pump/speed"); err == nil && speed != "" {
				currentSpeed = speed
			}
		}

		service := mhscript.NewPoolService(hlog.Logger, provider)
		opts := mhscript.SetupOptions{
			PreferredDeviceID:    currentPreferred,
			PreferredSpeed:       currentSpeed,
			NightRunDurationMs:   int(DefaultNightRunDuration.Milliseconds()),
			GraceDelayMs:         int(DefaultGraceDelay.Milliseconds()),
			EcoSpeed:             DefaultEcoSpeed,
			MidSpeed:             DefaultMidSpeed,
			HighSpeed:            DefaultHighSpeed,
			TemperatureThreshold: DefaultTemperatureThreshold,
		}

		fmt.Printf("Adding device %s to pool pump mesh...\n", dev.Name())
		if err := service.AddDevice(ctx, via, sd, dev.Id(), pro3ID, pro1ID, allIDs, opts); err != nil {
			return fmt.Errorf("failed to add device: %w", err)
		}

		fmt.Printf("✓ Device %s added to pool pump mesh\n", dev.Name())
		fmt.Printf("  Total devices in mesh: %d\n", len(allIDs))
		fmt.Printf("  Preferred: %s (speed: %s)\n", currentPreferred, currentSpeed)
		return nil
	},
}

var preferredCmd = &cobra.Command{
	Use:   "preferred <device-id> <speed>",
	Short: "Set the preferred device and speed on all devices",
	Long: `Sets preferred_device_id and preferred_speed KVS values on ALL devices
in the pool pump mesh. The specified device will activate at the given speed.

Speed values:
  eco  - Low speed
  mid  - Medium speed (Pro3 only)
  high - High speed (Pro3 only)
  max  - Maximum speed`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		preferredID := args[0]
		speed := args[1]

		validSpeeds := map[string]bool{"eco": true, "mid": true, "high": true, "max": true}
		if !validSpeeds[speed] {
			return fmt.Errorf("invalid speed: %s (must be eco, mid, high, or max)", speed)
		}

		devices, err := getPoolDevices(ctx)
		if err != nil {
			return fmt.Errorf("failed to discover pool pump devices: %w", err)
		}
		if len(devices) == 0 {
			return fmt.Errorf("no devices running pool-pump.js. Run 'ctl pool add <device>' first")
		}

		// Verify preferred device is in the mesh
		found := false
		for _, sd := range devices {
			if sd.Id() == preferredID || sd.Name() == preferredID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("device %s is not in the pool pump mesh", preferredID)
		}

		provider := &poolProvider{}
		service := mhscript.NewPoolService(hlog.Logger, provider)
		via := types.ChannelMqtt

		fmt.Printf("Setting preferred device to %s (speed: %s) on %d devices...\n",
			preferredID, speed, len(devices))

		for _, sd := range devices {
			if err := service.SetPreferred(ctx, via, sd, preferredID, speed); err != nil {
				fmt.Printf("  ⚠ Failed to update %s: %v\n", sd.Name(), err)
				continue
			}
			fmt.Printf("  ✓ Updated %s\n", sd.Name())
		}

		fmt.Printf("\n✓ Preferred device set to %s (speed: %s)\n", preferredID, speed)
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
