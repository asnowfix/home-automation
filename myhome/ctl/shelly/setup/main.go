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
	"mynet"
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

func init() {
	Cmd.Flags().StringVar(&staEssid, "sta-essid", "", "STA ESSID")
	Cmd.Flags().StringVar(&staPasswd, "sta-passwd", "", "STA Password")
	Cmd.Flags().StringVar(&sta1Essid, "sta1-essid", "", "STA1 ESSID")
	Cmd.Flags().StringVar(&sta1Passwd, "sta1-passwd", "", "STA1 Password")
	Cmd.Flags().StringVar(&apPasswd, "ap-passwd", "", "AP Password")
	Cmd.Flags().StringVar(&mqttBrokerOverride, "mqtt-broker", "", "Override MQTT broker address (default: use current process broker)")
	// No subcommands for setup
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

	// Get device name from args if provided (initial setup), otherwise use existing name
	var targetName string
	if len(args) > 0 && args[0] != "" {
		targetName = args[0]
	} else {
		targetName = sd.Name()
	}

	// Device identifier for all output lines
	deviceId := fmt.Sprintf("%s (%s)", targetName, sd.Id())

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

	fmt.Printf("\n✓ Setup complete for %s\n", deviceId)
	return nil, nil
}

var Cmd = &cobra.Command{
	Use:   `setup <device_name> [device_ip]`,
	Short: "Setup Shelly device(s) with the specified settings",
	Long: `Setup one or more Shelly devices with the specified settings.

Arguments:
  <device_name>    Name or pattern to match device(s) (e.g., 'my-device' or '*radiateur*')
  [device_ip]      Optional IP address for initial setup of a new device`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		devicePattern := args[0]

		// If IP address is provided, use it directly (initial setup mode)
		if len(args) > 1 {
			ip := net.ParseIP(args[1])
			if ip == nil {
				return fmt.Errorf("invalid IP address: %s", args[1])
			}

			// For initial setup with IP, use the IP directly
			return setupDeviceByIP(cmd.Context(), devicePattern, ip)
		}

		// No IP provided - lookup devices by name pattern
		return setupDevicesByName(cmd.Context(), devicePattern)
	},
}
