package script

import (
	"context"
	"fmt"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

// HeaterKVSKeys maps config field names to KVS keys
var HeaterKVSKeys = map[string]string{
	"enable_logging":             "script/heater/enable-logging",
	"room_id":                    "script/heater/room-id",
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

	// Get KVS values via MQTT channel (rate limiting ensures proper spacing)
	config := &myhome.HeaterConfig{}
	via := types.ChannelHttp
	if !sd.IsHttpReady() {
		via = types.ChannelMqtt
	}

	// Fetch each KVS key
	for field, kvsKey := range HeaterKVSKeys {
		val, err := kvs.GetValue(ctx, s.log, via, sd, kvsKey)
		if err != nil {
			continue // Key doesn't exist
		}
		if val == nil || val.Value == "" {
			continue
		}

		// Parse value based on field type
		switch field {
		case "enable_logging":
			config.EnableLogging = val.Value == "true"
		case "room_id":
			config.RoomID = val.Value
		case "cheap_start_hour":
			if v, err := parseIntValue(val.Value); err == nil {
				config.CheapStartHour = v
			}
		case "cheap_end_hour":
			if v, err := parseIntValue(val.Value); err == nil {
				config.CheapEndHour = v
			}
		case "poll_interval_ms":
			if v, err := parseIntValue(val.Value); err == nil {
				config.PollIntervalMs = v
			}
		case "preheat_hours":
			if v, err := parseIntValue(val.Value); err == nil {
				config.PreheatHours = v
			}
		case "normally_closed":
			config.NormallyClosed = val.Value == "true"
		case "internal_temperature_topic":
			config.InternalTemperatureTopic = val.Value
		case "external_temperature_topic":
			config.ExternalTemperatureTopic = val.Value
		}
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

	// Prefer HTTP channel for config operations when available, otherwise MQTT has proper rate limiting
	via := types.ChannelHttp
	if !sd.IsHttpReady() {
		via = types.ChannelMqtt
	}

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

	return &myhome.HeaterSetConfigResult{Success: true}, nil
}

// parseIntValue parses a string value to int
func parseIntValue(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
