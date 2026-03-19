package gen1

import (
	"context"
	"fmt"
	"myhome"
	"myhome/devices"
	"myhome/model"
	"testing"

	"github.com/go-logr/logr"
)

// fakeRegistry is a minimal DeviceRegistry for testing sensor update propagation.
type fakeRegistry struct {
	devices.DeviceRegistry // embed to satisfy unimplemented methods

	sensorUpdates []sensorUpdate
	getByIdErr    error
	getByIdDevice *myhome.Device
}

type sensorUpdate struct {
	deviceID string
	sensor   string
	value    string
}

func (f *fakeRegistry) UpdateSensorValue(_ context.Context, deviceID, sensor, value string) error {
	f.sensorUpdates = append(f.sensorUpdates, sensorUpdate{deviceID, sensor, value})
	return nil
}

func (f *fakeRegistry) GetDeviceById(_ context.Context, id string) (*myhome.Device, error) {
	return f.getByIdDevice, f.getByIdErr
}

// TestHandleMessage_SensorUpdateWritesCache verifies that a Gen1 sensor topic
// calls UpdateSensorValue on the registry so the value survives page reloads.
func TestHandleMessage_SensorUpdateWritesCache(t *testing.T) {
	reg := &fakeRegistry{}
	ctx := logr.NewContext(context.Background(), logr.Discard())

	topic := "shellies/shellyht-abc123/sensor/temperature"
	payload := []byte("21.5")

	err := handleMessage(ctx, logr.Discard(), reg, nil, topic, payload)
	if err != nil {
		t.Fatalf("handleMessage returned error: %v", err)
	}

	if len(reg.sensorUpdates) != 1 {
		t.Fatalf("expected 1 sensor update, got %d", len(reg.sensorUpdates))
	}
	u := reg.sensorUpdates[0]
	if u.deviceID != "shellyht-abc123" {
		t.Errorf("deviceID: got %q, want %q", u.deviceID, "shellyht-abc123")
	}
	if u.sensor != "temperature" {
		t.Errorf("sensor: got %q, want %q", u.sensor, "temperature")
	}
	if u.value != "21.5" {
		t.Errorf("value: got %q, want %q", u.value, "21.5")
	}
}

// TestHandleMessage_SensorTypes verifies multiple sensor types are forwarded.
func TestHandleMessage_SensorTypes(t *testing.T) {
	cases := []struct {
		topic   string
		payload string
		wantID  string
		wantSensor string
	}{
		{"shellies/dev1/sensor/humidity", "65", "dev1", "humidity"},
		{"shellies/dev2/sensor/battery", "42", "dev2", "battery"},
		{"shellies/dev3/sensor/illuminance", "300.5", "dev3", "illuminance"},
	}

	for _, tc := range cases {
		t.Run(tc.topic, func(t *testing.T) {
			reg := &fakeRegistry{}
			ctx := logr.NewContext(context.Background(), logr.Discard())

			if err := handleMessage(ctx, logr.Discard(), reg, nil, tc.topic, []byte(tc.payload)); err != nil {
				t.Fatalf("handleMessage error: %v", err)
			}
			if len(reg.sensorUpdates) != 1 {
				t.Fatalf("expected 1 sensor update, got %d", len(reg.sensorUpdates))
			}
			if got := reg.sensorUpdates[0].deviceID; got != tc.wantID {
				t.Errorf("deviceID: got %q, want %q", got, tc.wantID)
			}
			if got := reg.sensorUpdates[0].sensor; got != tc.wantSensor {
				t.Errorf("sensor: got %q, want %q", got, tc.wantSensor)
			}
		})
	}
}

// TestHandleMessage_NonSensorTopicNoUpdate verifies info topics don't call UpdateSensorValue.
func TestHandleMessage_NonSensorTopicNoUpdate(t *testing.T) {
	reg := &fakeRegistry{
		getByIdErr: fmt.Errorf("not found"),
	}
	_ = model.Router(nil) // ensure model is used
	ctx := logr.NewContext(context.Background(), logr.Discard())
	// info topic: 3 parts, not a sensor update
	handleMessage(ctx, logr.Discard(), reg, nil, "shellies/dev1/info", []byte("{}"))

	if len(reg.sensorUpdates) != 0 {
		t.Errorf("expected 0 sensor updates for info topic, got %d", len(reg.sensorUpdates))
	}
}
