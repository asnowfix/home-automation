package blu

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/go-logr/logr"
)

// memBLURegistry is a stateful DeviceRegistry mimicking the real cache's keys:
// by id and by MAC, and a SetDevice that replaces any record sharing the MAC.
type memBLURegistry struct {
	byID map[string]*myhome.Device
}

func newMemBLURegistry(devs ...*myhome.Device) *memBLURegistry {
	r := &memBLURegistry{byID: map[string]*myhome.Device{}}
	for _, d := range devs {
		r.byID[d.Id()] = d
	}
	return r
}

func (r *memBLURegistry) SetDevice(_ context.Context, d *myhome.Device, _ bool) (bool, error) {
	for id, e := range r.byID {
		if e.MAC != "" && e.MAC == d.MAC && id != d.Id() {
			delete(r.byID, id) // same-MAC replacement, as Cache.SetDevice does
		}
	}
	r.byID[d.Id()] = d
	return true, nil
}

func (r *memBLURegistry) GetDeviceById(_ context.Context, id string) (*myhome.Device, error) {
	if d, ok := r.byID[id]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func (r *memBLURegistry) GetDeviceByMAC(_ context.Context, mac string) (*myhome.Device, error) {
	for _, d := range r.byID {
		if d.MAC == mac {
			return d, nil
		}
	}
	return nil, errors.New("not found")
}

func (r *memBLURegistry) UpdateSensorValue(context.Context, string, string, string) error {
	return nil
}

func (r *memBLURegistry) RenameDevice(_ context.Context, oldID, newID string) error {
	if d, ok := r.byID[oldID]; ok {
		delete(r.byID, oldID)
		d.Id_ = newID
		r.byID[newID] = d
	}
	return nil
}

func (r *memBLURegistry) feed(t *testing.T, data BLUEventData) string {
	t.Helper()
	id, _, err := handleBLUEvent(context.Background(), logr.Discard(), "shelly-blu/events/x", buildBLUPayload(t, data), r)
	if err != nil {
		t.Fatalf("handleBLUEvent: %v", err)
	}
	return id
}

const testHTMAC = "7c:c6:b6:7f:48:ed"

// TestHandleBLUEvent_HTBatteryThenTemperature is the #786/#772 scenario: a BLU
// H&T sends battery-only advertisements next to temperature+humidity ones. The
// battery-only one must not create a generic "shellyblu-<mac>" twin, must not
// wipe the stored capabilities, and the device must be a thermometer.
func TestHandleBLUEvent_HTBatteryThenTemperature(t *testing.T) {
	bat, temp, hum := 90, 21.5, 55.0
	reg := newMemBLURegistry()

	reg.feed(t, BLUEventData{Address: testHTMAC, Battery: &bat}) // generic record, battery only
	id := reg.feed(t, BLUEventData{Address: testHTMAC, Temperature: &temp, Humidity: &hum})
	if id != "shellybluht3-7cc6b67f48ed" {
		t.Fatalf("temperature event id = %q", id)
	}
	if got := len(reg.byID); got != 1 {
		t.Fatalf("expected one record after upgrade, got %d: %v", got, reg.byID)
	}
	d := reg.byID[id]
	d.WithName("temperature-salon").RoomId = "sejour" // user assignments

	id2 := reg.feed(t, BLUEventData{Address: testHTMAC, Battery: &bat}) // later battery-only packet
	if id2 != id {
		t.Errorf("battery-only event resolved to %q, want the typed %q", id2, id)
	}
	if got := len(reg.byID); got != 1 {
		t.Fatalf("battery-only event created a twin: %v", reg.byID)
	}
	d = reg.byID[id]
	if d.Name() != "temperature-salon" || d.RoomId != "sejour" {
		t.Errorf("name/room lost: %q %q", d.Name(), d.RoomId)
	}
	if !myhome.IsTemperatureSensor(d) {
		t.Errorf("not a thermometer, caps = %v", d.Info.BTHome.Capabilities)
	}
}

// TestHandleBLUEvent_StoredBatteryOnlyConverges: a device stored with
// ["battery"] under its typed id is recognised once it reports a temperature.
func TestHandleBLUEvent_StoredBatteryOnlyConverges(t *testing.T) {
	mac, _ := net.ParseMAC(testHTMAC)
	stored := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellybluht3-7cc6b67f48ed").WithMAC(mac)
	stored.Info = &shelly.DeviceInfo{Product: shelly.Product{Model: "shellyblu"}}
	stored.Info.BTHome = &shelly.BTHomeInfo{Capabilities: []string{"battery"}}
	reg := newMemBLURegistry(stored)
	if myhome.IsTemperatureSensor(stored) {
		t.Fatal("precondition: battery-only must not be a thermometer")
	}

	temp := 20.0
	reg.feed(t, BLUEventData{Address: testHTMAC, Temperature: &temp})
	if !myhome.IsTemperatureSensor(reg.byID["shellybluht3-7cc6b67f48ed"]) {
		t.Errorf("caps did not converge: %v", stored.Info.BTHome.Capabilities)
	}
}

// TestHandleBLUEvent_PreexistingTwinDoesNotShadow: with a typed record and a
// generic twin already present (#772), events update the typed one.
func TestHandleBLUEvent_PreexistingTwinDoesNotShadow(t *testing.T) {
	mac, _ := net.ParseMAC("7c:c6:b6:9f:20:74")
	typed := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellybludoorwindow2-7cc6b69f2074").WithMAC(mac).WithName("porte-cuisine-couloir")
	typed.RoomId = "cuisine"
	generic := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyblu-7cc6b69f2074")
	reg := newMemBLURegistry(typed, generic)

	bat := 70
	id := reg.feed(t, BLUEventData{Address: "7c:c6:b6:9f:20:74", Battery: &bat})
	if id != typed.Id() {
		t.Errorf("event resolved to %q, want %q", id, typed.Id())
	}
	if typed.Info == nil || typed.Info.BTHome == nil {
		t.Error("typed record was not updated")
	}
}
