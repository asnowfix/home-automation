package script

import (
	"context"
	"fmt"
	"pkg/shelly"
	"pkg/shelly/kvs"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/sswitch"
	"pkg/shelly/types"
	"time"

	"github.com/go-logr/logr"
)

// PoolKVSKeys maps configuration fields to KVS keys
// Note: KVS keys must be < 42 characters (target: ≤32 chars)
// Prefix: script/pool-pump/ (18 chars) + key name
var PoolKVSKeys = map[string]string{
	"device_role":                 "script/pool-pump/device-role",    // 30 chars ✓
	"controller_device_id":        "script/pool-pump/controller-id",  // 32 chars ✓
	"bootstrap_device_id":         "script/pool-pump/bootstrap-id",   // 31 chars ✓
	"mqtt_topic_prefix":           "script/pool-pump/mqtt-topic",     // 29 chars ✓
	"enable_logging":              "script/pool-pump/logging",        // 26 chars ✓
	"eco_speed":                   "script/pool-pump/eco-speed",      // 28 chars ✓
	"mid_speed":                   "script/pool-pump/mid-speed",      // 28 chars ✓
	"high_speed":                  "script/pool-pump/high-speed",     // 29 chars ✓
	"bootstrap_duration_ms":       "script/pool-pump/boot-duration",  // 32 chars ✓
	"night_run_duration_ms":       "script/pool-pump/night-duration", // 32 chars ✓
	"bootstrap_to_speed_delay_ms": "script/pool-pump/boot-delay",     // 28 chars ✓
	"bootstrap_hours_threshold":   "script/pool-pump/boot-hours",     // 28 chars ✓
	"temperature_threshold":       "script/pool-pump/temp-threshold", // 32 chars ✓
}

// PoolService handles pool pump operations
type PoolService struct {
	log      logr.Logger
	provider DeviceProvider
}

// NewPoolService creates a new pool service
func NewPoolService(log logr.Logger, provider DeviceProvider) *PoolService {
	return &PoolService{
		log:      log.WithName("PoolService"),
		provider: provider,
	}
}

// SetupOptions contains configuration for pool setup
type SetupOptions struct {
	ControllerDeviceID      string
	BootstrapDeviceID       string
	BootstrapHoursThreshold float64
	BootstrapDurationMs     int
	NightRunDurationMs      int
	BootstrapToSpeedDelayMs int
	EcoSpeed                int
	MidSpeed                int
	HighSpeed               int
	TemperatureThreshold    float64
	ForceUpload             bool
	NoMinify                bool
}

// Setup configures the pool pump system on both controller and bootstrap devices
func (s *PoolService) Setup(ctx context.Context, opts SetupOptions) error {
	s.log.Info("Setting up pool pump system", "controller", opts.ControllerDeviceID, "bootstrap", opts.BootstrapDeviceID)

	// Get controller device and resolve identifier to ID
	controllerDev, err := s.provider.GetDeviceByAny(ctx, opts.ControllerDeviceID)
	if err != nil {
		return fmt.Errorf("controller device not found: %w", err)
	}

	controllerSD, err := s.provider.GetShellyDevice(ctx, controllerDev)
	if err != nil {
		return fmt.Errorf("failed to get controller shelly device: %w", err)
	}

	// Get bootstrap device and resolve identifier to ID
	bootstrapDev, err := s.provider.GetDeviceByAny(ctx, opts.BootstrapDeviceID)
	if err != nil {
		return fmt.Errorf("bootstrap device not found: %w", err)
	}

	bootstrapSD, err := s.provider.GetShellyDevice(ctx, bootstrapDev)
	if err != nil {
		return fmt.Errorf("failed to get bootstrap shelly device: %w", err)
	}

	// Resolve identifiers to actual device IDs for storage
	resolvedControllerID := controllerDev.Id()
	resolvedBootstrapID := bootstrapDev.Id()

	s.log.Info("Resolved device identifiers",
		"controller_input", opts.ControllerDeviceID, "controller_id", resolvedControllerID,
		"bootstrap_input", opts.BootstrapDeviceID, "bootstrap_id", resolvedBootstrapID)

	// Update opts with resolved IDs
	resolvedOpts := opts
	resolvedOpts.ControllerDeviceID = resolvedControllerID
	resolvedOpts.BootstrapDeviceID = resolvedBootstrapID

	// Use MQTT channel for KVS operations (HTTP may not be accessible/working)
	via := types.ChannelMqtt

	// Setup controller device with resolved IDs
	if err := s.setupDevice(ctx, via, controllerSD, "controller", resolvedOpts); err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}

	// Setup bootstrap device with resolved IDs
	if err := s.setupDevice(ctx, via, bootstrapSD, "bootstrap", resolvedOpts); err != nil {
		return fmt.Errorf("failed to setup bootstrap: %w", err)
	}

	s.log.Info("Pool pump setup complete", "controller_id", resolvedControllerID, "bootstrap_id", resolvedBootstrapID)
	return nil
}

func (s *PoolService) setupDevice(ctx context.Context, via types.Channel, sd *shelly.Device, role string, opts SetupOptions) error {
	s.log.Info("Setting up device", "device", sd.Name(), "role", role)

	// Build KVS configuration based on role
	var kvsConfig map[string]string

	if role == "bootstrap" {
		// Bootstrap device: minimal configuration only
		kvsConfig = map[string]string{
			PoolKVSKeys["device_role"]:          "bootstrap",
			PoolKVSKeys["controller_device_id"]: opts.ControllerDeviceID,
			PoolKVSKeys["mqtt_topic_prefix"]:    "pool/pump",
		}
	} else {
		// Controller device: full configuration
		kvsConfig = map[string]string{
			PoolKVSKeys["enable_logging"]:              "true",
			PoolKVSKeys["mqtt_topic_prefix"]:           "pool/pump",
			PoolKVSKeys["device_role"]:                 "controller",
			PoolKVSKeys["controller_device_id"]:        opts.ControllerDeviceID,
			PoolKVSKeys["bootstrap_device_id"]:         opts.BootstrapDeviceID,
			PoolKVSKeys["eco_speed"]:                   fmt.Sprintf("%d", opts.EcoSpeed),
			PoolKVSKeys["mid_speed"]:                   fmt.Sprintf("%d", opts.MidSpeed),
			PoolKVSKeys["high_speed"]:                  fmt.Sprintf("%d", opts.HighSpeed),
			PoolKVSKeys["bootstrap_duration_ms"]:       fmt.Sprintf("%d", opts.BootstrapDurationMs),
			PoolKVSKeys["night_run_duration_ms"]:       fmt.Sprintf("%d", opts.NightRunDurationMs),
			PoolKVSKeys["bootstrap_to_speed_delay_ms"]: fmt.Sprintf("%d", opts.BootstrapToSpeedDelayMs),
			PoolKVSKeys["bootstrap_hours_threshold"]:   fmt.Sprintf("%.1f", opts.BootstrapHoursThreshold),
			PoolKVSKeys["temperature_threshold"]:       fmt.Sprintf("%.1f", opts.TemperatureThreshold),
		}
	}

	// Set KVS configuration values
	fmt.Printf("  → Configuring %s (%d settings)...\n", sd.Name(), len(kvsConfig))
	for key, value := range kvsConfig {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, key, value); err != nil {
			return fmt.Errorf("failed to set KVS key %s: %w", key, err)
		}
		s.log.V(1).Info("Set KVS value", "key", key, "value", value)
		// Small delay to avoid overwhelming the device
		time.Sleep(100 * time.Millisecond)
	}

	// Upload and start script
	scriptName := "pool-pump.js"
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", scriptName, err)
	}

	fmt.Printf("  → Uploading script to %s...\n", sd.Name())
	uploadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	minify := !opts.NoMinify
	_, err = UploadWithVersion(uploadCtx, s.log, via, sd, scriptName, buf, minify, opts.ForceUpload)
	if err != nil {
		return fmt.Errorf("failed to upload/start %s: %w", scriptName, err)
	}
	fmt.Printf("  → Script uploaded and started on %s\n", sd.Name())

	s.log.Info("Device setup complete", "device", sd.Name(), "role", role)
	return nil
}

// Speed represents pump speed
type Speed string

const (
	SpeedEco  Speed = "eco"
	SpeedMid  Speed = "mid"
	SpeedHigh Speed = "high"
)

// speedMappings holds the switch ID for each speed
type speedMappings struct {
	Eco  int
	Mid  int
	High int
}

// getSpeedMappings retrieves speed-to-switch mappings from controller's KVS
func (s *PoolService) getSpeedMappings(ctx context.Context, sd *shelly.Device, via types.Channel) (*speedMappings, error) {
	mappings := &speedMappings{
		Eco:  0, // defaults
		Mid:  1,
		High: 2,
	}

	// Try to load from KVS
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["eco_speed"]); err == nil && val != nil {
		if _, err := fmt.Sscanf(val.Value, "%d", &mappings.Eco); err != nil {
			s.log.V(1).Info("Failed to parse eco_speed, using default", "error", err)
		}
	}
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["mid_speed"]); err == nil && val != nil {
		if _, err := fmt.Sscanf(val.Value, "%d", &mappings.Mid); err != nil {
			s.log.V(1).Info("Failed to parse mid_speed, using default", "error", err)
		}
	}
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["high_speed"]); err == nil && val != nil {
		if _, err := fmt.Sscanf(val.Value, "%d", &mappings.High); err != nil {
			s.log.V(1).Info("Failed to parse high_speed, using default", "error", err)
		}
	}

	return mappings, nil
}

// getBootstrapDeviceID retrieves the bootstrap device ID from controller's KVS
func (s *PoolService) getBootstrapDeviceID(ctx context.Context, controllerID string) (string, error) {
	// Get controller device
	device, err := s.provider.GetDeviceByAny(ctx, controllerID)
	if err != nil {
		return "", fmt.Errorf("controller device not found: %w", err)
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return "", fmt.Errorf("failed to get shelly device: %w", err)
	}

	via := types.ChannelDefault

	// Get bootstrap device ID from controller's KVS
	result, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["bootstrap_device_id"])
	if err != nil {
		return "", fmt.Errorf("failed to get bootstrap device ID from controller KVS: %w", err)
	}

	if result == nil || result.Value == "" {
		return "", fmt.Errorf("bootstrap device ID not configured in controller KVS")
	}

	return result.Value, nil
}

// Start starts the pool pump at the specified speed
func (s *PoolService) Start(ctx context.Context, controllerID string, speed Speed) error {
	s.log.Info("Starting pool pump", "controller", controllerID, "speed", speed)

	// Get controller device
	device, err := s.provider.GetDeviceByAny(ctx, controllerID)
	if err != nil {
		return fmt.Errorf("controller device not found: %w", err)
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return fmt.Errorf("failed to get shelly device: %w", err)
	}

	via := types.ChannelDefault

	// Get speed mappings from KVS
	mappings, err := s.getSpeedMappings(ctx, sd, via)
	if err != nil {
		return fmt.Errorf("failed to get speed mappings: %w", err)
	}

	// Map speed to switch ID
	var switchID int
	switch speed {
	case SpeedEco:
		switchID = mappings.Eco
	case SpeedMid:
		switchID = mappings.Mid
	case SpeedHigh:
		switchID = mappings.High
	default:
		return fmt.Errorf("invalid speed: %s", speed)
	}

	// Turn on the appropriate switch
	// The pool-pump.js script will handle bootstrap logic automatically based on temperature
	result, err := sswitch.Set(ctx, sd, via, switchID, true)
	if err != nil {
		return fmt.Errorf("failed to start pump: %w", err)
	}

	s.log.Info("Pump started", "speed", speed, "switch", switchID, "result", result)
	return nil
}

// Stop stops the pool pump on both devices
func (s *PoolService) Stop(ctx context.Context, controllerID string) error {
	s.log.Info("Stopping pool pump", "controller", controllerID)

	// Get bootstrap device ID from controller's KVS
	bootstrapID, err := s.getBootstrapDeviceID(ctx, controllerID)
	if err != nil {
		return fmt.Errorf("failed to get bootstrap device ID: %w", err)
	}

	s.log.V(1).Info("Retrieved bootstrap device ID from controller", "bootstrap", bootstrapID)

	// Get controller device
	controllerDev, err := s.provider.GetDeviceByAny(ctx, controllerID)
	if err != nil {
		return fmt.Errorf("controller device not found: %w", err)
	}

	controllerSD, err := s.provider.GetShellyDevice(ctx, controllerDev)
	if err != nil {
		return fmt.Errorf("failed to get controller shelly device: %w", err)
	}

	// Get bootstrap device
	bootstrapDev, err := s.provider.GetDeviceByAny(ctx, bootstrapID)
	if err != nil {
		return fmt.Errorf("bootstrap device not found: %w", err)
	}

	bootstrapSD, err := s.provider.GetShellyDevice(ctx, bootstrapDev)
	if err != nil {
		return fmt.Errorf("failed to get bootstrap shelly device: %w", err)
	}

	via := types.ChannelDefault

	// Stop all controller switches (0, 1, 2)
	for i := 0; i < 3; i++ {
		if _, err := sswitch.Set(ctx, controllerSD, via, i, false); err != nil {
			s.log.Error(err, "Failed to turn off controller switch", "switch", i)
		}
	}

	// Stop bootstrap switch (0)
	if _, err := sswitch.Set(ctx, bootstrapSD, via, 0, false); err != nil {
		s.log.Error(err, "Failed to turn off bootstrap switch")
	}

	s.log.Info("Pool pump stopped")
	return nil
}

// PoolStatus represents the status of the pool pump system
type PoolStatus struct {
	Controller  DeviceStatus `json:"controller" yaml:"controller"`
	Bootstrap   DeviceStatus `json:"bootstrap" yaml:"bootstrap"`
	Environment Environment  `json:"environment" yaml:"environment"`
}

// DeviceStatus represents the status of a single device
type DeviceStatus struct {
	DeviceID         string          `json:"device_id" yaml:"device_id"`
	DeviceName       string          `json:"device_name,omitempty" yaml:"device_name,omitempty"`
	DeviceType       string          `json:"device_type,omitempty" yaml:"device_type,omitempty"` // "pro3" or "pro1"
	Role             string          `json:"role" yaml:"role"`
	Online           bool            `json:"online" yaml:"online"`
	ScriptRunning    bool            `json:"script_running" yaml:"script_running"`
	ActiveOutput     int             `json:"active_output" yaml:"active_output"` // -1 = off, 0/1/2 = switch ID
	ActiveSpeed      string          `json:"active_speed,omitempty" yaml:"active_speed,omitempty"`
	SavedOutput      int             `json:"saved_output,omitempty" yaml:"saved_output,omitempty"` // Saved before water-supply protection
	Inputs           map[string]bool `json:"inputs" yaml:"inputs"`
	Switches         map[string]int  `json:"switches,omitempty" yaml:"switches,omitempty"` // Switch names to IDs
	MqttConnected    bool            `json:"mqtt_connected,omitempty" yaml:"mqtt_connected,omitempty"`
	LastRunTimestamp *int64          `json:"last_run_timestamp,omitempty" yaml:"last_run_timestamp,omitempty"` // Unix timestamp
	ScheduleMode     string          `json:"schedule_mode,omitempty" yaml:"schedule_mode,omitempty"`           // "summer" or "winter"
	Error            string          `json:"error,omitempty" yaml:"error,omitempty"`
}

// Environment represents environmental conditions and configuration
type Environment struct {
	// Bootstrap configuration
	BootstrapHoursThreshold float64 `json:"bootstrap_hours_threshold" yaml:"bootstrap_hours_threshold"`
	BootstrapDurationMs     int     `json:"bootstrap_duration_ms,omitempty" yaml:"bootstrap_duration_ms,omitempty"`
	BootstrapToSpeedDelayMs int     `json:"bootstrap_to_speed_delay_ms,omitempty" yaml:"bootstrap_to_speed_delay_ms,omitempty"`
	BootstrapRequired       bool    `json:"bootstrap_required" yaml:"bootstrap_required"`
	BootstrapInProgress     bool    `json:"bootstrap_in_progress" yaml:"bootstrap_in_progress"`

	// Schedule configuration
	TemperatureThreshold float64 `json:"temperature_threshold,omitempty" yaml:"temperature_threshold,omitempty"`
	NightRunDurationMs   int     `json:"night_run_duration_ms,omitempty" yaml:"night_run_duration_ms,omitempty"`

	// Speed mappings (controller only)
	EcoSpeed  *int `json:"eco_speed,omitempty" yaml:"eco_speed,omitempty"`
	MidSpeed  *int `json:"mid_speed,omitempty" yaml:"mid_speed,omitempty"`
	HighSpeed *int `json:"high_speed,omitempty" yaml:"high_speed,omitempty"`

	// MQTT configuration
	MqttTopicPrefix string `json:"mqtt_topic_prefix,omitempty" yaml:"mqtt_topic_prefix,omitempty"`

	// Weather forecast
	ForecastUrl string `json:"forecast_url,omitempty" yaml:"forecast_url,omitempty"`
}

// Status returns the current status of the pool pump system
func (s *PoolService) Status(ctx context.Context, controllerID string) (*PoolStatus, error) {
	s.log.Info("Getting pool pump status", "controller", controllerID)

	// Resolve controller identifier to device
	controllerDev, err := s.provider.GetDeviceByAny(ctx, controllerID)
	if err != nil {
		return nil, fmt.Errorf("controller device not found: %w", err)
	}
	resolvedControllerID := controllerDev.Id()

	// Get bootstrap device ID from controller's KVS
	bootstrapID, err := s.getBootstrapDeviceID(ctx, resolvedControllerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bootstrap device ID: %w", err)
	}

	s.log.V(1).Info("Retrieved bootstrap device ID from controller", "bootstrap", bootstrapID)

	status := &PoolStatus{
		Environment: Environment{
			BootstrapHoursThreshold: 6.0, // Default, will be updated from KVS
		},
	}

	// Get controller status
	controllerStatus, err := s.getDeviceStatus(ctx, resolvedControllerID, "controller")
	if err != nil {
		controllerStatus = DeviceStatus{
			DeviceID:   resolvedControllerID,
			DeviceName: controllerDev.Name(),
			Role:       "controller",
			Online:     false,
			Error:      err.Error(),
			Inputs:     make(map[string]bool),
		}
	}
	status.Controller = controllerStatus

	// Get bootstrap status
	bootstrapStatus, err := s.getDeviceStatus(ctx, bootstrapID, "bootstrap")
	if err != nil {
		// Best-effort: retrieve the bootstrap device name; fall back to its ID.
		bootstrapName := bootstrapID
		if bootstrapDev, lookupErr := s.provider.GetDeviceByAny(ctx, bootstrapID); lookupErr == nil {
			bootstrapName = bootstrapDev.Name()
		}
		bootstrapStatus = DeviceStatus{
			DeviceID:   bootstrapID,
			DeviceName: bootstrapName,
			Role:       "bootstrap",
			Online:     false,
			Error:      err.Error(),
			Inputs:     make(map[string]bool),
		}
	}
	status.Bootstrap = bootstrapStatus

	// Get environment info from controller KVS
	if controllerStatus.Online {
		if err := s.getEnvironmentStatus(ctx, resolvedControllerID, &status.Environment); err != nil {
			s.log.V(1).Info("Failed to get environment status", "error", err)
		}
	}

	return status, nil
}

func (s *PoolService) getDeviceStatus(ctx context.Context, deviceID, role string) (DeviceStatus, error) {
	// Get device to retrieve name
	device, err := s.provider.GetDeviceByAny(ctx, deviceID)
	if err != nil {
		return DeviceStatus{}, err
	}

	status := DeviceStatus{
		DeviceID:   deviceID,
		DeviceName: device.Name(),
		Role:       role,
		Inputs:     make(map[string]bool),
		Switches:   make(map[string]int),
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return status, fmt.Errorf("failed to get shelly device: %w", err)
	}

	via := types.ChannelDefault
	status.Online = true

	// Get script status
	scriptStatus, err := pkgscript.ScriptStatus(ctx, sd, via, "pool-pump.js")
	if err == nil && scriptStatus != nil {
		status.ScriptRunning = scriptStatus.Running
	}

	// Get switch status to determine active output
	status.ActiveOutput = -1
	status.ActiveSpeed = "off"

	numSwitches := 3
	if role == "bootstrap" {
		numSwitches = 1
	}

	// Get speed mappings for controller to show correct speed names
	var mappings *speedMappings
	if role == "controller" {
		if m, err := s.getSpeedMappings(ctx, sd, via); err == nil {
			mappings = m
		}
	}

	for i := 0; i < numSwitches; i++ {
		result, err := sswitch.GetStatus(ctx, sd, via, i)
		if err == nil && result != nil && result.Output {
			status.ActiveOutput = i
			if role == "controller" && mappings != nil {
				// Map switch ID back to speed name using actual configuration
				if i == mappings.Eco {
					status.ActiveSpeed = "eco"
				} else if i == mappings.Mid {
					status.ActiveSpeed = "mid"
				} else if i == mappings.High {
					status.ActiveSpeed = "high"
				} else {
					status.ActiveSpeed = fmt.Sprintf("switch-%d", i)
				}
			}
			break
		}
	}

	// Get input status
	inputNames := []string{"water-supply", "high-water"}
	if role == "controller" {
		inputNames = append(inputNames, "max-speed-active")
	}

	for i, name := range inputNames {
		params := map[string]interface{}{"id": i}
		result, err := sd.CallE(ctx, via, "Input.GetStatus", params)
		if err == nil && result != nil {
			if statusMap, ok := result.(map[string]interface{}); ok {
				if state, ok := statusMap["state"].(bool); ok {
					status.Inputs[name] = state
				}
			}
		}
	}

	// Get switch names from device config
	switchNames := []string{"pump-eco", "pump-mid", "pump-high"}
	if role == "bootstrap" {
		switchNames = []string{"pump-max"}
	}
	for i, name := range switchNames {
		if i < numSwitches {
			status.Switches[name] = i
		}
	}

	// Determine device type from number of switches
	if numSwitches >= 3 {
		status.DeviceType = "pro3"
	} else {
		status.DeviceType = "pro1"
	}

	// Get MQTT connection status
	params := map[string]interface{}{}
	if result, err := sd.CallE(ctx, via, "MQTT.GetStatus", params); err == nil && result != nil {
		if mqttStatus, ok := result.(map[string]interface{}); ok {
			if connected, ok := mqttStatus["connected"].(bool); ok {
				status.MqttConnected = connected
			}
		}
	}

	// Get state from KVS (controller only has these)
	if role == "controller" {
		// Get last run timestamp
		if val, err := kvs.GetValue(ctx, s.log, via, sd, "script/pool-pump/last-run-ts"); err == nil && val != nil && val.Value != "" {
			var ts int64
			if _, err := fmt.Sscanf(val.Value, "%d", &ts); err == nil {
				status.LastRunTimestamp = &ts
			}
		}

		// Get schedule mode
		if val, err := kvs.GetValue(ctx, s.log, via, sd, "script/pool-pump/schedule-mode"); err == nil && val != nil && val.Value != "" {
			status.ScheduleMode = val.Value
		}

		// Get saved output (for water-supply protection)
		if val, err := kvs.GetValue(ctx, s.log, via, sd, "script/pool-pump/active-output"); err == nil && val != nil && val.Value != "" {
			var savedOut int
			if _, err := fmt.Sscanf(val.Value, "%d", &savedOut); err == nil {
				status.SavedOutput = savedOut
			}
		}
	}

	return status, nil
}

func (s *PoolService) getEnvironmentStatus(ctx context.Context, controllerID string, env *Environment) error {
	device, err := s.provider.GetDeviceByAny(ctx, controllerID)
	if err != nil {
		return err
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return err
	}

	via := types.ChannelMqtt

	// Get bootstrap configuration from KVS
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["bootstrap_hours_threshold"]); err == nil && val != nil {
		var threshold float64
		if _, err := fmt.Sscanf(val.Value, "%f", &threshold); err == nil {
			env.BootstrapHoursThreshold = threshold
		}
	}

	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["bootstrap_duration_ms"]); err == nil && val != nil {
		var duration int
		if _, err := fmt.Sscanf(val.Value, "%d", &duration); err == nil {
			env.BootstrapDurationMs = duration
		}
	}

	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["bootstrap_to_speed_delay_ms"]); err == nil && val != nil {
		var delay int
		if _, err := fmt.Sscanf(val.Value, "%d", &delay); err == nil {
			env.BootstrapToSpeedDelayMs = delay
		}
	}

	// Get schedule configuration from KVS
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["temperature_threshold"]); err == nil && val != nil {
		var threshold float64
		if _, err := fmt.Sscanf(val.Value, "%f", &threshold); err == nil {
			env.TemperatureThreshold = threshold
		}
	}

	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["night_run_duration_ms"]); err == nil && val != nil {
		var duration int
		if _, err := fmt.Sscanf(val.Value, "%d", &duration); err == nil {
			env.NightRunDurationMs = duration
		}
	}

	// Get speed mappings from KVS
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["eco_speed"]); err == nil && val != nil {
		var speed int
		if _, err := fmt.Sscanf(val.Value, "%d", &speed); err == nil {
			env.EcoSpeed = &speed
		}
	}

	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["mid_speed"]); err == nil && val != nil {
		var speed int
		if _, err := fmt.Sscanf(val.Value, "%d", &speed); err == nil {
			env.MidSpeed = &speed
		}
	}

	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["high_speed"]); err == nil && val != nil {
		var speed int
		if _, err := fmt.Sscanf(val.Value, "%d", &speed); err == nil {
			env.HighSpeed = &speed
		}
	}

	// Get MQTT configuration from KVS
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["mqtt_topic_prefix"]); err == nil && val != nil {
		env.MqttTopicPrefix = val.Value
	}

	// Get forecast URL from Script.storage (not KVS)
	// Note: This is stored in Script.storage, not KVS, so we'd need to call Script.Eval to retrieve it
	// For now, we'll skip this as it requires executing code on the device

	// Note: The script tracks last run time and makes bootstrap decisions internally
	// We only display the configuration here, not the runtime state

	return nil
}
