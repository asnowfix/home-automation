package setup

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"global"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"mynet"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/script"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
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

// options for MQTT
var mqttBroker string
var mqttPort int

func init() {
	Cmd.Flags().StringVar(&staEssid, "sta-essid", "", "STA ESSID")
	Cmd.Flags().StringVar(&staPasswd, "sta-passwd", "", "STA Password")
	Cmd.Flags().StringVar(&sta1Essid, "sta1-essid", "", "STA1 ESSID")
	Cmd.Flags().StringVar(&sta1Passwd, "sta1-passwd", "", "STA1 Password")
	Cmd.Flags().StringVar(&apPasswd, "ap-passwd", "", "AP Password")
	Cmd.Flags().StringVar(&mqttBroker, "mqtt-broker", "mqtt.local", "MQTT broker address")
	Cmd.Flags().IntVar(&mqttPort, "mqtt-port", 1883, "MQTT broker port")
	// No subcommands for setup
}

// setupDeviceByIP sets up a device using its IP address (initial setup mode)
func setupDeviceByIP(cmdCtx context.Context, name string, ip net.IP) error {
	longCtx := options.CommandLineContext(context.Background(), hlog.Logger, 2*time.Minute, global.Version(cmdCtx))
	_, err := myhome.Foreach(longCtx, hlog.Logger, ip.String(), types.ChannelHttp, doSetup, []string{name})
	return err
}

// setupDevicesByName sets up devices by looking them up by name pattern
func setupDevicesByName(cmdCtx context.Context, pattern string) error {
	longCtx := options.CommandLineContext(context.Background(), hlog.Logger, 2*time.Minute, global.Version(cmdCtx))
	_, err := myhome.Foreach(longCtx, hlog.Logger, pattern, types.ChannelHttp, doSetup, []string{})
	return err
}

// doSetup performs the actual setup logic for a single device
func doSetup(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("expected types.Device, got %T", device)
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

	// - set device name to args[0]
	fmt.Printf("  . Configuring system settings on %s...\n", deviceId)
	configModified := false
	config, err := system.GetConfig(ctx, via, sd)
	if err != nil {
		fmt.Printf("  ✗ Failed to get system config on %s: %v\n", deviceId, err)
		return nil, err
	}

	log.Info("Device config", "device", sd.Id(), "config", config)

	if len(args) > 0 && args[0] != "" && config.Device.Name != targetName {
		configModified = true
		config.Device.Name = targetName
	}

	// NTP Pool Project (recommended)
	// - pool.ntp.org
	// - Regional pools for better latency, e.g.:
	// 	- europe.pool.ntp.org
	// 	- north-america.pool.ntp.org
	// 	- asia.pool.ntp.org
	// These resolve to multiple servers run by volunteers worldwide.
	if config.Sntp.Server != "pool.ntp.org" {
		configModified = true
		config.Sntp.Server = "pool.ntp.org"
	}

	if configModified {
		_, err = system.SetConfig(ctx, via, sd, config)
		if err != nil {
			fmt.Printf("  ✗ Failed to set system config on %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("  ✓ System settings configured on %s (name: %s, NTP: %s)\n", deviceId, config.Device.Name, config.Sntp.Server)
	} else {
		fmt.Printf("  → System settings already configured on %s\n", deviceId)
	}

	// - set Wifi STA ESSID & passwd
	fmt.Printf("  . Configuring WiFi settings on %s...\n", deviceId)
	var wifiModified bool = false
	wc, err := wifi.DoGetConfig(ctx, via, sd)
	if err != nil {
		fmt.Printf("  ✗ Failed to get WiFi config on %s: %v\n", deviceId, err)
		return nil, err
	}
	log.Info("Current device wifi config", "device", sd.Id(), "config", wc)

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

	// - set Wifi AP password to arg[1]
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

	log.Info("Setting device wifi config", "device", sd.Id(), "config", wc)
	if wifiModified {
		_, err = wifi.DoSetConfig(ctx, via, sd, wc)
		if err != nil {
			fmt.Printf("  ✗ Failed to set WiFi config on %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("  ✓ WiFi settings configured on %s\n", deviceId)
	} else {
		fmt.Printf("  → WiFi settings not changed on %s\n", deviceId)
	}

	// - configure MQTT server
	fmt.Printf("  . Configuring MQTT broker on %s...\n", deviceId)
	if mqttBroker != "" {
		ips, err := mynet.MyResolver(hlog.Logger).LookupHost(ctx, mqttBroker)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP address resolved for %s", mqttBroker)
		}
		mqttBroker = ips[0].String()
		_, err = mqtt.SetServer(ctx, via, sd, mqttBroker+":"+strconv.Itoa(mqttPort))
		if err != nil {
			fmt.Printf("  ✗ Failed to set MQTT broker on %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("  ✓ MQTT broker configured on %s (%s:%d)\n", deviceId, mqttBroker, mqttPort)
	} else {
		fmt.Printf("  → MQTT broker not configured on %s\n", deviceId)
	}

	status, err := system.GetStatus(ctx, via, sd)
	if err != nil {
		fmt.Printf("  ✗ Failed to get device status on %s: %v\n", deviceId, err)
		return nil, err
	}
	log.Info("Device status", "device", sd.Id(), "status", status)

	// reboot device, if necessary (required after MQTT configuration change)
	if status.RestartRequired {
		fmt.Printf("  . Rebooting %s (required after configuration changes)...\n", deviceId)
		hlog.Logger.Info("Device rebooting", "device", sd.Id())
		err = shelly.DoReboot(ctx, sd)
		if err != nil {
			fmt.Printf("  ✗ Failed to reboot %s: %v\n", deviceId, err)
			return nil, err
		}

		// Wait for device to go offline (reboot started)
		fmt.Printf("  . Waiting for %s to go offline...\n", deviceId)
		time.Sleep(5 * time.Second)

		// Wait for device to come back online
		fmt.Printf("  . Waiting for %s to come back online...\n", deviceId)
		maxRetries := 20 // 20 * 3 seconds = 60 seconds max
		for i := 0; i < maxRetries; i++ {
			time.Sleep(3 * time.Second)
			status, err = system.GetStatus(ctx, via, sd)
			if err == nil {
				// Device is back online
				fmt.Printf("  ✓ %s rebooted successfully\n", deviceId)
				hlog.Logger.Info("Device rebooted", "device", sd.Id(), "status", status)
				break
			}
			if i == maxRetries-1 {
				fmt.Printf("  ✗ %s did not come back online after reboot\n", deviceId)
				return nil, fmt.Errorf("device did not come back online after reboot")
			}
		}
	}

	// load watchdog.js as script #1
	// - Check if watchdog.js is already loaded as script #1
	fmt.Printf("  . Setting up watchdog script on %s...\n", deviceId)
	loaded, err := script.ListLoaded(ctx, via, sd)
	if err != nil {
		fmt.Printf("  ✗ Failed to list loaded scripts on %s: %v\n", deviceId, err)
		return nil, err
	}
	ok = false
	for _, s := range loaded {
		if s.Name == "watchdog.js" {
			log.Info("watchdog.js is already loaded")
			if s.Running && s.Id == 1 {
				log.Info("watchdog.js is already running as script #1")
				fmt.Printf("  → Watchdog script already running on %s (id: %d)\n", deviceId, s.Id)
				ok = true
				break
			}
			err := fmt.Errorf("watchdog.js is already loaded but not running as script #1 on device %s", sd.Id())
			log.Error(err, "watchdog.js improper configuration", "device", sd.Id(), "script_id", s.Id)
			fmt.Printf("  ✗ Watchdog script improperly configured on %s (id: %d, expected: 1)\n", deviceId, s.Id)
			return nil, err
		}
	}
	if !ok {
		// Not already in place: upload, ...
		fmt.Printf("    - Uploading watchdog.js to %s...\n", deviceId)
		id, err := script.Upload(ctx, via, sd, "watchdog.js", true, false)
		if err != nil {
			fmt.Printf("  ✗ Failed to upload watchdog script to %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("    ✓ Uploaded to %s (id: %d)\n", deviceId, id)

		// ...enable (auto-restart at boot, ...
		fmt.Printf("    - Enabling auto-start on boot for %s...\n", deviceId)
		_, err = script.EnableDisable(ctx, via, sd, "watchdog.js", true)
		if err != nil {
			fmt.Printf("  ✗ Failed to enable watchdog script on %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("    ✓ Enabled on %s\n", deviceId)

		// ...and start it.
		fmt.Printf("    - Starting watchdog script on %s...\n", deviceId)
		_, err = script.StartStopDelete(ctx, via, sd, "watchdog.js", script.Start)
		if err != nil {
			fmt.Printf("  ✗ Failed to start watchdog script on %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("    ✓ Started on %s\n", deviceId)
		fmt.Printf("  ✓ Watchdog script setup complete on %s\n", deviceId)
	}

	fmt.Printf("\nSetup complete for %s\n", deviceId)
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
