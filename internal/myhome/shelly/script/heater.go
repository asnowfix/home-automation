package script

import (
	"context"
	"fmt"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/kvs"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/types"
	"time"

	"github.com/go-logr/logr"
)

// HeaterKVSKeys maps config field names to KVS keys
var HeaterKVSKeys = map[string]string{
	"enable_logging":             "script/heater/enable-logging",
	"room_id":                    "room-id",
	"cheap_start_hour":           "script/heater/cheap-start-hour",
	"cheap_end_hour":             "script/heater/cheap-end-hour",
	"poll_interval_ms":           "script/heater/poll-interval-ms",
	"preheat_hours":              "script/heater/preheat-hours",
	"normally_closed":            "normally-closed",
	"internal_temperature_topic": "script/heater/internal-temperature-topic",
	"external_temperature_topic": "script/heater/external-temperature-topic",
}

// DeviceProvider interface for getting devices
type DeviceProvider interface {
	GetDeviceByAny(ctx context.Context, identifier string) (*myhome.Device, error)
	GetShellyDevice(ctx context.Context, device *myhome.Device) (*shelly.Device, error)
}

// HeaterService handles heater configuration RPC methods
type HeaterService struct {
	log      logr.Logger
	provider DeviceProvider
}

// NewHeaterService creates a new heater service
func NewHeaterService(log logr.Logger, provider DeviceProvider) *HeaterService {
	return &HeaterService{
		log:      log.WithName("HeaterService"),
		provider: provider,
	}
}

// RegisterHandlers registers the heater RPC handlers
func (s *HeaterService) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.HeaterGetConfig, func(in any) (any, error) {
		// FIXME: rather implement a per-request context
		ctx := logr.NewContext(context.Background(), s.log)
		return s.HandleGetConfig(ctx, in.(*myhome.HeaterGetConfigParams))
	})
	myhome.RegisterMethodHandler(myhome.HeaterSetConfig, func(in any) (any, error) {
		// FIXME: rather implement a per-request context
		ctx := logr.NewContext(context.Background(), s.log)
		return s.HandleSetConfig(ctx, in.(*myhome.HeaterSetConfigParams))
	})
}

// HandleGetConfig returns the heater configuration for a device
func (s *HeaterService) HandleGetConfig(ctx context.Context, params *myhome.HeaterGetConfigParams) (*myhome.HeaterGetConfigResult, error) {
	// Get device from DB
	device, err := s.provider.GetDeviceByAny(ctx, params.Identifier)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	// Get Shelly device for RPC calls
	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return nil, fmt.Errorf("failed to get shelly device: %w", err)
	}

	result := &myhome.HeaterGetConfigResult{
		DeviceID:   device.Id(),
		DeviceName: device.Name(),
		HasScript:  false,
	}

	// Check if device has heater.js script
	if device.Config != nil {
		for _, sc := range device.Config.Scripts {
			if sc.Name == "heater.js" || sc.Name == "heater" {
				result.HasScript = true
				break
			}
		}
	}

	if !result.HasScript {
		return result, nil
	}

	// Use ChannelDefault to let the device dynamically select the best available channel
	config := &myhome.HeaterConfig{}
	via := types.ChannelDefault

	// Batch fetch prefixed keys with KVS.GetMany (reduces 7 calls to 1)
	prefixedValues, err := kvs.GetManyValues(ctx, s.log, via, sd, "script/heater/*")
	if err != nil {
		s.log.V(1).Info("Failed to get prefixed KVS values", "error", err)
	}

	// Helper to get value from GetMany response
	getValue := func(key string) string {
		if prefixedValues == nil || prefixedValues.Items == nil {
			return ""
		}
		if v, ok := prefixedValues.Items[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	// Parse prefixed values
	if v := getValue("script/heater/enable-logging"); v != "" {
		config.EnableLogging = v == "true"
	}
	if v := getValue("script/heater/cheap-start-hour"); v != "" {
		if i, err := parseIntValue(v); err == nil {
			config.CheapStartHour = i
		}
	}
	if v := getValue("script/heater/cheap-end-hour"); v != "" {
		if i, err := parseIntValue(v); err == nil {
			config.CheapEndHour = i
		}
	}
	if v := getValue("script/heater/poll-interval-ms"); v != "" {
		if i, err := parseIntValue(v); err == nil {
			config.PollIntervalMs = i
		}
	}
	if v := getValue("script/heater/preheat-hours"); v != "" {
		if i, err := parseIntValue(v); err == nil {
			config.PreheatHours = i
		}
	}
	if v := getValue("script/heater/internal-temperature-topic"); v != "" {
		config.InternalTemperatureTopic = v
	}
	if v := getValue("script/heater/external-temperature-topic"); v != "" {
		config.ExternalTemperatureTopic = v
	}

	// Fetch unprefixed keys individually (only 2 calls)
	if val, err := kvs.GetValue(ctx, s.log, via, sd, "room-id"); err == nil && val != nil {
		config.RoomID = val.Value
	}
	if val, err := kvs.GetValue(ctx, s.log, via, sd, "normally-closed"); err == nil && val != nil {
		config.NormallyClosed = val.Value == "true"
	}

	result.Config = config
	return result, nil
}

// HandleSetConfig sets the heater configuration for a device
func (s *HeaterService) HandleSetConfig(ctx context.Context, params *myhome.HeaterSetConfigParams) (*myhome.HeaterSetConfigResult, error) {
	// Get device from DB
	device, err := s.provider.GetDeviceByAny(ctx, params.Identifier)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	// Get Shelly device for RPC calls
	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return nil, fmt.Errorf("failed to get shelly device: %w", err)
	}

	// Use ChannelDefault to let the device dynamically select the best available channel
	// This allows automatic fallback to MQTT if HTTP fails mid-operation
	via := types.ChannelDefault

	// Set each provided value
	if params.EnableLogging != nil {
		val := "false"
		if *params.EnableLogging {
			val = "true"
		}
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["enable_logging"], val); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set enable_logging: " + err.Error()}, nil
		}
	}

	if params.RoomID != nil {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["room_id"], *params.RoomID); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set room_id: " + err.Error()}, nil
		}
	}

	if params.CheapStartHour != nil {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["cheap_start_hour"], fmt.Sprintf("%d", *params.CheapStartHour)); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set cheap_start_hour: " + err.Error()}, nil
		}
	}

	if params.CheapEndHour != nil {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["cheap_end_hour"], fmt.Sprintf("%d", *params.CheapEndHour)); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set cheap_end_hour: " + err.Error()}, nil
		}
	}

	if params.PollIntervalMs != nil {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["poll_interval_ms"], fmt.Sprintf("%d", *params.PollIntervalMs)); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set poll_interval_ms: " + err.Error()}, nil
		}
	}

	if params.PreheatHours != nil {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["preheat_hours"], fmt.Sprintf("%d", *params.PreheatHours)); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set preheat_hours: " + err.Error()}, nil
		}
	}

	if params.NormallyClosed != nil {
		val := "false"
		if *params.NormallyClosed {
			val = "true"
		}
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["normally_closed"], val); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set normally_closed: " + err.Error()}, nil
		}
	}

	if params.InternalTemperatureTopic != nil {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["internal_temperature_topic"], *params.InternalTemperatureTopic); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set internal_temperature_topic: " + err.Error()}, nil
		}
	}

	if params.ExternalTemperatureTopic != nil {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, HeaterKVSKeys["external_temperature_topic"], *params.ExternalTemperatureTopic); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set external_temperature_topic: " + err.Error()}, nil
		}
	}

	// Upload and start heater.js script
	scriptName := "heater.js"
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to read heater.js: " + err.Error()}, nil
	}

	// Use a longer timeout for script upload
	uploadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	_, err = UploadWithVersion(uploadCtx, s.log, via, sd, scriptName, buf, true, false)
	if err != nil {
		return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to upload/start heater.js: " + err.Error()}, nil
	}

	s.log.Info("Heater script uploaded and started", "device", device.Name())
	return &myhome.HeaterSetConfigResult{Success: true}, nil
}

// parseIntValue parses a string value to int
func parseIntValue(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
