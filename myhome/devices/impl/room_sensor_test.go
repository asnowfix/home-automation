package impl

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	mhd "github.com/asnowfix/home-automation/myhome/devices"
	shellyshelly "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/go-logr/logr"
)

// fakeRegistry is an in-memory DeviceRegistry; only what the room handlers use.
type fakeRegistry struct {
	mhd.DeviceRegistry
	devs []*myhome.Device
}

func (f *fakeRegistry) GetAllDevices(context.Context) ([]*myhome.Device, error) { return f.devs, nil }
func (f *fakeRegistry) GetDeviceById(_ context.Context, id string) (*myhome.Device, error) {
	for _, d := range f.devs {
		if d.Id() == id {
			return d, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (f *fakeRegistry) GetDeviceByAny(ctx context.Context, id string) (*myhome.Device, error) {
	return f.GetDeviceById(ctx, id)
}
func (f *fakeRegistry) GetDevicesByRoom(_ context.Context, room string) ([]*myhome.Device, error) {
	var out []*myhome.Device
	for _, d := range f.devs {
		if d.RoomId == room {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeRegistry) SetDeviceRoom(ctx context.Context, id, room string) (bool, error) {
	d, err := f.GetDeviceById(ctx, id)
	if err != nil {
		return false, err
	}
	d.RoomId = room
	return true, nil
}
func (f *fakeRegistry) SetDevice(_ context.Context, d *myhome.Device, _ bool) (bool, error) {
	for i, o := range f.devs {
		if o.Id() == d.Id() {
			f.devs[i] = d
			return false, nil // "not modified": skips the SSE broadcast, which needs a logger
		}
	}
	f.devs = append(f.devs, d)
	return false, nil
}

func sensorIn(id, room string) *myhome.Device {
	d := makeDevice(id, nil)
	d.RoomId = room
	return d
}

func newRoomTestManager(devs ...*myhome.Device) (*DeviceManager, *fakeRegistry) {
	f := &fakeRegistry{devs: devs}
	return &DeviceManager{dr: f, log: logr.Discard(), sseBroadcaster: &recordingSSE{}}, f
}

func TestIsTemperatureSensor(t *testing.T) {
	if !myhome.IsTemperatureSensor(makeDevice("shellyht-abc", nil)) {
		t.Error("Gen1 H&T should be a temperature sensor")
	}
	if !myhome.IsTemperatureSensor(makeDevice("shellyblu-aabbccddeeff", []string{"window", "temperature"})) {
		t.Error("BLU with temperature capability should be a temperature sensor")
	}
	if myhome.IsTemperatureSensor(makeDevice("shellyblu-aabbccddeeff", []string{"battery"})) {
		t.Error("BLU with only battery is the known E7 gap, not a sensor")
	}
	if myhome.IsTemperatureSensor(makeDevice("shellyplus1-aabbcc", nil)) || myhome.IsTemperatureSensor(nil) {
		t.Error("heater / nil must not be a temperature sensor")
	}
}

func TestIsTemperatureSensor_Gen1ByApplication(t *testing.T) {
	d := makeDevice("kitchen-ht", nil) // id without the shellyht- prefix
	d.Info = &shellyshelly.DeviceInfo{}
	d.Info.Application = "shellyht"
	if !myhome.IsTemperatureSensor(d) {
		t.Error("Gen1 H&T identified by Info.Application should be a temperature sensor")
	}
	if !myhome.IsTemperatureSensor(makeDevice("ShellyHT-ABC", nil)) {
		t.Error("id prefix match should be case-insensitive")
	}
}

func TestSetRoom_LookupErrorReturned(t *testing.T) {
	dm, _ := newRoomTestManager()
	dm.dr = &errRegistry{fakeRegistry: &fakeRegistry{}}
	if _, err := dm.handleDeviceSetRoom(context.Background(), &myhome.DeviceSetRoomParams{Identifier: "x", RoomId: "salon"}); err == nil {
		t.Fatal("a non-not-found lookup error must be returned")
	}
}

type errRegistry struct{ *fakeRegistry }

func (e *errRegistry) GetDeviceByAny(context.Context, string) (*myhome.Device, error) {
	return nil, errors.New("database is locked")
}

func TestSetRoom_SecondSensorRejected(t *testing.T) {
	dm, f := newRoomTestManager(sensorIn("shellyht-aaa", "salon"), sensorIn("shellyht-bbb", ""))
	_, err := dm.handleDeviceSetRoom(context.Background(), &myhome.DeviceSetRoomParams{Identifier: "shellyht-bbb", RoomId: "salon"})
	if err == nil || !strings.HasPrefix(err.Error(), `configuration error: room "salon" already has a temperature sensor`) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "shellyht-aaa") || !strings.Contains(err.Error(), "myhome ctl room clear shellyht-aaa") {
		t.Errorf("error should name the existing sensor and the remedy: %v", err)
	}
	if f.devs[1].RoomId != "" {
		t.Errorf("room was written despite the refusal: %q", f.devs[1].RoomId)
	}
}

func TestSetRoom_NonSensorAccepted(t *testing.T) {
	// A BLU door sensor: not a temperature sensor, and setroom skips KVS for BLU
	// devices, so no device implementation is needed.
	door := makeDevice("shellybludoorwindow2-aabbccddeeff", []string{"window", "battery"})
	dm, f := newRoomTestManager(sensorIn("shellyht-aaa", "salon"), door)
	if _, err := dm.handleDeviceSetRoom(context.Background(), &myhome.DeviceSetRoomParams{Identifier: door.Id(), RoomId: "salon"}); err != nil {
		t.Fatalf("door sensor refused: %v", err)
	}
	if f.devs[1].RoomId != "salon" {
		t.Errorf("room = %q", f.devs[1].RoomId)
	}
}

func TestSetRoom_MoveOnlySensorAndClear(t *testing.T) {
	dm, f := newRoomTestManager(sensorIn("shellyht-aaa", "salon"))
	ctx := context.Background()
	if _, err := dm.handleDeviceSetRoom(ctx, &myhome.DeviceSetRoomParams{Identifier: "shellyht-aaa", RoomId: "salon"}); err != nil {
		t.Fatalf("re-set in same room: %v", err)
	}
	if _, err := dm.handleDeviceSetRoom(ctx, &myhome.DeviceSetRoomParams{Identifier: "shellyht-aaa", RoomId: "cuisine"}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if f.devs[0].RoomId != "cuisine" {
		t.Errorf("room = %q", f.devs[0].RoomId)
	}
	if _, err := dm.handleDeviceSetRoom(ctx, &myhome.DeviceSetRoomParams{Identifier: "shellyht-aaa", RoomId: ""}); err != nil {
		t.Fatalf("clear: %v", err)
	}
}

func TestUpdate_SecondSensorRejected(t *testing.T) {
	dm, f := newRoomTestManager(sensorIn("shellyht-aaa", "salon"), sensorIn("shellyht-bbb", ""))
	_, err := dm.handleDeviceUpdate(context.Background(), sensorIn("shellyht-bbb", "salon"))
	if err == nil || !strings.Contains(err.Error(), "already has a temperature sensor") {
		t.Fatalf("err = %v", err)
	}
	if f.devs[1].RoomId != "" {
		t.Errorf("room written despite refusal: %q", f.devs[1].RoomId)
	}
	// New (unknown) device into an occupied room is refused too.
	if _, err := dm.handleDeviceUpdate(context.Background(), sensorIn("shellyht-ccc", "salon")); err == nil {
		t.Error("unknown sensor into occupied room should be refused")
	}
}

func TestUpdate_LegacyResaveAndNonSensorAccepted(t *testing.T) {
	// Two sensors already in "salon" (data from before the rule): a re-save that
	// does not change the room must not be refused.
	dm, _ := newRoomTestManager(sensorIn("shellyht-aaa", "salon"), sensorIn("shellyht-bbb", "salon"))
	if _, err := dm.handleDeviceUpdate(context.Background(), sensorIn("shellyht-bbb", "salon")); err != nil {
		t.Fatalf("legacy re-save refused: %v", err)
	}
	heater := makeDevice("shellyplus1-aabbcc", nil)
	heater.RoomId = "salon"
	if _, err := dm.handleDeviceUpdate(context.Background(), heater); err != nil {
		t.Fatalf("non-sensor refused: %v", err)
	}
}

func TestSetupRoom_RefusesSeveralSensors(t *testing.T) {
	heater := makeDevice("shellyplus1-aabbcc", nil)
	heater.RoomId = "salon"
	dm, _ := newRoomTestManager(heater, sensorIn("shellyht-aaa", "salon"), sensorIn("shellyht-bbb", "salon"))
	res, err := dm.SetupRoom(context.Background(), "salon")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 1 || !strings.HasPrefix(res.Devices[0].Error, "configuration error: room") || res.Devices[0].KVSKeysSet != 0 {
		t.Fatalf("result = %+v", res.Devices)
	}
}
