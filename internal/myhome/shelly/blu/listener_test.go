package blu

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/events"
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

func (f *fakeBLURegistry) RenameDevice(_ context.Context, _, _ string) error { return nil }

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

	_, sensors, err := handleBLUEvent(ctx, logr.Discard(), topic, payload, reg)
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

	_, sensors, _ := handleBLUEvent(ctx, logr.Discard(), topic, payload, reg)

	if sensors != nil && len(*sensors) > 0 {
		t.Errorf("expected no sensors, got %v", *sensors)
	}
	if len(reg.sensorUpdates) != 0 {
		t.Errorf("expected 0 cache updates, got %d", len(reg.sensorUpdates))
	}
}

func newTestEventsService(t *testing.T) *events.Service {
	t.Helper()
	store, err := events.NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("events.NewStorage: %v", err)
	}
	t.Cleanup(store.Close)
	return events.NewService(logr.Discard(), store, nil, nil, 0)
}

// TestHandleBLUEventBridge_WindowEvent checks that a window open/close event is
// stored with a non-zero Ts, the canonical device id (not the raw MAC), and a
// populated Data field containing the triggering sensor value.
func TestHandleBLUEventBridge_WindowEvent(t *testing.T) {
	const mac = "7c:c6:b6:9e:7c:99"
	open := 1
	data := BLUEventData{
		Address: mac,
		Window:  &open,
		RSSI:    -70,
	}
	payload := buildBLUPayload(t, data)

	svc := newTestEventsService(t)
	before := time.Now().Unix()
	handleBLUEventBridge(context.Background(), logr.Discard(), payload, svc, nil)
	after := time.Now().Unix()

	evts, err := svc.Store().Query(context.Background(), events.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	e := evts[0]

	// Ts must be a plausible receive time, not epoch 0
	if e.Ts < float64(before) || e.Ts > float64(after)+1 {
		t.Errorf("Ts=%v is not within [%d, %d]", e.Ts, before, after+1)
	}

	// DeviceID must be the canonical type-inferred id, not the raw MAC
	wantID := deviceIDFromCapabilities(mac, data)
	if e.DeviceID != wantID {
		t.Errorf("DeviceID=%q, want canonical %q", e.DeviceID, wantID)
	}

	// Data must be populated and contain the triggering field
	if e.Data == nil {
		t.Fatal("Data must not be nil for a window event")
	}
	if !strings.Contains(*e.Data, "window") {
		t.Errorf("Data %q should contain 'window'", *e.Data)
	}
}
