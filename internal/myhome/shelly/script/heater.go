package script

import (
	"context"
	"fmt"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	pkgshelly "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// HeaterKVSKeys maps config field names to KVS keys
var HeaterKVSKeys = map[string]string{
	"normally_closed":            string(myhome.NormallyClosedKey),
	"room_id":                    string(myhome.RoomIdKey),
	"enable_logging":             "script/heater/enable-logging",
	"cheap_start_hour":           "script/heater/cheap-start-hour",
	"cheap_end_hour":             "script/heater/cheap-end-hour",
	"poll_interval_ms":           "script/heater/poll-interval-ms",
	"preheat_hours":              "script/heater/preheat-hours",
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
	myhome.RegisterMethodHandler(myhome.HeaterGetConfig, func(ctx context.Context, in any) (any, error) {
		return s.HandleGetConfig(ctx, in.(*myhome.HeaterGetConfigParams))
	})
	myhome.RegisterMethodHandler(myhome.HeaterSetConfig, func(ctx context.Context, in any) (any, error) {
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
		for _, sc := range []*pkgshelly.ScriptInfo{device.Config.Script1, device.Config.Script2, device.Config.Script3, device.Config.Script4} {
			if sc != nil && sc.Name == "heater.js" || sc.Name == "heater" {
				result.HasScript = true
				break
			}
		}
	}

	if !result.HasScript {
		return result, nil
	}

	// Use ChannelDefault to let the device dynamically select the best available channel
	via := types.ChannelDefault

	// Batch fetch prefixed keys with KVS.GetMany (reduces 7 calls to 1)
	prefixedValues, err := kvs.GetManyValues(ctx, s.log, via, sd, "script/heater/*")
	if err != nil {
		s.log.V(1).Info("Failed to get prefixed KVS values", "error", err)
	}
	var items map[string]any
	if prefixedValues != nil {
		items = prefixedValues.Items
	}

	// Fetch unprefixed keys individually (only 2 calls)
	var roomID, normallyClosed *string
	if val, err := kvs.GetValue(ctx, s.log, via, sd, string(myhome.RoomIdKey)); err == nil && val != nil {
		roomID = &val.Value
	}
	if val, err := kvs.GetValue(ctx, s.log, via, sd, string(myhome.NormallyClosedKey)); err == nil && val != nil {
		normallyClosed = &val.Value
	}

	result.Config = parseHeaterConfig(items, roomID, normallyClosed)
	return result, nil
}

// parseHeaterConfig turns the raw KVS values fetched for a device's
// heater.js script into a typed HeaterConfig. items is the prefixed
// "script/heater/*" batch (nil-safe); roomID/normallyClosed are the two
// unprefixed keys fetched individually, nil when unavailable.
func parseHeaterConfig(items map[string]any, roomID *string, normallyClosed *string) *myhome.HeaterConfig {
	config := &myhome.HeaterConfig{}

	getValue := func(key string) string {
		if items == nil {
			return ""
		}
		if v, ok := items[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

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

	if roomID != nil {
		config.RoomID = *roomID
	}
	if normallyClosed != nil {
		config.NormallyClosed = *normallyClosed == "true"
	}

	return config
}

// heaterKVSWrite pairs a KVS key with the value to write for one heater
// config field; Field is the human-readable name used in error messages.
type heaterKVSWrite struct {
	Field string
	Key   string
	Value string
}

// buildHeaterKVSWrites decides which KVS writes HandleSetConfig must issue
// for the fields the caller explicitly set, in the same order they were
// written before this was extracted (enable_logging, room_id, cheap hours,
// poll interval, preheat hours, normally_closed, temperature topics).
func buildHeaterKVSWrites(params *myhome.HeaterSetConfigParams) []heaterKVSWrite {
	var writes []heaterKVSWrite

	if params.EnableLogging != nil {
		val := "false"
		if *params.EnableLogging {
			val = "true"
		}
		writes = append(writes, heaterKVSWrite{"enable_logging", HeaterKVSKeys["enable_logging"], val})
	}
	if params.RoomID != nil {
		writes = append(writes, heaterKVSWrite{"room_id", HeaterKVSKeys["room_id"], *params.RoomID})
	}
	if params.CheapStartHour != nil {
		writes = append(writes, heaterKVSWrite{"cheap_start_hour", HeaterKVSKeys["cheap_start_hour"], fmt.Sprintf("%d", *params.CheapStartHour)})
	}
	if params.CheapEndHour != nil {
		writes = append(writes, heaterKVSWrite{"cheap_end_hour", HeaterKVSKeys["cheap_end_hour"], fmt.Sprintf("%d", *params.CheapEndHour)})
	}
	if params.PollIntervalMs != nil {
		writes = append(writes, heaterKVSWrite{"poll_interval_ms", HeaterKVSKeys["poll_interval_ms"], fmt.Sprintf("%d", *params.PollIntervalMs)})
	}
	if params.PreheatHours != nil {
		writes = append(writes, heaterKVSWrite{"preheat_hours", HeaterKVSKeys["preheat_hours"], fmt.Sprintf("%d", *params.PreheatHours)})
	}
	if params.NormallyClosed != nil {
		val := "false"
		if *params.NormallyClosed {
			val = "true"
		}
		writes = append(writes, heaterKVSWrite{"normally_closed", HeaterKVSKeys["normally_closed"], val})
	}
	if params.InternalTemperatureTopic != nil {
		writes = append(writes, heaterKVSWrite{"internal_temperature_topic", HeaterKVSKeys["internal_temperature_topic"], *params.InternalTemperatureTopic})
	}
	if params.ExternalTemperatureTopic != nil {
		writes = append(writes, heaterKVSWrite{"external_temperature_topic", HeaterKVSKeys["external_temperature_topic"], *params.ExternalTemperatureTopic})
	}

	return writes
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

	// Write each provided value, stopping at the first failure.
	for _, w := range buildHeaterKVSWrites(params) {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, w.Key, w.Value); err != nil {
			return &myhome.HeaterSetConfigResult{Success: false, Message: "failed to set " + w.Field + ": " + err.Error()}, nil
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
