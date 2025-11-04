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

// StartMqttListener listens to Gen1 MQTT sensor topics and updates device status
// It subscribes to shellies/+/sensor/# and auto-registers devices as they publish data
func StartMqttListener(ctx context.Context, mc mqtt.Client, sc devices.DeviceRegistry, router model.Router) error {
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
				handleMessage(ctx, log, sc, router, msg.Topic(), msg.Payload())
			}
		}
	}()

	return nil
}

// handleMessage processes a Gen1 sensor MQTT message
func handleMessage(ctx context.Context, log logr.Logger, sc devices.DeviceRegistry, router model.Router, topic string, payload []byte) {
	var device Device

	// Parse topic: shellies/<device-id>/sensor/<sensor-type>
	// Example: shellies/shellyht-208500/sensor/temperature
	parts := strings.Split(topic, "/")
	switch len(parts) {
	case 4:
		deviceId := parts[1]
		sensorType := parts[3]
		log.V(1).Info("Received Gen1 sensor data", "device_id", deviceId, "sensor", sensorType, "payload", string(payload))
	case 3:
		if parts[2] != "info" {
			log.V(1).Info("Dropping Gen1 unknown message", "topic", topic)
			return
		}
		err := json.Unmarshal(payload, &device)
		if err != nil {
			log.Error(err, "Failed to parse Gen1 device info", "payload", string(payload))
			return
		}
		if device.Id != parts[1] {
			log.Error(err, "Device ID mismatch", "topic", topic, "device_id", device.Id)
			return
		}
		log.V(1).Info("Received Gen1 device info", "device", device)
	default:
		log.V(1).Info("Dropping unknown Gen1 message", "topic", topic)
		return
	}

	// Parse the sensor value as a Number
	var number float64
	if err := json.Unmarshal(payload, &number); err != nil {
		log.Error(err, "Failed to parse sensor value", "payload", string(payload))
		return
	}

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
			return
		}
		mhd.WithMAC(host.Mac())
	}

	// Save the device (will create or update)
	if err := sc.SetDevice(ctx, mhd, true); err != nil {
		log.Error(err, "Failed to save Gen1 device", "device", mhd)
		return
	}

	log.Info("Created/Updatd Gen1 device", "device", mhd)
}
