package setup

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"hlog"
	shellysetup "internal/myhome/shelly/setup"
	"myhome"
	mhmqtt "myhome/mqtt"
	"myhome/net"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
)

// options for STA
var staEssid string
var staPasswd string

// options for AP
var apPasswd string

// options for STA1
var sta1Essid string
var sta1Passwd string

// options for MQTT (override)
var mqttBrokerOverride string

// options for device name
var deviceNameOverride string

func init() {
	Cmd.Flags().StringVar(&staEssid, "sta-essid", "", "STA ESSID")
	Cmd.Flags().StringVar(&staPasswd, "sta-passwd", "", "STA Password")
	Cmd.Flags().StringVar(&sta1Essid, "sta1-essid", "", "STA1 ESSID")
	Cmd.Flags().StringVar(&sta1Passwd, "sta1-passwd", "", "STA1 Password")
	Cmd.Flags().StringVar(&apPasswd, "ap-passwd", "", "AP Password")
	Cmd.Flags().StringVar(&mqttBrokerOverride, "mqtt-broker", "", "Override MQTT broker address (default: use current process broker)")
	Cmd.Flags().StringVar(&deviceNameOverride, "name", "", "Set device name (overrides auto-derivation)")
}

// isTimeoutError checks if an error is a timeout error
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "Timeout") ||
		strings.Contains(errStr, "deadline exceeded")
}

// setupDeviceByIP sets up a device using its IP address (initial setup mode)
func setupDeviceByIP(ctx context.Context, name string, ip net.IP) error {
	_, err := myhome.Foreach(ctx, hlog.Logger, ip.String(), types.ChannelHttp, doSetup, []string{name})
	return err
}

// setupDevicesByName sets up devices by looking them up by name pattern
func setupDevicesByName(ctx context.Context, pattern string) error {
	_, err := myhome.Foreach(ctx, hlog.Logger, pattern, types.ChannelHttp, doSetup, []string{})
	return err
}

// getSetupConfig returns the setup configuration, using the current process MQTT broker
// unless overridden by command-line flag
func getSetupConfig(ctx context.Context) shellysetup.Config {
	cfg := shellysetup.Config{
		MqttPort: 1883,
		Resolver: mynet.MyResolver(hlog.Logger),
	}

	// Use override if specified, otherwise use current process MQTT broker
	if mqttBrokerOverride != "" {
		cfg.MqttBroker = mqttBrokerOverride
	} else {
		// Get the MQTT broker from the current process client
		mqttClient, err := mhmqtt.GetClientE(ctx)
		if err == nil && mqttClient != nil {
			// GetServer returns host:port, we just need the host part for setup
			server := mqttClient.GetServer()
			cfg.MqttBroker = server // Already includes port, setup will handle it
			cfg.MqttPort = 0        // Signal to use the server as-is (includes port)
		} else {
			// Fallback to mqtt.local if no client available
			cfg.MqttBroker = "mqtt.local"
		}
	}

	return cfg
}

// doSetup performs the actual setup logic for a single device
func doSetup(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("expected *shellyapi.Device, got %T", device)
	}

	// Get device name: --name flag takes priority, then args, then existing name
	var targetName string
	if deviceNameOverride != "" {
		targetName = deviceNameOverride
	} else if len(args) > 0 && args[0] != "" {
		targetName = args[0]
	} else {
		targetName = sd.Name()
	}

	// Device identifier for all output lines
	deviceId := fmt.Sprintf("%s (%s)", targetName, sd.Id())

	// For Gen1 and BLU devices: skip device communication, but save name to DB if provided
	if shellyapi.IsGen1Device(sd.Id()) || shellyapi.IsBluDevice(sd.Id()) {
		deviceType := "Gen1"
		if shellyapi.IsBluDevice(sd.Id()) {
			deviceType = "BLU"
		}
		fmt.Printf("Skipping device communication for %s device %s\n", deviceType, deviceId)

		// Save name to DB if provided (via RPC to daemon)
		if targetName != "" && targetName != sd.Name() && myhome.TheClient != nil {
			fmt.Printf("  . Updating device name in DB: %s\n", targetName)
			_, err := myhome.TheClient.CallE(ctx, myhome.DeviceSetup, &myhome.DeviceSetupParams{
				Identifier: sd.Id(),
				Name:       targetName,
			})
			if err != nil {
				fmt.Printf("  ✗ Failed to update device name: %v\n", err)
				return nil, err
			}
			fmt.Printf("  ✓ Device name updated\n")
		}
		fmt.Printf("\n✓ Setup complete for %s (no device communication)\n", deviceId)
		return nil, nil
	}

	fmt.Printf("Setting up device %s\n", deviceId)

	// Configure WiFi if any WiFi options are specified (CLI-specific feature)
	if staEssid != "" || sta1Essid != "" || apPasswd != "" {
		fmt.Printf("  . Configuring WiFi settings on %s...\n", deviceId)
		wc, err := wifi.DoGetConfig(ctx, via, sd)
		if err != nil {
			fmt.Printf("  ✗ Failed to get WiFi config on %s: %v\n", deviceId, err)
			return nil, err
		}

		wifiModified := false

		// - set Wifi STA ESSID & passwd
		if staEssid != "" {
			wc.STA.SSID = staEssid
			wc.STA.Enable = true
			if staPasswd != "" {
				wc.STA.IsOpen = false
				wc.STA.Password = &staPasswd
			} else {
				wc.STA.IsOpen = true
			}
			wifiModified = true
		} else {
			wc.STA = nil
		}

		// - set Wifi STA1 ESSID & passwd
		if sta1Essid != "" {
			wc.STA1.SSID = sta1Essid
			wc.STA1.Enable = true
			if sta1Passwd != "" {
				wc.STA1.IsOpen = false
				wc.STA1.Password = &sta1Passwd
			} else {
				wc.STA1.IsOpen = true
			}
			wifiModified = true
		} else {
			wc.STA1 = nil
		}

		// - set Wifi AP password
		if apPasswd != "" {
			wc.AP.SSID = sd.Id() // Factory default SSID
			wc.AP.Password = &apPasswd
			wc.AP.Enable = true
			wc.AP.IsOpen = false
			wc.AP.RangeExtender = &wifi.RangeExtender{Enable: true}
			wifiModified = true
		} else {
			wc.AP = nil
		}

		if wifiModified {
			_, err = wifi.DoSetConfig(ctx, via, sd, wc)
			if err != nil {
				fmt.Printf("  ✗ Failed to set WiFi config on %s: %v\n", deviceId, err)
				return nil, err
			}
			fmt.Printf("  ✓ WiFi settings configured on %s\n", deviceId)
		}
	}

	// Get setup configuration (uses current process MQTT broker by default)
	cfg := getSetupConfig(ctx)

	fmt.Printf("  . Running core setup (firmware, MQTT, watchdog, auto-update)...\n")
	fmt.Printf("    MQTT broker: %s\n", cfg.MqttBroker)

	// Delegate to the internal setup package for core setup
	err := shellysetup.SetupDevice(ctx, log, sd, targetName, cfg)
	if err != nil {
		fmt.Printf("  ✗ Setup failed on %s: %v\n", deviceId, err)
		return nil, err
	}

	// Trigger device refresh via myhome RPC to sync DB (if client is available)
	if myhome.TheClient != nil {
		if _, refreshErr := myhome.TheClient.CallE(ctx, myhome.DeviceRefresh, sd.Id()); refreshErr != nil {
			log.V(1).Info("Could not trigger device refresh via RPC", "error", refreshErr)
		} else {
			fmt.Printf("  ✓ Device refresh triggered\n")
		}
	}

	fmt.Printf("\n✓ Setup complete for %s\n", deviceId)
	return nil, nil
}

var Cmd = &cobra.Command{
	Use:   `setup <device_identifier>`,
	Short: "Setup Shelly device(s) with the specified settings",
	Long: `Setup one or more Shelly devices with the specified settings.

Arguments:
  <device_identifier>  Device identifier: device ID, MAC address, hostname, IP address, or name pattern (e.g., 'shellyplus1-08b61fd9d708', '08:b6:1f:d9:d7:08', '192.168.1.58', '*radiateur*')

Flags:
  --name             Set device name (overrides auto-derivation from output/input names)
  --mqtt-broker      Override MQTT broker address
  --sta-essid        Configure WiFi STA ESSID
  --sta-passwd       Configure WiFi STA password`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deviceIdentifier := args[0]

		// Check if identifier is an IP address - use it directly
		if ip := net.ParseIP(deviceIdentifier); ip != nil {
			return setupDeviceByIP(cmd.Context(), deviceNameOverride, ip)
		}

		// Otherwise, lookup device by any identifier (id, MAC, hostname, name pattern)
		return setupDevicesByName(cmd.Context(), deviceIdentifier)
	},
}
