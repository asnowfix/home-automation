package setup

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/jackpal/gateway"
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

var Cmd = &cobra.Command{
	Use:   `setup <device_name> <device_ip>`,
	Short: "Shelly devices features",
	Long: `Setup a new Shelly device with the specified settings.

Arguments:
  <device_name>    Name to assign to the Shelly device`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		var ip net.IP

		if len(args) > 1 {
			ip = net.ParseIP(args[1])
			if ip == nil {
				return fmt.Errorf("invalid IP address: %s", args[1])
			}
		} else {
			gateways, err := gateway.DiscoverGateways()
			if err != nil {
				return err
			}
			for _, g := range gateways {
				if g.String() == "192.168.33.1" || g.String() == "192.168.34.1" {
					ip = g.To4()
					break
				}
			}
			if ip == nil {
				return fmt.Errorf("no gateway is a shelly device to be configured")
			}
		}

		// If we are connected to a shelly device
		// Use a long-lived context decoupled from the global command timeout
		longCtx := options.CommandLineContext(context.Background(), hlog.Logger, 2*time.Minute, global.Version(cmd.Context()))
		myhome.Foreach(longCtx, hlog.Logger, ip.String(), types.ChannelHttp, func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
			sd, ok := device.(*shellyapi.Device)
			if !ok {
				return nil, fmt.Errorf("expected types.Device, got %T", device)
			}

			fmt.Printf("Setting up device %s (%s)\n", name, sd.Id())

			// - set device name to args[0]
			fmt.Printf("  . Configuring system settings...\n")
			configModified := false
			config, err := system.GetConfig(ctx, via, sd)
			if err != nil {
				fmt.Printf("  ✗ Failed to get system config: %v\n", err)
				return nil, err
			}

			log.Info("Device config", "device", sd.Id(), "config", config)

			if config.Device.Name != name {
				configModified = true
				config.Device.Name = name
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
					fmt.Printf("  ✗ Failed to set system config: %v\n", err)
					return nil, err
				}
				fmt.Printf("  ✓ System settings configured (name: %s, NTP: %s)\n", config.Device.Name, config.Sntp.Server)
			} else {
				fmt.Printf("  → System settings already configured\n")
			}

			// - set Wifi STA ESSID & passwd
			fmt.Printf("  . Configuring WiFi settings...\n")
			var wifiModified bool = false
			wc, err := wifi.DoGetConfig(ctx, via, sd)
			if err != nil {
				fmt.Printf("  ✗ Failed to get WiFi config: %v\n", err)
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
					fmt.Printf("  ✗ Failed to set WiFi config: %v\n", err)
					return nil, err
				}
				fmt.Printf("  ✓ WiFi settings configured\n")
			} else {
				fmt.Printf("  → WiFi settings not changed\n")
			}

			// - configure MQTT server
			fmt.Printf("  . Configuring MQTT broker...\n")
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
					fmt.Printf("  ✗ Failed to set MQTT broker: %v\n", err)
					return nil, err
				}
				fmt.Printf("  ✓ MQTT broker configured (%s:%d)\n", mqttBroker, mqttPort)
			} else {
				fmt.Printf("  → MQTT broker not configured\n")
			}

			status, err := system.GetStatus(ctx, via, sd)
			if err != nil {
				fmt.Printf("  ✗ Failed to get device status: %v\n", err)
				return nil, err
			}
			log.Info("Device status", "device", sd.Id(), "status", status)

			// reboot device, if necessary (required after MQTT configuration change)
			if status.RestartRequired {
				fmt.Printf("  . Rebooting device (required after configuration changes)...\n")
				hlog.Logger.Info("Device rebooting", "device", sd.Id())
				err = shelly.DoReboot(ctx, sd)
				if err != nil {
					fmt.Printf("  ✗ Failed to reboot device: %v\n", err)
					return nil, err
				}

				// Wait for device to go offline (reboot started)
				fmt.Printf("  . Waiting for device to go offline...\n")
				time.Sleep(5 * time.Second)
				
				// Wait for device to come back online
				fmt.Printf("  . Waiting for device to come back online...\n")
				maxRetries := 20 // 20 * 3 seconds = 60 seconds max
				for i := 0; i < maxRetries; i++ {
					time.Sleep(3 * time.Second)
					status, err = system.GetStatus(ctx, via, sd)
					if err == nil {
						// Device is back online
						fmt.Printf("  ✓ Device rebooted successfully\n")
						hlog.Logger.Info("Device rebooted", "device", sd.Id(), "status", status)
						break
					}
					if i == maxRetries-1 {
						fmt.Printf("  ✗ Device did not come back online after reboot\n")
						return nil, fmt.Errorf("device did not come back online after reboot")
					}
				}
			}

			// load watchdog.js as script #1
			// - Check if watchdog.js is already loaded as script #1
			fmt.Printf("  . Setting up watchdog script...\n")
			loaded, err := script.ListLoaded(ctx, via, sd)
			if err != nil {
				fmt.Printf("  ✗ Failed to list loaded scripts: %v\n", err)
				return nil, err
			}
			ok = false
			for _, s := range loaded {
				if s.Name == "watchdog.js" {
					log.Info("watchdog.js is already loaded")
					if s.Running && s.Id == 1 {
						log.Info("watchdog.js is already running as script #1")
						fmt.Printf("  → Watchdog script already running (id: %d)\n", s.Id)
						ok = true
						break
					}
					err := fmt.Errorf("watchdog.js is already loaded but not running as script #1 on device %s", sd.Id())
					log.Error(err, "watchdog.js improper configuration", "device", sd.Id(), "script_id", s.Id)
					fmt.Printf("  ✗ Watchdog script improperly configured (id: %d, expected: 1)\n", s.Id)
					return nil, err
				}
			}
			if !ok {
				// Not already in place: upload, ...
				fmt.Printf("    - Uploading watchdog.js...\n")
				id, err := script.Upload(ctx, via, sd, "watchdog.js", true, false)
				if err != nil {
					fmt.Printf("  ✗ Failed to upload watchdog script: %v\n", err)
					return nil, err
				}
				fmt.Printf("    ✓ Uploaded (id: %d)\n", id)
				
				// ...enable (auto-restart at boot, ...
				fmt.Printf("    - Enabling auto-start on boot...\n")
				_, err = script.EnableDisable(ctx, via, sd, "watchdog.js", true)
				if err != nil {
					fmt.Printf("  ✗ Failed to enable watchdog script: %v\n", err)
					return nil, err
				}
				fmt.Printf("    ✓ Enabled\n")
				
				// ...and start it.
				fmt.Printf("    - Starting watchdog script...\n")
				_, err = script.StartStopDelete(ctx, via, sd, "watchdog.js", script.Start)
				if err != nil {
					fmt.Printf("  ✗ Failed to start watchdog script: %v\n", err)
					return nil, err
				}
				fmt.Printf("    ✓ Started\n")
				fmt.Printf("  ✓ Watchdog script setup complete\n")
			}

			fmt.Printf("\nSetup complete for %s (%s)\n", name, sd.Id())
			return nil, nil
		}, []string{args[0]})

		return nil
	},
}
