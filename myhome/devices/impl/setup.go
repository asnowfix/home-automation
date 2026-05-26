package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

// deviceRole classifies a device within a room.
type deviceRole int

const (
	roleUnknown      deviceRole = iota
	roleHeater                  // non-BLU, non-sensor device (Shelly Gen2 switch)
	roleTempSensor              // BLU H&T or Gen1 shellyht
	roleDoorSensor              // BLU door/window sensor
	roleMotionSensor            // BLU motion sensor (classified but not wired to heater gates)
)

// classifyDevice returns the role of a device based on its ID and BTHome capabilities.
func classifyDevice(d *myhome.Device) deviceRole {
	id := strings.ToLower(d.Id())

	if shelly.IsBluDevice(id) {
		if d.Info != nil && d.Info.BTHome != nil {
			for _, cap := range d.Info.BTHome.Capabilities {
				switch cap {
				case "window":
					return roleDoorSensor
				case "temperature":
					return roleTempSensor
				case "motion":
					return roleMotionSensor
				}
			}
		}
		return roleUnknown
	}

	if strings.HasPrefix(id, "shellyht-") {
		return roleTempSensor
	}

	// Non-BLU, non-HT → heater candidate
	return roleHeater
}

// sensorMQTTTopic derives the MQTT topic for a sensor device.
// All BLU devices publish to shelly-blu/events/<mac:with:colons> regardless of type.
func sensorMQTTTopic(d *myhome.Device) string {
	id := strings.ToLower(d.Id())

	if shelly.IsBluDevice(id) {
		mac := shelly.MacFromShellyID(id)
		if mac == nil {
			return ""
		}
		return "shelly-blu/events/" + mac.String()
	}

	if strings.HasPrefix(id, "shellyht-") {
		suffix := strings.TrimPrefix(id, "shellyht-")
		return "shellies/shellyht-" + suffix + "/sensor/temperature"
	}

	return ""
}

// SetupRoom classifies devices in a room and pushes sensor topics to each heater's KVS.
// It returns a result describing what was configured for each heater device.
func (dm *DeviceManager) SetupRoom(ctx context.Context, roomID string) (*myhome.RoomSetupResult, error) {
	log := dm.log.WithName("room.setup").WithValues("room_id", roomID)

	devices, err := dm.GetDevicesByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}

	var heaters, tempSensors, doorSensors, motionSensors []*myhome.Device
	for _, d := range devices {
		switch classifyDevice(d) {
		case roleHeater:
			heaters = append(heaters, d)
		case roleTempSensor:
			tempSensors = append(tempSensors, d)
		case roleDoorSensor:
			doorSensors = append(doorSensors, d)
		case roleMotionSensor:
			motionSensors = append(motionSensors, d)
		}
	}

	log.Info("Classified devices",
		"heaters", len(heaters),
		"temp_sensors", len(tempSensors),
		"door_sensors", len(doorSensors),
		"motion_sensors", len(motionSensors))

	// Derive sensor topics
	var tempTopic string
	if len(tempSensors) > 0 {
		tempTopic = sensorMQTTTopic(tempSensors[0]) // first temp sensor
	}

	var doorTopics []string
	for _, d := range doorSensors {
		if t := sensorMQTTTopic(d); t != "" {
			doorTopics = append(doorTopics, t)
		}
	}

	result := &myhome.RoomSetupResult{RoomsProcessed: 1}

	for _, heater := range heaters {
		dr := myhome.RoomSetupDeviceResult{DeviceID: heater.Id()}

		sd, err := dm.GetShellyDevice(ctx, heater)
		if err != nil {
			log.Error(err, "Failed to get Shelly device for heater", "device_id", heater.Id())
			dr.Error = err.Error()
			result.Devices = append(result.Devices, dr)
			continue
		}

		kvsEntries := map[string]string{
			"room-id": roomID,
		}
		if tempTopic != "" {
			kvsEntries["script/heater/internal-temperature-topic"] = tempTopic
			dr.TempSensorTopic = tempTopic
		}
		if len(doorTopics) > 0 {
			kvsEntries["script/heater/door-sensor-topics"] = strings.Join(doorTopics, ",")
			dr.DoorSensorTopics = doorTopics
		}

		for k, v := range kvsEntries {
			if _, err := kvs.SetKeyValue(ctx, log, types.ChannelDefault, sd, k, v); err != nil {
				log.Error(err, "Failed to set KVS key", "device_id", heater.Id(), "key", k)
				dr.Error = fmt.Sprintf("KVS.Set %s: %v", k, err)
			} else {
				dr.KVSKeysSet++
			}
		}

		log.Info("Configured heater KVS",
			"device_id", heater.Id(),
			"kvs_keys", dr.KVSKeysSet,
			"temp_topic", dr.TempSensorTopic,
			"door_topics", dr.DoorSensorTopics)

		result.Devices = append(result.Devices, dr)
	}

	return result, nil
}

// SetupAllRooms runs SetupRoom for every room that has at least one device assigned.
func (dm *DeviceManager) SetupAllRooms(ctx context.Context) *myhome.RoomSetupResult {
	log := dm.log.WithName("room.setup.all")

	// Collect distinct room IDs from all devices
	allDevices, err := dm.dr.GetAllDevices(ctx)
	if err != nil {
		log.Error(err, "Failed to list devices for room setup")
		return &myhome.RoomSetupResult{}
	}

	seen := make(map[string]bool)
	for _, d := range allDevices {
		if d.RoomId != "" {
			seen[d.RoomId] = true
		}
	}

	combined := &myhome.RoomSetupResult{}
	for roomID := range seen {
		r, err := dm.SetupRoom(ctx, roomID)
		if err != nil {
			log.Error(err, "Setup failed for room", "room_id", roomID)
			continue
		}
		combined.RoomsProcessed++
		combined.Devices = append(combined.Devices, r.Devices...)
	}

	log.Info("Room setup complete", "rooms", combined.RoomsProcessed, "devices", len(combined.Devices))
	return combined
}
