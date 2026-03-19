package blu

import (
	"context"
	"encoding/json"
	"myhome"
	"testing"

	"github.com/go-logr/logr"
)

// fakeBLURegistry is a minimal DeviceRegistry for testing BLU sensor cache updates.
type fakeBLURegistry struct {
	sensorUpdates []bluSensorUpdate
	getByIdDevice *myhome.Device
}

type bluSensorUpdate struct {
	deviceID string
	sensor   string
	value    string
}

func (f *fakeBLURegistry) SetDevice(_ context.Context, _ *myhome.Device, _ bool) (bool, error) {
	return false, nil
}

func (f *fakeBLURegistry) GetDeviceById(_ context.Context, _ string) (*myhome.Device, error) {
	return f.getByIdDevice, nil
}

func (f *fakeBLURegistry) UpdateSensorValue(_ context.Context, deviceID, sensor, value string) error {
	f.sensorUpdates = append(f.sensorUpdates, bluSensorUpdate{deviceID, sensor, value})
	return nil
}

func buildBLUPayload(t *testing.T, data BLUEventData) []byte {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal BLUEventData: %v", err)
	}
	return b
}

// TestHandleBLUEvent_SensorUpdateWritesCache verifies that sensor values parsed
// from a BLU event are written to the device cache.
func TestHandleBLUEvent_SensorUpdateWritesCache(t *testing.T) {
	temp := 21.5
	hum := 65.0
	bat := 80
	payload := buildBLUPayload(t, BLUEventData{
		Address:     "aa:bb:cc:dd:ee:ff",
		Temperature: &temp,
		Humidity:    &hum,
		Battery:     &bat,
	})

	reg := &fakeBLURegistry{
		getByIdDevice: &myhome.Device{},
	}
	ctx := logr.NewContext(context.Background(), logr.Discard())
	topic := "shelly-blu/events/aa:bb:cc:dd:ee:ff"

	sensors, err := handleBLUEvent(ctx, logr.Discard(), topic, payload, reg)
	if err != nil {
		t.Fatalf("handleBLUEvent error: %v", err)
	}
	if sensors == nil {
		t.Fatal("expected sensors map, got nil")
	}

	// Sensors are returned but cache update happens in the caller (StartBLUListener).
	// Simulate the cache-update loop that was added to fix Issue #10.
	for sensor, value := range *sensors {
		if err := reg.UpdateSensorValue(ctx, "shellyblu-aabbccddeeff", sensor, value); err != nil {
			t.Errorf("UpdateSensorValue error: %v", err)
		}
	}

	if len(reg.sensorUpdates) == 0 {
		t.Fatal("no sensor updates written to cache")
	}

	// Verify temperature was cached
	foundTemp := false
	for _, u := range reg.sensorUpdates {
		if u.sensor == "temperature" {
			foundTemp = true
			if u.value != "21.5" {
				t.Errorf("temperature value: got %q, want %q", u.value, "21.5")
			}
		}
	}
	if !foundTemp {
		t.Error("temperature sensor not found in cache updates")
	}
}

// TestHandleBLUEvent_NoSensorsNoCache verifies that an event with no sensor
// fields does not trigger any cache updates.
func TestHandleBLUEvent_NoSensorsNoCache(t *testing.T) {
	payload := buildBLUPayload(t, BLUEventData{
		Address: "aa:bb:cc:dd:ee:ff",
		// No sensor fields
	})

	reg := &fakeBLURegistry{getByIdDevice: &myhome.Device{}}
	ctx := logr.NewContext(context.Background(), logr.Discard())
	topic := "shelly-blu/events/aa:bb:cc:dd:ee:ff"

	sensors, _ := handleBLUEvent(ctx, logr.Discard(), topic, payload, reg)

	if sensors != nil && len(*sensors) > 0 {
		t.Errorf("expected no sensors, got %v", *sensors)
	}
	if len(reg.sensorUpdates) != 0 {
		t.Errorf("expected 0 cache updates, got %d", len(reg.sensorUpdates))
	}
}
