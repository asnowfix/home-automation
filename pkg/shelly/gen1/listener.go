package gen1

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome"
	"myhome/devices"
	"pkg/shelly/mqtt"
	"pkg/shelly/shelly"
	"strings"

	"github.com/go-logr/logr"
)

// StartMqttListener listens to Gen1 MQTT sensor topics and updates device status
// It subscribes to shellies/+/sensor/# and auto-registers devices as they publish data
func StartMqttListener(ctx context.Context, mc mqtt.Client, sc devices.DeviceRegistry) error {
	log := logr.FromContextOrDiscard(ctx).WithName("Gen1MqttListener")

	log.Info("Starting Gen1 MQTT listener")

	// Check if the client supports SubscriberWithTopic
	subscriber, ok := mc.(mqtt.SubscriberWithTopic)
	if !ok {
		return fmt.Errorf("MQTT client does not support SubscriberWithTopic")
	}

	// Subscribe to all Gen1 sensor topics: shellies/+/sensor/#
	// This will match: shellies/<device-id>/sensor/temperature, shellies/<device-id>/sensor/humidity, etc.
	topic := "shellies/+/sensor/#"
	ch, err := subscriber.SubscriberWithTopic(ctx, topic, 16)
	if err != nil {
		log.Error(err, "Failed to subscribe to Gen1 sensor topics", "topic", topic)
		return err
	}

	log.Info("Subscribed to Gen1 sensor topics", "topic", topic)

	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Info("Exiting Gen1 listener")
				return

			case msg := <-ch:
				handleSensorMessage(ctx, log, sc, msg.Topic(), msg.Payload())
			}
		}
	}()

	return nil
}

// handleSensorMessage processes a Gen1 sensor MQTT message
func handleSensorMessage(ctx context.Context, log logr.Logger, sc devices.DeviceRegistry, topic string, payload []byte) {
	// Parse topic: shellies/<device-id>/sensor/<sensor-type>
	// Example: shellies/shellyht-208500/sensor/temperature
	parts := strings.Split(topic, "/")
	if len(parts) != 4 {
		log.V(1).Info("Ignoring invalid Gen1 topic format", "topic", topic)
		return
	}

	deviceId := parts[1]
	sensorType := parts[3]

	log.V(1).Info("Received Gen1 sensor data", "device_id", deviceId, "sensor", sensorType, "payload", string(payload))

	// Parse the sensor value as a Number
	var number float64
	if err := json.Unmarshal(payload, &number); err != nil {
		log.Error(err, "Failed to parse sensor value", "payload", string(payload))
		return
	}

	// Get or create the device
	device, err := sc.GetDeviceById(ctx, deviceId)
	if err != nil {
		// Device doesn't exist - create it as a Gen1 device
		log.Info("Auto-registering Gen1 device", "device_id", deviceId)
		device = &myhome.Device{}
		device.WithId(deviceId)
		device.WithName(deviceId)
	}
	if device.Status == nil {
		device.Status = &shelly.Status{}
	}
	if device.Status.Gen1 == nil {
		device.Status.Gen1 = &map[string]float32{}
	}
	(*device.Status.Gen1)[sensorType] = float32(number)

	// Save the device (will create or update)
	if err := sc.SetDevice(ctx, device, true); err != nil {
		log.Error(err, "Failed to save Gen1 device", "device_id", deviceId)
		return
	}

	log.Info("Updated Gen1 device status", "device_id", deviceId, "sensor", sensorType, "value", number)
}
