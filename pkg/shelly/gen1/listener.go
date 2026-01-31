package gen1

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome"
	"myhome/devices"
	"myhome/model"
	"pkg/shelly/mqtt"
	"strings"

	"github.com/go-logr/logr"
)

// SensorUpdate represents a parsed sensor update in a common format
type SensorUpdate struct {
	DeviceID    string
	Temperature *float64
	DoorOpened  *bool // true if door/window is open, false if closed (Gen1 doesn't have door sensors)
}

// SSEBroadcaster interface for broadcasting sensor updates to UI
type SSEBroadcaster interface {
	BroadcastSensorUpdate(deviceID string, sensor string, value float64)
	BroadcastDoorStatus(deviceID string, opened bool)
}

// ParseSensorEvent parses a Gen1 MQTT sensor event and extracts sensor data
// Returns nil if the event doesn't contain sensor data
func ParseSensorEvent(topic string, payload []byte) *SensorUpdate {
	// Parse Gen1 sensor topic: shellies/<device-id>/sensor/<sensor-type>
	parts := strings.Split(topic, "/")
	if len(parts) != 4 || parts[2] != "sensor" {
		return nil
	}

	deviceID := parts[1]
	sensorType := parts[3]

	// Only handle temperature for now
	if sensorType != "temperature" {
		return nil
	}

	// Parse sensor value
	var value float64
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil
	}

	return &SensorUpdate{
		DeviceID:    deviceID,
		Temperature: &value,
	}
}

// StartMqttListener listens to Gen1 MQTT topics and updates device status
// It subscribes to shellies/# and auto-registers devices as they publish data
func StartMqttListener(ctx context.Context, mc mqtt.Client, sc devices.DeviceRegistry, router model.Router, sseBroadcaster SSEBroadcaster) error {
	log := logr.FromContextOrDiscard(ctx).WithName("Gen1MqttListener")

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
				if update.Temperature != nil {
					sseBroadcaster.BroadcastSensorUpdate(update.DeviceID, "temperature", *update.Temperature)
				}
				if update.DoorOpened != nil {
					sseBroadcaster.BroadcastDoorStatus(update.DeviceID, *update.DoorOpened)
				}
			}
		}

		return err
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to Gen1 topics: %w", err)
	}

	log.Info("Gen1 MQTT listener started", "topic", topic)
	return nil
}

// handleMessage processes a Gen1 MQTT message
func handleMessage(ctx context.Context, log logr.Logger, sc devices.DeviceRegistry, router model.Router, topic string, payload []byte) error {
	// Parse topic: shellies/<device-id>/sensor/<sensor-type> or shellies/<device-id>/info
	// Example: shellies/shellyht-208500/sensor/temperature or shellies/shellyht-208500/info
	parts := strings.Split(topic, "/")

	switch len(parts) {
	case 4:
		// Sensor value topic: shellies/<device-id>/sensor/<sensor-type>
		deviceId := parts[1]
		sensorType := parts[3]

		// Parse the sensor value as a number
		var number float64
		if err := json.Unmarshal(payload, &number); err != nil {
			log.Error(err, "Failed to parse sensor value", "device_id", deviceId, "sensor", sensorType, "payload", string(payload))
			return err
		}

		log.V(1).Info("Received Gen1 sensor data", "device_id", deviceId, "sensor", sensorType, "value", number)

	case 3:
		// Info topic: shellies/<device-id>/info
		if parts[2] != "info" {
			log.V(1).Info("Dropping Gen1 unknown message", "topic", topic)
			return nil
		}

		var device Device
		err := json.Unmarshal(payload, &device)
		if err != nil {
			log.Error(err, "Failed to parse Gen1 device info", "payload", string(payload))
			return err
		}

		if device.Id != parts[1] {
			log.Error(nil, "Gen1 device ID mismatch", "topic", topic, "expected", parts[1], "got", device.Id)
			return err
		}

		log.V(1).Info("Received Gen1 device info", "device", device)

		// Get or create the device
		mhd, err := sc.GetDeviceById(ctx, device.Id)
		if err != nil {
			// Device doesn't exist - create it as a Gen1 device
			log.Info("Auto-registering Gen1 device", "device_id", device.Id)
			mhd = &myhome.Device{}
			mhd.WithId(device.Id)
			mhd.WithName(device.Id)

			host, err := router.GetHostByIp(ctx, device.Ip)
			if err != nil {
				log.Error(err, "Failed to get host", "ip", device.Ip)
				return err
			}
			mhd.WithMAC(host.Mac())
		}

		// Save the device (will create or update)
		if err := sc.SetDevice(ctx, mhd, true); err != nil {
			log.Error(err, "Failed to save Gen1 device", "device", mhd)
			return err
		}

		log.Info("Created/Updated Gen1 device", "device", mhd)

	default:
		log.V(1).Info("Dropping unknown Gen1 message", "topic", topic)
	}

	return nil
}
