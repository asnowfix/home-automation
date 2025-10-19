package setup

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"global"
	"hlog"
	mhscript "internal/myhome/shelly/script"
	"myhome"
	"mynet"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/matter"
	"pkg/shelly/mqtt"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"schedule"
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
func setupDeviceByIP(cmdCtx context.Context, name string, ip net.IP) error {
	// Setup includes firmware updates and script uploads which can take a long time
	longCtx := global.ContextWithoutTimeout(cmdCtx, hlog.Logger)
	_, err := myhome.Foreach(longCtx, hlog.Logger, ip.String(), types.ChannelHttp, doSetup, []string{name})
	return err
}

// setupDevicesByName sets up devices by looking them up by name pattern
func setupDevicesByName(cmdCtx context.Context, pattern string) error {
	// Setup includes firmware updates and script uploads which can take a long time
	longCtx := global.ContextWithoutTimeout(cmdCtx, hlog.Logger)
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

	// Check for and apply firmware updates BEFORE configuring MQTT and scripts
	// This ensures we're working with the latest firmware
	fmt.Printf("  . Checking for firmware updates on %s...\n", deviceId)
	// Create a context without timeout for potentially long update process
	updateCtx := global.ContextWithoutTimeout(ctx, log)
	err = checkAndApplyUpdates(updateCtx, log, via, sd, deviceId)
	if err != nil {
		fmt.Printf("  ✗ Failed to check/apply updates on %s: %v\n", deviceId, err)
		return nil, err
	}

	// Disable Matter component immediately after firmware update
	// This ensures we're working with the latest firmware's Matter implementation
	fmt.Printf("  . Disabling Matter component on %s...\n", deviceId)
	err = matter.Disable(ctx, via, sd)
	if err != nil {
		// Matter might not be available on all devices, so just log and continue
		log.Info("Unable to disable Matter (may not be supported on this device)", "device", sd.Id(), "error", err)
		fmt.Printf("  → Matter not available on %s (may not be supported)\n", deviceId)
	} else {
		fmt.Printf("  ✓ Matter disabled on %s\n", deviceId)
	}

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
		
		// Use timeout-free context for reboot since device won't respond (it reboots immediately)
		rebootCtx := global.ContextWithoutTimeout(ctx, log)
		err = shelly.DoReboot(rebootCtx, sd)
		// Ignore timeout errors for reboot - they're expected since device reboots immediately
		if err != nil && !isTimeoutError(err) {
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
		
		// Wait additional time for all services to fully initialize
		// The device may respond to status checks but internal services (like script engine) need more time
		fmt.Printf("  . Waiting for %s services to fully initialize...\n", deviceId)
		time.Sleep(10 * time.Second)
	}

	// load watchdog.js as script #1
	// - Check if watchdog.js is already loaded as script #1
	fmt.Printf("  . Setting up watchdog script on %s...\n", deviceId)
	loaded, err := pkgscript.ListLoaded(ctx, via, sd)
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
		buf, err := pkgscript.ReadEmbeddedFile("watchdog.js")
		if err != nil {
			fmt.Printf("  ✗ Failed to read watchdog script: %v\n", err)
			return nil, err
		}
		id, err := mhscript.UploadWithVersion(ctx, log, via, sd, "watchdog.js", buf, true, false)
		if err != nil {
			fmt.Printf("  ✗ Failed to upload watchdog script to %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("    ✓ Uploaded to %s (id: %d)\n", deviceId, id)

		// ...enable (auto-restart at boot, ...
		fmt.Printf("    - Enabling auto-start on boot for %s...\n", deviceId)
		_, err = pkgscript.EnableDisable(ctx, via, sd, "watchdog.js", true)
		if err != nil {
			fmt.Printf("  ✗ Failed to enable watchdog script on %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("    ✓ Enabled on %s\n", deviceId)

		// ...and start it.
		fmt.Printf("    - Starting watchdog script on %s...\n", deviceId)
		_, err = pkgscript.StartStopDelete(ctx, via, sd, "watchdog.js", pkgscript.Start)
		if err != nil {
			fmt.Printf("  ✗ Failed to start watchdog script on %s: %v\n", deviceId, err)
			return nil, err
		}
		fmt.Printf("    ✓ Started on %s\n", deviceId)
		fmt.Printf("  ✓ Watchdog script setup complete on %s\n", deviceId)
	}

	// Setup auto-update scheduled job
	fmt.Printf("  . Setting up auto-update job on %s...\n", deviceId)
	err = setupAutoUpdateJob(ctx, log, via, sd, deviceId)
	if err != nil {
		fmt.Printf("  ✗ Failed to setup auto-update job on %s: %v\n", deviceId, err)
		return nil, err
	}

	fmt.Printf("\nSetup complete for %s\n", deviceId)
	return nil, nil
}

// setupAutoUpdateJob creates or updates a scheduled job for Shelly.Update
// The job runs at a random time between 03:00 and 05:00 every weekday
func setupAutoUpdateJob(ctx context.Context, log logr.Logger, via types.Channel, sd *shellyapi.Device, deviceId string) error {
	// Get existing jobs
	out, err := schedule.ShowJobs(ctx, log, via, sd)
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}
	
	scheduled := out.(*schedule.Scheduled)
	
	// Generate random time between 03:00 and 05:00
	// Random hour: 3 or 4
	// Random minute: 0-59
	randomHour := 3 + rand.Intn(2)  // 3 or 4
	randomMinute := rand.Intn(60)    // 0-59
	
	// Cron format: second minute hour day month weekday
	// 0 <minute> <hour> * * SUN,MON,TUE,WED,THU,FRI,SAT
	timespec := fmt.Sprintf("0 %d %d * * SUN,MON,TUE,WED,THU,FRI,SAT", randomMinute, randomHour)
	
	// Look for existing Shelly.Update job
	var existingJobId *uint32
	for _, job := range scheduled.Jobs {
		for _, call := range job.Calls {
			if call.Method == "Shelly.Update" {
				existingJobId = &job.JobId.Id
				log.Info("Found existing Shelly.Update job", "job_id", job.JobId.Id, "timespec", job.Timespec)
				break
			}
		}
		if existingJobId != nil {
			break
		}
	}
	
	// Create the job spec
	jobSpec := schedule.JobSpec{
		Enable:   true,
		Timespec: timespec,
		Calls: []schedule.JobCall{
			{
				Method: "Shelly.Update",
				Params: map[string]interface{}{
					"stage": "stable",
				},
			},
		},
	}
	
	if existingJobId != nil {
		// Update existing job
		job := schedule.Job{
			JobId:   schedule.JobId{Id: *existingJobId},
			JobSpec: jobSpec,
		}
		_, err = sd.CallE(ctx, via, string(schedule.Update), &job)
		if err != nil {
			return fmt.Errorf("failed to update job: %w", err)
		}
		fmt.Printf("    ✓ Updated auto-update job on %s (id: %d, time: %02d:%02d daily)\n", deviceId, *existingJobId, randomHour, randomMinute)
	} else {
		// Create new job
		result, err := sd.CallE(ctx, via, string(schedule.Create), jobSpec)
		if err != nil {
			return fmt.Errorf("failed to create job: %w", err)
		}
		jobId := result.(*schedule.JobId)
		fmt.Printf("    ✓ Created auto-update job on %s (id: %d, time: %02d:%02d daily)\n", deviceId, jobId.Id, randomHour, randomMinute)
	}
	
	return nil
}

// checkAndApplyUpdates checks for firmware updates and applies them repeatedly until no more updates are available
func checkAndApplyUpdates(ctx context.Context, log logr.Logger, via types.Channel, sd *shellyapi.Device, deviceId string) error {
	maxIterations := 5 // Safety limit to prevent infinite loops
	iteration := 0
	
	for iteration < maxIterations {
		iteration++
		
		// Check for available updates
		updateInfo, err := shelly.DoCheckForUpdate(ctx, via, sd)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		
		// Check if stable update is available
		if updateInfo.Stable == nil || updateInfo.Stable.Version == "" {
			if iteration == 1 {
				fmt.Printf("    ✓ Firmware is up to date on %s\n", deviceId)
			} else {
				fmt.Printf("    ✓ All updates applied on %s\n", deviceId)
			}
			return nil
		}
		
		// Update available
		fmt.Printf("    → Stable firmware %s (build %s) available on %s\n", 
			updateInfo.Stable.Version, updateInfo.Stable.BuildId, deviceId)
		fmt.Printf("    - Applying update on %s...\n", deviceId)
		
		// Apply the update
		err = shelly.DoUpdate(ctx, via, sd, "stable")
		if err != nil {
			return fmt.Errorf("failed to initiate update: %w", err)
		}
		
		// Wait for update to complete
		// The device will reboot during the update process
		fmt.Printf("    - Waiting for %s to update and reboot (this may take 2-3 minutes)...\n", deviceId)
		time.Sleep(10 * time.Second) // Initial wait for update to start
		
		// Wait for device to go offline (update started)
		fmt.Printf("    - Waiting for %s to go offline for update...\n", deviceId)
		time.Sleep(30 * time.Second)
		
		// Wait for device to come back online after update
		fmt.Printf("    - Waiting for %s to come back online...\n", deviceId)
		maxRetries := 40 // 40 * 5 seconds = 200 seconds (3+ minutes)
		deviceOnline := false
		for i := 0; i < maxRetries; i++ {
			time.Sleep(5 * time.Second)
			status, err := system.GetStatus(ctx, via, sd)
			if err == nil && status != nil {
				// Device is back online
				deviceOnline = true
				fmt.Printf("    ✓ Update completed on %s\n", deviceId)
				log.Info("Device updated and rebooted", "device", sd.Id(), "iteration", iteration)
				break
			}
			if i == maxRetries-1 {
				return fmt.Errorf("device did not come back online after update (waited %d seconds)", maxRetries*5)
			}
		}
		
		if !deviceOnline {
			return fmt.Errorf("device did not come back online after update")
		}
		
		// Check if more updates are available
		fmt.Printf("    - Checking for additional updates on %s...\n", deviceId)
	}
	
	// If we hit the max iterations, warn but don't fail
	fmt.Printf("    ⚠ Reached maximum update iterations (%d) on %s\n", maxIterations, deviceId)
	log.Info("Reached maximum update iterations", "device", sd.Id(), "iterations", maxIterations)
	return nil
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
