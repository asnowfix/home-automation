package gen1

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/myhome/model"
	"github.com/asnowfix/home-automation/myhome/devices"
	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/go-logr/logr"
)

// SensorUpdate represents a parsed sensor update in a common format
type SensorUpdate struct {
	DeviceID string
	Type     string
	Value    string
}

// SSEBroadcaster interface for broadcasting sensor updates to UI
type SSEBroadcaster interface {
	BroadcastSensorUpdate(deviceID string, sensor string, value string)
}

// ParseSensorEvent parses a Gen1 MQTT sensor event and extracts sensor data
// Returns nil if the event doesn't contain sensor data
// <https://shelly-api-docs.shelly.cloud/gen1/#shelly-sense-mqtt>
func ParseSensorEvent(topic string, payload []byte) *SensorUpdate {
	// Parse Gen1 sensor topic: shellies/<device-id>/sensor/<sensor-type>
	parts := strings.Split(topic, "/")
	if len(parts) != 4 || parts[2] != "sensor" {
		return nil
	}

	deviceID := parts[1]
	sensorType := parts[3]
	value := string(payload)

	return &SensorUpdate{
		DeviceID: deviceID,
		Type:     sensorType,
		Value:    value,
	}
}

// StartMqttListener listens to Gen1 MQTT topics and updates device status
// It subscribes to shellies/# and auto-registers devices as they publish data
func StartMqttListener(ctx context.Context, log logr.Logger, mc mqtt.Client, sc devices.DeviceRegistry, router model.Router, sseBroadcaster SSEBroadcaster) error {
	return StartMqttListenerWithEvents(ctx, log, mc, sc, router, sseBroadcaster, nil, nil)
}

// StartMqttListenerWithEvents is like StartMqttListener but also records events and sensor observations.
// eventSvc and tracker may be nil, in which case event recording is skipped.
func StartMqttListenerWithEvents(ctx context.Context, log logr.Logger, mc mqtt.Client, sc devices.DeviceRegistry, router model.Router, sseBroadcaster SSEBroadcaster, eventSvc *events.Service, tracker *events.SensorDailyTracker) error {
	log = log.WithName("Gen1MqttListener")

	log.Info("Starting Gen1 MQTT listener using MQTT client type", "type", fmt.Sprintf("%T", mc))

	// Subscribe to all Gen1 topics: (not just sensors like `shellies/+/sensor/#`)
	// This will match: shellies/<device-id>/info, shellies/<device-id>/sensor/temperature, shellies/<device-id>/sensor/humidity, etc.
	topic := "shellies/#"
	err := mc.SubscribeWithHandler(ctx, topic, 16, "shelly/gen1", func(topic string, payload []byte, subscriber string) error {
		// Handle device registration
		err := handleMessage(ctx, log, sc, router, topic, payload)

		// Broadcast sensor updates via SSE if broadcaster is available
		if sseBroadcaster != nil {
			if update := ParseSensorEvent(topic, payload); update != nil {
				log.Info("Broadcasting Gen1 sensor update via SSE", "device_id", update.DeviceID, "sensor", update.Type, "value", update.Value)
				sseBroadcaster.BroadcastSensorUpdate(update.DeviceID, update.Type, update.Value)
			} else {
				log.V(1).Info("Not a sensor event, skipping SSE broadcast", "topic", topic, "payload", payload)
			}
		} else {
			log.Error(fmt.Errorf("sseBroadcaster is nil"), "Cannot broadcast sensor update", "topic", topic)
		}

		// Feed event log and sensor tracker (nil-safe)
		handleGen1EventBridge(ctx, log, topic, payload, eventSvc, tracker)

		return err
	})
	if err != nil {
		log.Error(err, "failed to subscribe to Gen1 events", "topic", topic)
		return err
	}

	log.Info("started", "topic", topic)
	return nil
}

// handleGen1EventBridge maps Gen1 MQTT topics to event log entries and tracker observations.
func handleGen1EventBridge(ctx context.Context, log logr.Logger, topic string, payload []byte, eventSvc *events.Service, tracker *events.SensorDailyTracker) {
	parts := strings.Split(topic, "/")
	// Minimum: shellies/<device-id>/<subtopic>
	if len(parts) < 3 || parts[0] != "shellies" {
		return
	}
	deviceID := parts[1]

	switch {
	case len(parts) == 4 && parts[2] == "relay":
		// shellies/<id>/relay/<n> → switch.on / switch.off
		if eventSvc == nil {
			return
		}
		n := parts[3]
		component := "switch:" + n
		eventName := "switch.off"
		if string(payload) == "on" {
			eventName = "switch.on"
		}
		e := events.Event{
			DeviceID:  deviceID,
			Component: component,
			Event:     eventName,
			Severity:  "info",
		}
		if err := eventSvc.Record(ctx, e); err != nil {
			log.Error(err, "Failed to record switch event", "device_id", deviceID)
		}

	case len(parts) == 3 && parts[2] == "online":
		// shellies/<id>/online → device.online / device.offline
		if eventSvc == nil {
			return
		}
		eventName := "device.offline"
		if string(payload) == "1" || string(payload) == "true" {
			eventName = "device.online"
		}
		e := events.Event{
			DeviceID:  deviceID,
			Component: "mqtt",
			Event:     eventName,
			Severity:  "info",
		}
		if err := eventSvc.Record(ctx, e); err != nil {
			log.Error(err, "Failed to record online event", "device_id", deviceID)
		}

	case len(parts) == 4 && parts[2] == "sensor" && parts[3] == "temperature":
		// shellies/<id>/sensor/temperature → tracker only, no event row
		if tracker == nil {
			return
		}
		v, err := strconv.ParseFloat(string(payload), 64)
		if err != nil {
			return
		}
		if err := tracker.Observe(ctx, events.Metric{DeviceID: deviceID, Component: "temperature:0", Metric: "tC"}, v); err != nil {
			log.Error(err, "Failed to observe temperature", "device_id", deviceID)
		}

	case len(parts) == 4 && parts[2] == "sensor" && parts[3] == "humidity":
		// shellies/<id>/sensor/humidity → tracker only, no event row
		if tracker == nil {
			return
		}
		v, err := strconv.ParseFloat(string(payload), 64)
		if err != nil {
			return
		}
		if err := tracker.Observe(ctx, events.Metric{DeviceID: deviceID, Component: "humidity:0", Metric: "rh"}, v); err != nil {
			log.Error(err, "Failed to observe humidity", "device_id", deviceID)
		}
	}
}

// handleMessage processes a Gen1 MQTT message
func handleMessage(ctx context.Context, log logr.Logger, sc devices.DeviceRegistry, router model.Router, topic string, payload []byte) error {
	// Parse topic: shellies/<device-id>/sensor/<sensor-type> or shellies/<device-id>/info
	// Example: shellies/shellyht-208500/sensor/temperature or shellies/shellyht-208500/info
	parts := strings.Split(topic, "/")

	modified := false

	switch len(parts) {
	case 4:
		// Sensor value topic: shellies/<device-id>/sensor/<sensor-type>
		deviceId := parts[1]
		sensorType := parts[3]
		value := string(payload)
		log.Info("Received Gen1 sensor data", "device_id", deviceId, "sensor", sensorType, "value", value)

		// Update device cache so sensor values survive page reloads
		if err := sc.UpdateSensorValue(ctx, deviceId, sensorType, value); err != nil {
			log.V(1).Info("Failed to update sensor in cache", "error", err, "device_id", deviceId)
		}

	case 3:
		// Info topic: shellies/<device-id>/info
		if parts[2] != "info" {
			log.V(1).Info("Dropping Gen1 unknown message", "topic", topic, "payload", string(payload))
			return nil
		}

		var device Device
		err := json.Unmarshal(payload, &device)
		if err != nil {
			log.Error(err, "Failed to parse Gen1 device info", "payload", string(payload))
			return err
		}

		if device.Id != parts[1] {
			err := fmt.Errorf("Gen1 device ID mismatch: expected %s, got %s", parts[1], device.Id)
			log.Error(err, "Gen1 device ID mismatch", "topic", topic, "expected", parts[1], "got", device.Id)
			return err
		}

		log.V(1).Info("Received Gen1 device info", "device", device)

		// Get or create the device
		mhd, err := sc.GetDeviceById(ctx, device.Id)
		if err != nil {
			// Device doesn't exist - create it as a Gen1 device
			log.Info("Auto-registering Gen1 device", "device_id", device.Id)
			mhd = &myhome.Device{}
			mhd = mhd.WithId(device.Id)
			mhd = mhd.WithName(device.Id)

			host, err := router.GetHostByIp(ctx, device.Ip)
			if err != nil {
				log.Error(err, "Failed to get host", "ip", device.Ip)
				return err
			}
			mhd = mhd.WithMAC(host.Mac())
		}

		// Save the device (will create or update)
		if modified, err = sc.SetDevice(ctx, mhd, true); err != nil {
			log.Error(err, "Failed to save Gen1 device", "device", mhd)
			return err
		}

		if modified {
			log.Info("Created/Updated Gen1 device", "device", mhd)
		} else {
			log.V(1).Info("Gen1 device unchanged", "device", mhd)
		}

		// SSEBroadcaster.BroadcastSensorUpdate(device.Id, "info", string(payload))

	default:
		log.V(1).Info("Dropping unknown Gen1 message", "topic", topic)
	}

	return nil
}
