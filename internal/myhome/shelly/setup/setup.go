package setup

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/go-logr/logr"

	mhscript "internal/myhome/shelly/script"
	"mynet"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/matter"
	"pkg/shelly/mqtt"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
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

// DefaultConfig returns a Config with default values
func DefaultConfig() Config {
	return Config{
		MqttBroker: "mqtt.local",
		MqttPort:   1883,
	}
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
	via := types.ChannelHttp

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

	if targetName != "" && config.Device.Name != targetName {
		configModified = true
		config.Device.Name = targetName
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
		log.Error(err, "Failed to check/apply updates", "device", deviceId)
		return fmt.Errorf("failed to check/apply updates: %w", err)
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

		_, err = mqtt.SetServer(ctx, via, sd, mqttServer)
		if err != nil {
			log.Error(err, "Failed to set MQTT broker", "device", deviceId)
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
