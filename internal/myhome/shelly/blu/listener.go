package blu

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/go-logr/logr"
)

// BLUEventData represents the data from a Shelly BLU device event
// Supports all BTHome v2 object IDs as defined in https://bthome.io/format/
type BLUEventData struct {
	Encryption    bool `json:"encryption"`
	BTHomeVersion int  `json:"BTHome_version"`
	PID           int  `json:"pid"`

	// Power & Energy (0x01, 0x0a, 0x0b, 0x0c, 0x43)
	Battery *int     `json:"battery,omitempty"` // 0x01 - Battery level in %
	Energy  *float64 `json:"energy,omitempty"`  // 0x0a - Energy in kWh
	Power   *float64 `json:"power,omitempty"`   // 0x0b - Power in W
	Voltage *float64 `json:"voltage,omitempty"` // 0x0c - Voltage in V
	Current *float64 `json:"current,omitempty"` // 0x43 - Current in A

	// Environmental Sensors (0x02, 0x03, 0x04, 0x05, 0x06, 0x08, 0x45)
	Temperature *float64 `json:"temperature,omitempty"` // 0x02/0x45 - Temperature in °C
	Humidity    *float64 `json:"humidity,omitempty"`    // 0x03/0x2e - Humidity in %
	Pressure    *float64 `json:"pressure,omitempty"`    // 0x04 - Pressure in hPa
	Illuminance *float64 `json:"illuminance,omitempty"` // 0x05 - Illuminance in lux
	Mass        *float64 `json:"mass,omitempty"`        // 0x06 - Mass in kg
	DewPoint    *float64 `json:"dew_point,omitempty"`   // 0x08 - Dew point in °C

	// Motion & Position (0x21, 0x2d, 0x3a, 0x3f)
	Motion   *int     `json:"motion,omitempty"`   // 0x21 - Motion (0=clear, 1=detected)
	Window   *int     `json:"window,omitempty"`   // 0x2d - Window (0=closed, 1=open)
	Button   *int     `json:"button,omitempty"`   // 0x3a - Button press count
	Rotation *float64 `json:"rotation,omitempty"` // 0x3f - Rotation in degrees

	// Distance (0x40, 0x41)
	DistanceMM *int     `json:"distance_mm,omitempty"` // 0x40 - Distance in mm
	DistanceM  *float64 `json:"distance_m,omitempty"`  // 0x41 - Distance in m

	// Timestamp (0x50)
	Timestamp *int `json:"timestamp,omitempty"` // 0x50 - Unix timestamp in seconds

	// Acceleration (0x51)
	Acceleration *float64 `json:"acceleration,omitempty"` // 0x51 - Acceleration in m/s²

	// Variable-length data (0x53, 0x54)
	Text interface{} `json:"text,omitempty"` // 0x53 - Text data (string or []string)
	Raw  interface{} `json:"raw,omitempty"`  // 0x54 - Raw data as hex (string or []string)

	// Device metadata
	RSSI    int    `json:"rssi"`
	Address string `json:"address"`

	// Raw BTHome frame data
	BTHome *BTHomeFrame `json:"bthome,omitempty"`
}

// BTHomeFrame contains raw BTHome BLE advertisement data
type BTHomeFrame struct {
	ServiceData      map[string]string `json:"service_data,omitempty"`
	ManufacturerData map[string]string `json:"manufacturer_data,omitempty"`
	LocalName        string            `json:"local_name,omitempty"`
}

// DeviceRegistry interface for registering discovered BLU devices
type DeviceRegistry interface {
	SetDevice(ctx context.Context, device *myhome.Device, overwrite bool) (bool, error)
	GetDeviceById(ctx context.Context, id string) (*myhome.Device, error)
	UpdateSensorValue(ctx context.Context, deviceID string, sensor string, value string) error
	RenameDevice(ctx context.Context, oldID, newID string) error
}

// SSEBroadcaster interface for broadcasting sensor updates to UI
type SSEBroadcaster interface {
	BroadcastSensorUpdate(deviceID string, sensor string, value string)
}

// StartBLUListener starts listening to BLU device MQTT events and registers them
func StartBLUListener(ctx context.Context, mc mqtt.Client, registry DeviceRegistry, sseBroadcaster SSEBroadcaster) error {
	return StartBLUListenerWithEvents(ctx, mc, registry, sseBroadcaster, nil, nil)
}

// deviceIDFromCapabilities infers the Shelly BLU device-type prefix from the
// sensor fields present in a BLU event, then appends the normalised MAC suffix.
// Precedence follows field exclusivity: window sensors never carry motion data,
// H&T sensors never carry motion/window data, etc.
func deviceIDFromCapabilities(mac string, data BLUEventData) string {
	suffix := strings.ToLower(strings.ReplaceAll(mac, ":", ""))
	var prefix string
	switch {
	case data.Window != nil:
		prefix = "shellybludoorwindow2"
	case data.Temperature != nil || data.Humidity != nil:
		prefix = "shellybluht3"
	case data.Motion != nil:
		prefix = "shellyblumotion1"
	case data.Button != nil:
		prefix = "shellyblubutton1"
	default:
		prefix = "shellyblu"
	}
	return prefix + "-" + suffix
}

// StartBLUListenerWithEvents is like StartBLUListener but also records events and sensor observations.
// eventSvc and tracker may be nil, in which case event recording is skipped.
func StartBLUListenerWithEvents(ctx context.Context, mc mqtt.Client, registry DeviceRegistry, sseBroadcaster SSEBroadcaster, eventSvc *events.Service, tracker *events.SensorDailyTracker) error {
	log := logr.FromContextOrDiscard(ctx).WithName("BLUListener")

	log.Info("Starting BLU listener", "mqtt_client", fmt.Sprintf("%T", mc), "registry", fmt.Sprintf("%T", registry))

	// Subscribe to BLU events topic
	topic := "shelly-blu/events/+"
	log.Info("Subscribing to BLU events", "topic", topic)
	err := mc.SubscribeWithHandler(ctx, topic, 16, "shelly/blu", func(topic string, payload []byte, subscriber string) error {
		log.Info("event received", "topic", topic, "payload", string(payload))

		// Handle device registration; returns the resolved device ID.
		deviceID, sensors, err := handleBLUEvent(ctx, log, topic, payload, registry)

		// Update cache and broadcast sensor updates via SSE
		if sensors != nil {
			for sensor, value := range *sensors {
				if cacheErr := registry.UpdateSensorValue(ctx, deviceID, sensor, value); cacheErr != nil {
					log.V(1).Info("Failed to update sensor in cache", "error", cacheErr, "device_id", deviceID)
				}
				if sseBroadcaster != nil {
					log.V(1).Info("Broadcasting BLU sensor update via SSE", "device_id", deviceID, "sensor", sensor, "value", value)
					sseBroadcaster.BroadcastSensorUpdate(deviceID, sensor, value)
				}
			}
		} else {
			log.V(1).Info("No sensors in event", "topic", topic, "device_id", deviceID)
		}

		// Feed event log and sensor tracker from the raw payload (nil-safe)
		handleBLUEventBridge(ctx, log, payload, eventSvc, tracker)

		log.V(1).Info("event processing completed", "device_id", deviceID)

		return err
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to BLU events: %w", err)
	}

	log.Info("started", "topic", topic)
	return nil
}

// handleBLUEventBridge maps BTHome object IDs to event log entries and tracker observations.
func handleBLUEventBridge(ctx context.Context, log logr.Logger, payload []byte, eventSvc *events.Service, tracker *events.SensorDailyTracker) {
	var data BLUEventData
	if err := json.Unmarshal(payload, &data); err != nil {
		return
	}
	if data.Address == "" {
		return
	}

	// device_id = BLU sensor MAC address
	deviceID := data.Address

	// Motion (0x21)
	if data.Motion != nil {
		if eventSvc != nil {
			eventName := "motion.cleared"
			if *data.Motion == 1 {
				eventName = "motion.detected"
			}
			if err := eventSvc.Record(ctx, events.Event{
				DeviceID:  deviceID,
				Component: "motion:0",
				Event:     eventName,
				Severity:  "info",
			}); err != nil {
				log.Error(err, "Failed to record motion event", "device_id", deviceID)
			}
		}
	}

	// Window (0x2D)
	if data.Window != nil {
		if eventSvc != nil {
			eventName := "window.closed"
			if *data.Window == 1 {
				eventName = "window.opened"
			}
			if err := eventSvc.Record(ctx, events.Event{
				DeviceID:  deviceID,
				Component: "window:0",
				Event:     eventName,
				Severity:  "info",
			}); err != nil {
				log.Error(err, "Failed to record window event", "device_id", deviceID)
			}
		}
	}

	// Button (0x3A)
	if data.Button != nil {
		if eventSvc != nil {
			if err := eventSvc.Record(ctx, events.Event{
				DeviceID:  deviceID,
				Component: "button:0",
				Event:     "button.push",
				Severity:  "info",
			}); err != nil {
				log.Error(err, "Failed to record button event", "device_id", deviceID)
			}
		}
	}

	// Battery low
	if data.Battery != nil && *data.Battery < 20 {
		if eventSvc != nil {
			if err := eventSvc.Record(ctx, events.Event{
				DeviceID:  deviceID,
				Component: "battery",
				Event:     "battery.low",
				Severity:  "warn",
			}); err != nil {
				log.Error(err, "Failed to record battery.low event", "device_id", deviceID)
			}
		}
	}

	// Temperature (0x02) → tracker only
	if data.Temperature != nil && tracker != nil {
		if err := tracker.Observe(ctx, events.Metric{DeviceID: deviceID, Component: "temperature:0", Metric: "tC"}, *data.Temperature); err != nil {
			log.Error(err, "Failed to observe BLU temperature", "device_id", deviceID)
		}
	}

	// Humidity (0x03) → tracker only
	if data.Humidity != nil && tracker != nil {
		if err := tracker.Observe(ctx, events.Metric{DeviceID: deviceID, Component: "humidity:0", Metric: "rh"}, *data.Humidity); err != nil {
			log.Error(err, "Failed to observe BLU humidity", "device_id", deviceID)
		}
	}
}

// handleBLUEvent returns (deviceID, sensors, error).
func handleBLUEvent(ctx context.Context, log logr.Logger, topic string, payload []byte, registry DeviceRegistry) (string, *map[string]string, error) {
	log.V(1).Info("Handling BLU event", "topic", topic, "payload", string(payload))

	// Parse the event data
	var eventData BLUEventData
	if err := json.Unmarshal(payload, &eventData); err != nil {
		log.V(1).Info("Failed to parse event", "topic", topic, "error", err)
		return "", nil, err
	}

	// Validate MAC address
	if eventData.Address == "" {
		err := fmt.Errorf("event missing MAC address")
		log.Error(err, "Event missing MAC address", "event", eventData)
		return "", nil, err
	}

	deviceID := deviceIDFromCapabilities(eventData.Address, eventData)
	// Fallback: look up by old generic ID for devices stored before type inference.
	legacyID := "shellyblu-" + strings.ToLower(strings.ReplaceAll(eventData.Address, ":", ""))

	// Determine sensor capabilities from the event data
	capabilities := []string{}
	sensors := map[string]string{}

	// Power & Energy
	if eventData.Battery != nil {
		capabilities = append(capabilities, "battery")
		sensors["battery"] = fmt.Sprintf("%d", *eventData.Battery)
	}
	if eventData.Energy != nil {
		capabilities = append(capabilities, "energy")
		sensors["energy"] = fmt.Sprintf("%.1f", *eventData.Energy)
	}
	if eventData.Power != nil {
		capabilities = append(capabilities, "power")
		sensors["power"] = fmt.Sprintf("%.1f", *eventData.Power)
	}
	if eventData.Voltage != nil {
		capabilities = append(capabilities, "voltage")
		sensors["voltage"] = fmt.Sprintf("%.1f", *eventData.Voltage)
	}
	if eventData.Current != nil {
		capabilities = append(capabilities, "current")
		sensors["current"] = fmt.Sprintf("%.1f", *eventData.Current)
	}

	// Environmental Sensors
	if eventData.Temperature != nil {
		capabilities = append(capabilities, "temperature")
		sensors["temperature"] = fmt.Sprintf("%.1f", *eventData.Temperature)
	}
	if eventData.Humidity != nil {
		capabilities = append(capabilities, "humidity")
		sensors["humidity"] = fmt.Sprintf("%.1f", *eventData.Humidity)
	}
	if eventData.Pressure != nil {
		capabilities = append(capabilities, "pressure")
		sensors["pressure"] = fmt.Sprintf("%.1f", *eventData.Pressure)
	}
	if eventData.Illuminance != nil {
		capabilities = append(capabilities, "illuminance")
		sensors["illuminance"] = fmt.Sprintf("%.1f", *eventData.Illuminance)
	}
	if eventData.Mass != nil {
		capabilities = append(capabilities, "mass")
	}
	if eventData.DewPoint != nil {
		capabilities = append(capabilities, "dew_point")
	}

	// Motion & Position
	if eventData.Motion != nil {
		capabilities = append(capabilities, "motion")
		sensors["motion"] = fmt.Sprintf("%d", *eventData.Motion)
	}
	if eventData.Window != nil {
		capabilities = append(capabilities, "window")
		sensors["window"] = fmt.Sprintf("%d", *eventData.Window)
	}
	if eventData.Button != nil {
		capabilities = append(capabilities, "button")
		sensors["button"] = fmt.Sprintf("%d", *eventData.Button)
	}
	if eventData.Rotation != nil {
		capabilities = append(capabilities, "rotation")
		sensors["rotation"] = fmt.Sprintf("%.1f", *eventData.Rotation)
	}

	// Distance
	if eventData.DistanceMM != nil {
		capabilities = append(capabilities, "distance_mm")
		sensors["distance_mm"] = fmt.Sprintf("%d", *eventData.DistanceMM)
	}
	if eventData.DistanceM != nil {
		capabilities = append(capabilities, "distance_m")
		sensors["distance_m"] = fmt.Sprintf("%.1f", *eventData.DistanceM)
	}

	// Timestamp
	if eventData.Timestamp != nil {
		capabilities = append(capabilities, "timestamp")
	}

	// Acceleration
	if eventData.Acceleration != nil {
		capabilities = append(capabilities, "acceleration")
		sensors["acceleration"] = fmt.Sprintf("%.1f", *eventData.Acceleration)
	}

	// Variable-length data
	if eventData.Text != nil {
		capabilities = append(capabilities, "text")
		if str, ok := eventData.Text.(string); ok {
			sensors["text"] = str
		}
	}
	if eventData.Raw != nil {
		capabilities = append(capabilities, "raw")
		if str, ok := eventData.Raw.(string); ok {
			sensors["raw"] = str
		}
	}

	// Extract model from local_name (lowercased)
	model := "shellyblu"
	if eventData.BTHome != nil && eventData.BTHome.LocalName != "" {
		model = strings.ToLower(eventData.BTHome.LocalName)
	}

	// Build new BTHome info with capabilities
	var newBTHome *shelly.BTHomeInfo
	if len(capabilities) > 0 || eventData.BTHome != nil {
		newBTHome = &shelly.BTHomeInfo{
			Version:      eventData.BTHomeVersion,
			Encryption:   eventData.Encryption,
			Capabilities: capabilities,
		}
		if eventData.BTHome != nil {
			newBTHome.ServiceData = eventData.BTHome.ServiceData
			newBTHome.ManufacturerData = eventData.BTHome.ManufacturerData
		}
	}

	// Parse MAC address
	macAddr, parseErr := net.ParseMAC(eventData.Address)
	if parseErr != nil {
		log.Error(parseErr, "Failed to parse MAC address", "device_id", deviceID, "mac", eventData.Address)
		return deviceID, nil, parseErr
	}

	// Try to get existing device from DB; fall back to legacy generic ID.
	existingDevice, err := registry.GetDeviceById(ctx, deviceID)
	if err != nil && legacyID != deviceID {
		existingDevice, err = registry.GetDeviceById(ctx, legacyID)
		if err == nil && existingDevice != nil {
			log.Info("Upgrading BLU device ID", "old_id", legacyID, "new_id", deviceID)
			if renameErr := registry.RenameDevice(ctx, legacyID, deviceID); renameErr != nil {
				log.Error(renameErr, "Failed to rename BLU device", "old_id", legacyID, "new_id", deviceID)
			} else {
				existingDevice.Id_ = deviceID
			}
		}
	}
	if err == nil && existingDevice != nil {
		// Device exists - check if anything changed
		changed := false

		// Check if BTHome info changed
		if existingDevice.Info == nil {
			// No info at all - need to update
			existingDevice.Info = &shelly.DeviceInfo{
				Product: shelly.Product{
					Model:      model,
					MacAddress: eventData.Address,
					Generation: 0,
				},
				Id: deviceID,
			}
			changed = true
		}

		// Check if BTHome capabilities changed
		if !btHomeInfoEqual(existingDevice.Info.BTHome, newBTHome) {
			existingDevice.Info.BTHome = newBTHome
			changed = true
		}

		// Update model if it changed (but not if it's just the default)
		if model != "shellyblu" && existingDevice.Info.Model != model {
			existingDevice.Info.Model = model
			changed = true
		}

		// Only save if something changed
		if changed {
			modified, err := registry.SetDevice(ctx, existingDevice, true)
			if err != nil {
				log.Error(err, "Failed to update BLU device", "device_id", deviceID)
				return deviceID, nil, err
			}
			if modified {
				log.V(1).Info("Updated BLU device", "device_id", deviceID, "capabilities", capabilities)
			} else {
				log.V(2).Info("BLU device unchanged in database", "device_id", deviceID)
			}
		}
		return deviceID, &sensors, nil
	}

	// Device doesn't exist - create new entry
	deviceInfo := &shelly.DeviceInfo{
		Product: shelly.Product{
			Model:      model,
			MacAddress: eventData.Address,
			Generation: 0, // BLU devices don't have a generation
		},
		Id: deviceID,
	}
	deviceInfo.BTHome = newBTHome

	device := myhome.NewDevice(log, myhome.SHELLY, deviceID)
	device = device.WithMAC(macAddr)
	device = device.WithName(deviceID) // Use device ID as default name for new devices
	device.Info = deviceInfo

	modified, err := registry.SetDevice(ctx, device, true)
	if err != nil {
		log.Error(err, "Failed to register BLU device", "device_id", deviceID)
		return deviceID, nil, err
	}

	if modified {
		log.Info("Registered new BLU device", "device_id", deviceID, "mac", eventData.Address, "capabilities", capabilities)
	} else {
		log.V(1).Info("BLU device already exists (unchanged)", "device_id", deviceID)
	}
	return deviceID, &sensors, nil
}

// btHomeInfoEqual compares two BTHomeInfo structs for equality
func btHomeInfoEqual(a, b *shelly.BTHomeInfo) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Version != b.Version || a.Encryption != b.Encryption {
		return false
	}
	if len(a.Capabilities) != len(b.Capabilities) {
		return false
	}
	// Compare capabilities (order matters for simplicity)
	for i, cap := range a.Capabilities {
		if cap != b.Capabilities[i] {
			return false
		}
	}
	return true
}
