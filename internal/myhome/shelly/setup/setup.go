package setup

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"

	mhscript "internal/myhome/shelly/script"
	"mynet"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/input"
	"pkg/shelly/kvs"
	"pkg/shelly/matter"
	"pkg/shelly/mqtt"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/shelly"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"schedule"
)

// Config holds configuration options for device setup
type Config struct {
	// MqttBroker is the MQTT broker hostname or IP (e.g., "mqtt.local" or "192.168.1.100")
	MqttBroker string
	// MqttPort is the MQTT broker port (default 1883)
	MqttPort int
	// Resolver is used for DNS lookups
	Resolver mynet.Resolver
}

// WifiConfig holds WiFi configuration options for device setup
type WifiConfig struct {
	StaEssid   string // WiFi STA ESSID
	StaPasswd  string // WiFi STA password
	Sta1Essid  string // WiFi STA1 ESSID
	Sta1Passwd string // WiFi STA1 password
	ApPasswd   string // WiFi AP password
}

// DefaultConfig returns a Config with default values
func DefaultConfig() Config {
	return Config{
		MqttBroker: "mqtt.local",
		MqttPort:   1883,
	}
}

// SetupDeviceWithWifi performs the full setup of a Shelly device with optional WiFi configuration.
// See SetupDevice for details on what setup includes.
func SetupDeviceWithWifi(ctx context.Context, log logr.Logger, sd *shellyapi.Device, targetName string, cfg Config, wifiCfg WifiConfig) error {
	// Auto-detect best available channel: prefer HTTP, fall back to MQTT
	via, err := selectChannel(sd)
	if err != nil {
		return fmt.Errorf("cannot setup device: %w", err)
	}

	// Configure WiFi if any WiFi options are specified
	if wifiCfg.StaEssid != "" || wifiCfg.Sta1Essid != "" || wifiCfg.ApPasswd != "" {
		deviceId := fmt.Sprintf("%s (%s)", targetName, sd.Id())
		fmt.Printf("  . Configuring WiFi settings on %s...\n", deviceId)
		wc, err := wifi.DoGetConfig(ctx, via, sd)
		if err != nil {
			fmt.Printf("  ✗ Failed to get WiFi config on %s: %v\n", deviceId, err)
			return err
		}

		wifiModified := false

		// Set WiFi STA ESSID & passwd
		if wifiCfg.StaEssid != "" {
			wc.STA.SSID = wifiCfg.StaEssid
			wc.STA.Enable = true
			if wifiCfg.StaPasswd != "" {
				wc.STA.IsOpen = false
				wc.STA.Password = &wifiCfg.StaPasswd
			} else {
				wc.STA.IsOpen = true
			}
			wifiModified = true
		} else {
			wc.STA = nil
		}

		// Set WiFi STA1 ESSID & passwd
		if wifiCfg.Sta1Essid != "" {
			wc.STA1.SSID = wifiCfg.Sta1Essid
			wc.STA1.Enable = true
			if wifiCfg.Sta1Passwd != "" {
				wc.STA1.IsOpen = false
				wc.STA1.Password = &wifiCfg.Sta1Passwd
			} else {
				wc.STA1.IsOpen = true
			}
			wifiModified = true
		} else {
			wc.STA1 = nil
		}

		// Set WiFi AP password
		if wifiCfg.ApPasswd != "" {
			wc.AP.SSID = sd.Id() // Factory default SSID
			wc.AP.Password = &wifiCfg.ApPasswd
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
				return err
			}
			fmt.Printf("  ✓ WiFi settings configured on %s\n", deviceId)
		}
	}

	// Delegate to the main setup function
	return SetupDevice(ctx, log, sd, targetName, cfg)
}

// selectChannel returns the best available channel for communicating with the device.
// Prefers MQTT if available (more reliable for devices discovered via MQTT), falls back to HTTP.
func selectChannel(sd *shellyapi.Device) (types.Channel, error) {
	httpReady := sd.IsHttpReady()
	mqttReady := sd.IsMqttReady()

	// Prefer MQTT - it's more reliable for devices that may have intermittent HTTP connectivity
	if mqttReady {
		return types.ChannelMqtt, nil
	}
	if httpReady {
		return types.ChannelHttp, nil
	}
	// Neither channel is ready
	return types.ChannelDefault, fmt.Errorf("device %s (%s) has no available communication channel (HTTP ready: %v, MQTT ready: %v, IP: %v)",
		sd.Name(), sd.Id(), httpReady, mqttReady, sd.Ip())
}

// SetupDevice performs the full setup of a Shelly device:
// - Configures system settings (name, NTP)
// - Checks and applies firmware updates
// - Disables Matter
// - Configures MQTT broker
// - Uploads and starts watchdog.js script
// - Sets up auto-update scheduled job
//
// This function can be called from both CLI and daemon contexts.
// The targetName parameter is optional - if empty, the device's current name is used.
func SetupDevice(ctx context.Context, log logr.Logger, sd *shellyapi.Device, targetName string, cfg Config) error {
	// Auto-detect best available channel: prefer HTTP, fall back to MQTT
	via, err := selectChannel(sd)
	if err != nil {
		return fmt.Errorf("cannot setup device: %w", err)
	}

	// Use device's current name if no target name specified
	if targetName == "" {
		targetName = sd.Name()
	}

	// Device identifier for all log messages
	deviceId := fmt.Sprintf("%s (%s)", targetName, sd.Id())

	log.Info("Setting up device", "device", deviceId)

	// Configure system settings
	log.Info("Configuring system settings", "device", deviceId)
	configModified := false
	config, err := system.GetConfig(ctx, via, sd)
	if err != nil {
		log.Error(err, "Failed to get system config", "device", deviceId)
		return fmt.Errorf("failed to get system config: %w", err)
	}

	// Device name priority:
	// 1. If targetName is explicitly provided (via --name flag), use it
	// 2. If device name equals device ID, try to derive from output/input names
	// 3. Otherwise, preserve existing device name
	if targetName != "" && targetName != sd.Name() && targetName != config.Device.Name {
		configModified = true
		config.Device.Name = targetName
		fmt.Printf("  ✓ Setting device name: %s\n", targetName)
		log.Info("Setting device name", "device", deviceId, "name", targetName)
	} else if config.Device.Name == "" || config.Device.Name == sd.Id() {
		// Try to derive a better name from output or input
		derivedName := deriveDeviceName(ctx, log, via, sd)
		if derivedName != "" {
			configModified = true
			config.Device.Name = derivedName
			fmt.Printf("  ✓ Derived device name: %s\n", derivedName)
			log.Info("Derived device name from output/input", "device", deviceId, "derived_name", derivedName)
		} else {
			log.V(1).Info("Could not derive device name from output/input", "device", deviceId)
		}
	} else {
		log.Info("Preserving existing device name", "device", deviceId, "name", config.Device.Name)
	}

	// NTP Pool Project (recommended)
	if config.Sntp.Server != "pool.ntp.org" {
		configModified = true
		config.Sntp.Server = "pool.ntp.org"
	}

	if configModified {
		_, err = system.SetConfig(ctx, via, sd, config)
		if err != nil {
			log.Error(err, "Failed to set system config", "device", deviceId)
			return fmt.Errorf("failed to set system config: %w", err)
		}
		log.Info("System settings configured", "device", deviceId, "name", config.Device.Name, "ntp", config.Sntp.Server)
	} else {
		log.Info("System settings already configured", "device", deviceId)
	}

	// Check for and apply firmware updates BEFORE configuring MQTT and scripts
	log.Info("Checking for firmware updates", "device", deviceId)
	err = checkAndApplyUpdates(ctx, log, via, sd, deviceId)
	if err != nil {
		// Update check failure is non-fatal - it depends on Shelly cloud availability
		// The watchdog.js script will handle updates later
		log.Info("Skipping firmware update check (non-fatal)", "device", deviceId, "reason", err.Error())
	}

	// Disable Matter component immediately after firmware update
	log.Info("Disabling Matter component", "device", deviceId)
	err = matter.Disable(ctx, via, sd)
	if err != nil {
		// Matter might not be available on all devices, so just log and continue
		log.Info("Unable to disable Matter (may not be supported on this device)", "device", deviceId, "error", err)
	} else {
		log.Info("Matter disabled", "device", deviceId)
	}

	// Configure MQTT server
	log.Info("Configuring MQTT broker", "device", deviceId)
	if cfg.MqttBroker != "" {
		mqttServer := cfg.MqttBroker

		// If MqttPort is 0, the broker string already includes the port (e.g., "192.168.1.1:1883")
		// Otherwise, we need to resolve the hostname and append the port
		if cfg.MqttPort == 0 {
			// Broker already includes port, use as-is
			log.Info("Using MQTT broker with embedded port", "server", mqttServer)
		} else {
			// Need to resolve hostname and append port
			if cfg.Resolver != nil {
				ips, err := cfg.Resolver.LookupHost(ctx, cfg.MqttBroker)
				if err != nil {
					return fmt.Errorf("failed to resolve MQTT broker %s: %w", cfg.MqttBroker, err)
				}
				if len(ips) == 0 {
					return fmt.Errorf("no IP address resolved for %s", cfg.MqttBroker)
				}
				mqttServer = ips[0].String()
			}
			mqttServer = mqttServer + ":" + strconv.Itoa(cfg.MqttPort)
		}

		log.Info("Setting MQTT broker", "device", deviceId, "server", mqttServer, "via", via, "http_ready", sd.IsHttpReady(), "mqtt_ready", sd.IsMqttReady())
		_, err = mqtt.SetServer(ctx, via, sd, mqttServer)
		if err != nil {
			log.Error(err, "Failed to set MQTT broker", "device", deviceId, "via", via, "http_ready", sd.IsHttpReady(), "mqtt_ready", sd.IsMqttReady())
			return fmt.Errorf("failed to set MQTT broker: %w", err)
		}
		log.Info("MQTT broker configured", "device", deviceId, "server", mqttServer)
	} else {
		log.Info("MQTT broker not configured (no broker specified)", "device", deviceId)
	}

	status, err := system.GetStatus(ctx, via, sd)
	if err != nil {
		log.Error(err, "Failed to get device status", "device", deviceId)
		return fmt.Errorf("failed to get device status: %w", err)
	}

	// Reboot device if necessary (required after MQTT configuration change)
	if status.RestartRequired {
		log.Info("Rebooting device (required after configuration changes)", "device", deviceId)

		err = shelly.DoReboot(ctx, sd)
		if err != nil {
			log.Error(err, "Failed to reboot device", "device", deviceId)
			return fmt.Errorf("failed to reboot device: %w", err)
		}

		// Wait for device to go offline (reboot started)
		log.Info("Waiting for device to go offline", "device", deviceId)
		time.Sleep(5 * time.Second)

		// Wait for device to come back online
		log.Info("Waiting for device to come back online", "device", deviceId)
		maxRetries := 20 // 20 * 3 seconds = 60 seconds max
		for i := 0; i < maxRetries; i++ {
			time.Sleep(3 * time.Second)
			status, err = system.GetStatus(ctx, via, sd)
			if err == nil {
				// Device is back online
				log.Info("Device rebooted successfully", "device", deviceId)
				break
			}
			if i == maxRetries-1 {
				log.Error(nil, "Device did not come back online after reboot", "device", deviceId)
				return fmt.Errorf("device did not come back online after reboot")
			}
		}

		// Wait additional time for all services to fully initialize
		log.Info("Waiting for device services to fully initialize", "device", deviceId)
		time.Sleep(10 * time.Second)
	}

	// Load watchdog.js as script #1
	log.Info("Setting up watchdog script", "device", deviceId)
	loaded, err := pkgscript.ListLoaded(ctx, via, sd)
	if err != nil {
		log.Error(err, "Failed to list loaded scripts", "device", deviceId)
		return fmt.Errorf("failed to list loaded scripts: %w", err)
	}
	watchdogOk := false
	for _, s := range loaded {
		if s.Name == "watchdog.js" {
			log.Info("watchdog.js is already loaded", "device", deviceId)
			if s.Running && s.Id == 1 {
				log.Info("watchdog.js is already running as script #1", "device", deviceId)
				watchdogOk = true
				break
			}
			err := fmt.Errorf("watchdog.js is already loaded but not running as script #1 on device %s", sd.Id())
			log.Error(err, "watchdog.js improper configuration", "device", deviceId, "script_id", s.Id)
			return err
		}
	}
	if !watchdogOk {
		// Not already in place: upload, enable, and start
		log.Info("Uploading watchdog.js", "device", deviceId)
		buf, err := pkgscript.ReadEmbeddedFile("watchdog.js")
		if err != nil {
			log.Error(err, "Failed to read watchdog script", "device", deviceId)
			return fmt.Errorf("failed to read watchdog script: %w", err)
		}
		id, err := mhscript.UploadWithVersion(ctx, log, via, sd, "watchdog.js", buf, true, false)
		if err != nil {
			log.Error(err, "Failed to upload watchdog script", "device", deviceId)
			return fmt.Errorf("failed to upload watchdog script: %w", err)
		}
		log.Info("Uploaded watchdog.js", "device", deviceId, "id", id)

		// Enable auto-restart at boot
		log.Info("Enabling auto-start on boot", "device", deviceId)
		_, err = pkgscript.EnableDisable(ctx, via, sd, "watchdog.js", true)
		if err != nil {
			log.Error(err, "Failed to enable watchdog script", "device", deviceId)
			return fmt.Errorf("failed to enable watchdog script: %w", err)
		}

		// Start it
		log.Info("Starting watchdog script", "device", deviceId)
		_, err = pkgscript.StartStopDelete(ctx, via, sd, "watchdog.js", pkgscript.Start)
		if err != nil {
			log.Error(err, "Failed to start watchdog script", "device", deviceId)
			return fmt.Errorf("failed to start watchdog script: %w", err)
		}
		log.Info("Watchdog script setup complete", "device", deviceId)
	}

	// Setup auto-update scheduled job
	log.Info("Setting up auto-update job", "device", deviceId)
	err = setupAutoUpdateJob(ctx, log, via, sd, deviceId)
	if err != nil {
		log.Error(err, "Failed to setup auto-update job", "device", deviceId)
		return fmt.Errorf("failed to setup auto-update job: %w", err)
	}

	// Mark device as set up in KVS
	err = markDeviceSetUp(ctx, log, via, sd)
	if err != nil {
		log.Error(err, "Failed to mark device as set up", "device", deviceId)
		// Don't fail setup for this, just log the error
	}

	log.Info("Setup complete", "device", deviceId)
	return nil
}

// SetupDeviceFromDevicesDevice is a convenience wrapper that extracts the Shelly device
// from a devices.Device interface and calls SetupDevice.
func SetupDeviceFromDevicesDevice(ctx context.Context, log logr.Logger, device devices.Device, cfg Config) error {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return fmt.Errorf("expected *shellyapi.Device, got %T", device)
	}
	return SetupDevice(ctx, log, sd, "", cfg)
}

// setupDoneKey is the KVS key used to mark a device as set up
const setupDoneKey = "script/setup/done"

// IsDeviceSetUp checks if a device has already been set up by looking for the setup marker in KVS.
// This is cheaper than listing scripts.
func IsDeviceSetUp(ctx context.Context, log logr.Logger, sd *shellyapi.Device) bool {
	via := types.ChannelHttp

	_, err := kvs.GetValue(ctx, log, via, sd, setupDoneKey)
	return err == nil
}

// markDeviceSetUp stores a marker in KVS to indicate the device has been set up
func markDeviceSetUp(ctx context.Context, log logr.Logger, via types.Channel, sd *shellyapi.Device) error {
	_, err := kvs.SetKeyValue(ctx, log, via, sd, setupDoneKey, "1")
	return err
}

// nonAlphanumericRegex matches any non-alphanumeric character
var nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9]+`)

// deriveDeviceName tries to derive a device name from switch:0's name (output) or input:0's name.
// First checks output name, then input name if output is not suitable.
// Returns empty string if no suitable name can be derived.
// A name is suitable if:
// - It's not empty
// - It doesn't start with "Shelly" (case insensitive)
// Name transformation: lowercase, non-alphanumeric replaced by dash, multiple dashes collapsed, leading/trailing dashes removed.
func deriveDeviceName(ctx context.Context, log logr.Logger, via types.Channel, sd *shellyapi.Device) string {
	// First try switch:0 output name
	name := getOutputName(ctx, log, via, sd)
	if name != "" {
		log.V(1).Info("Derived device name from output", "device", sd.Id(), "derived_name", name)
		return name
	}

	// Fall back to input:0 name
	name = getInputName(ctx, log, via, sd)
	if name != "" {
		log.V(1).Info("Derived device name from input", "device", sd.Id(), "derived_name", name)
		return name
	}

	log.V(1).Info("No suitable name found from output or input", "device", sd.Id())
	return ""
}

// getOutputName gets and transforms the first non-empty switch output name.
// Iterates through switch:0, switch:1, etc. until it finds a valid name or gets an error.
func getOutputName(ctx context.Context, log logr.Logger, via types.Channel, sd *shellyapi.Device) string {
	// Try up to 4 outputs (covers most Shelly devices: 1, 2, or 4 outputs)
	for id := 0; id < 4; id++ {
		out, err := sd.CallE(ctx, via, sswitch.GetConfig.String(), map[string]int{"id": id})
		if err != nil {
			// No more outputs available
			if id == 0 {
				log.V(1).Info("Cannot get switch:0 config for name derivation", "device", sd.Id(), "error", err)
			}
			break
		}

		switchCfg, ok := out.(*sswitch.Config)
		if !ok || switchCfg == nil {
			continue
		}

		name := transformName(switchCfg.Name)
		if name != "" {
			log.V(1).Info("Found output name", "device", sd.Id(), "switch_id", id, "name", switchCfg.Name)
			return name
		}
	}
	return ""
}

// getInputName gets and transforms the input:0 name
func getInputName(ctx context.Context, log logr.Logger, via types.Channel, sd *shellyapi.Device) string {
	out, err := sd.CallE(ctx, via, input.GetConfig.String(), map[string]int{"id": 0})
	if err != nil {
		log.V(1).Info("Cannot get input:0 config for name derivation", "device", sd.Id(), "error", err)
		return ""
	}

	inputCfg, ok := out.(*input.Configuration)
	if !ok || inputCfg == nil {
		return ""
	}

	return transformName(inputCfg.Name)
}

// transformName transforms a raw name into a valid device name.
// Returns empty string if name is empty or starts with "Shelly" (case insensitive).
func transformName(rawName string) string {
	if rawName == "" {
		return ""
	}

	// Skip if name starts with "Shelly" (case insensitive)
	if strings.HasPrefix(strings.ToLower(rawName), "shelly") {
		return ""
	}

	// Transform: lowercase, replace non-alphanumeric with dash, collapse multiple dashes, trim dashes
	name := strings.ToLower(rawName)
	name = nonAlphanumericRegex.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")

	return name
}

// setupAutoUpdateJob creates or updates a scheduled job for Shelly.Update
// The job runs at a random time between 03:00 and 05:00 every day
func setupAutoUpdateJob(ctx context.Context, log logr.Logger, via types.Channel, sd *shellyapi.Device, deviceId string) error {
	// Get existing jobs
	out, err := schedule.ShowJobs(ctx, log, via, sd)
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	scheduled := out.(*schedule.Scheduled)

	// Generate random time between 03:00 and 05:00
	randomHour := 3 + rand.Intn(2) // 3 or 4
	randomMinute := rand.Intn(60)  // 0-59

	// Cron format: second minute hour day month weekday
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
		log.Info("Updated auto-update job", "device", deviceId, "job_id", *existingJobId, "hour", randomHour, "minute", randomMinute)
	} else {
		// Create new job
		result, err := sd.CallE(ctx, via, string(schedule.Create), jobSpec)
		if err != nil {
			return fmt.Errorf("failed to create job: %w", err)
		}
		jobId := result.(*schedule.JobId)
		log.Info("Created auto-update job", "device", deviceId, "job_id", jobId.Id, "hour", randomHour, "minute", randomMinute)
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
				log.Info("Firmware is up to date", "device", deviceId)
			} else {
				log.Info("All updates applied", "device", deviceId)
			}
			return nil
		}

		// Update available
		log.Info("Stable firmware available", "device", deviceId, "version", updateInfo.Stable.Version, "build", updateInfo.Stable.BuildId)
		log.Info("Applying update", "device", deviceId)

		// Apply the update
		err = shelly.DoUpdate(ctx, via, sd, "stable")
		if err != nil {
			return fmt.Errorf("failed to initiate update: %w", err)
		}

		// Wait for update to complete
		log.Info("Waiting for device to update and reboot (this may take 2-3 minutes)", "device", deviceId)
		time.Sleep(10 * time.Second) // Initial wait for update to start

		// Wait for device to go offline (update started)
		log.Info("Waiting for device to go offline for update", "device", deviceId)
		time.Sleep(30 * time.Second)

		// Wait for device to come back online after update
		log.Info("Waiting for device to come back online", "device", deviceId)
		maxRetries := 40 // 40 * 5 seconds = 200 seconds (3+ minutes)
		deviceOnline := false
		for i := 0; i < maxRetries; i++ {
			time.Sleep(5 * time.Second)
			status, err := system.GetStatus(ctx, via, sd)
			if err == nil && status != nil {
				// Device is back online
				deviceOnline = true
				log.Info("Update completed", "device", deviceId, "iteration", iteration)
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
		log.Info("Checking for additional updates", "device", deviceId)
	}

	// If we hit the max iterations, warn but don't fail
	log.Info("Reached maximum update iterations", "device", deviceId, "iterations", maxIterations)
	return nil
}
