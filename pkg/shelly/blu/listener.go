package blu

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome"
	"myhome/mqtt"
	"net"
	"pkg/shelly/shelly"
	"strings"

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
	SetDevice(ctx context.Context, device *myhome.Device, overwrite bool) error
	GetDeviceById(ctx context.Context, id string) (*myhome.Device, error)
}

// StartBLUListener starts listening for Shelly BLU device events on MQTT
// and registers discovered devices with their sensor capabilities
func StartBLUListener(ctx context.Context, mqttClient *mqtt.Client, registry DeviceRegistry) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return fmt.Errorf("no logger in context: %w", err)
	}
	log = log.WithName("BLUListener")

	log.Info("Starting Shelly BLU listener")

	// Subscribe to all shelly-blu events
	topic := "shelly-blu/events/#"
	msgChan, err := mqttClient.SubscriberWithTopic(ctx, topic, 100)
	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", topic, err)
	}

	go func() {
		log.Info("BLU listener started", "topic", topic)
		for {
			select {
			case <-ctx.Done():
				log.Info("BLU listener stopped")
				return
			case msg := <-msgChan:
				if msg == nil {
					continue
				}
				if mqttMsg, ok := msg.(*mqtt.MqttMessage); ok {
					handleBLUEvent(ctx, log, mqttMsg.Topic(), mqttMsg.Payload(), registry)
				}
			}
		}
	}()

	return nil
}

func handleBLUEvent(ctx context.Context, log logr.Logger, topic string, payload []byte, registry DeviceRegistry) {
	// Parse the event data
	var eventData BLUEventData
	if err := json.Unmarshal(payload, &eventData); err != nil {
		log.V(1).Info("Failed to parse BLU event", "topic", topic, "error", err)
		return
	}

	// Validate MAC address
	if eventData.Address == "" {
		log.V(1).Info("BLU event missing MAC address", "topic", topic)
		return
	}

	// Normalize MAC address: lowercase, remove colons
	mac := strings.ToLower(strings.ReplaceAll(eventData.Address, ":", ""))
	deviceID := "shellyblu-" + mac

	log.V(1).Info("Received BLU event", "device_id", deviceID, "topic", topic)

	// Determine sensor capabilities from the event data
	capabilities := []string{}

	// Power & Energy
	if eventData.Battery != nil {
		capabilities = append(capabilities, "battery")
	}
	if eventData.Energy != nil {
		capabilities = append(capabilities, "energy")
	}
	if eventData.Power != nil {
		capabilities = append(capabilities, "power")
	}
	if eventData.Voltage != nil {
		capabilities = append(capabilities, "voltage")
	}
	if eventData.Current != nil {
		capabilities = append(capabilities, "current")
	}

	// Environmental Sensors
	if eventData.Temperature != nil {
		capabilities = append(capabilities, "temperature")
	}
	if eventData.Humidity != nil {
		capabilities = append(capabilities, "humidity")
	}
	if eventData.Pressure != nil {
		capabilities = append(capabilities, "pressure")
	}
	if eventData.Illuminance != nil {
		capabilities = append(capabilities, "illuminance")
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
	}
	if eventData.Window != nil {
		capabilities = append(capabilities, "window")
	}
	if eventData.Button != nil {
		capabilities = append(capabilities, "button")
	}
	if eventData.Rotation != nil {
		capabilities = append(capabilities, "rotation")
	}

	// Distance
	if eventData.DistanceMM != nil {
		capabilities = append(capabilities, "distance_mm")
	}
	if eventData.DistanceM != nil {
		capabilities = append(capabilities, "distance_m")
	}

	// Timestamp
	if eventData.Timestamp != nil {
		capabilities = append(capabilities, "timestamp")
	}

	// Acceleration
	if eventData.Acceleration != nil {
		capabilities = append(capabilities, "acceleration")
	}

	// Variable-length data
	if eventData.Text != nil {
		capabilities = append(capabilities, "text")
	}
	if eventData.Raw != nil {
		capabilities = append(capabilities, "raw")
	}

	// Extract model from local_name (lowercased)
	model := "shellyblu"
	if eventData.BTHome != nil && eventData.BTHome.LocalName != "" {
		model = strings.ToLower(eventData.BTHome.LocalName)
	}

	// Build device info with sensor capabilities and BTHome frame data (static device characteristics only)
	deviceInfo := &shelly.DeviceInfo{
		Product: shelly.Product{
			Model:      model,
			MacAddress: eventData.Address,
			Generation: 0, // BLU devices don't have a generation
		},
		Id: deviceID,
	}

	// Add BTHome protocol information if available
	if eventData.BTHome != nil {
		deviceInfo.BTHome = &shelly.BTHomeInfo{
			Version:          eventData.BTHomeVersion,
			Encryption:       eventData.Encryption,
			Capabilities:     capabilities,
			ServiceData:      eventData.BTHome.ServiceData,
			ManufacturerData: eventData.BTHome.ManufacturerData,
		}
	}

	// Note: Battery level is dynamic data and should not be stored in DeviceInfo
	// It should be retrieved from real-time status/events instead

	// Parse MAC address
	macAddr, parseErr := net.ParseMAC(eventData.Address)
	if parseErr != nil {
		log.Error(parseErr, "Failed to parse MAC address", "device_id", deviceID, "mac", eventData.Address)
		return
	}

	// Create device entry
	device := myhome.NewDevice(log, myhome.SHELLY, deviceID)
	device = device.WithMAC(macAddr)
	device = device.WithName(deviceID) // Use device ID as default name
	device.Info = deviceInfo

	// Register or update the device (overwrite=true to update capabilities if they change)
	if err := registry.SetDevice(ctx, device, true); err != nil {
		log.Error(err, "Failed to register BLU device", "device_id", deviceID)
		return
	}

	log.V(1).Info("Registered/updated BLU device", "device_id", deviceID, "mac", eventData.Address, "capabilities", capabilities)
}
