package impl

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	shellyshelly "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	mhd "github.com/asnowfix/home-automation/myhome/devices"
	"github.com/go-logr/logr"
)

func makeDevice(id string, caps []string) *myhome.Device {
	d := myhome.NewDevice(logr.Discard(), myhome.SHELLY, id)
	if len(caps) > 0 {
		d.Info = &shellyshelly.DeviceInfo{}
		d.Info.BTHome = &shellyshelly.BTHomeInfo{Capabilities: caps}
	}
	return d
}

func TestClassifyDevice(t *testing.T) {
	cases := []struct {
		id   string
		caps []string
		want deviceRole
	}{
		// Generic BLU fallback ID
		{"shellyblu-aabbccddeeff", []string{"window", "temperature", "battery"}, roleDoorSensor},
		{"shellyblu-aabbccddeeff", []string{"temperature", "humidity", "battery"}, roleTempSensor},
		{"shellyblu-aabbccddeeff", []string{"motion"}, roleMotionSensor},
		{"shellyblu-aabbccddeeff", []string{"battery"}, roleUnknown},
		{"shellyblu-aabbccddeeff", nil, roleUnknown},
		// Typed BLU device IDs
		{"shellybludoorwindow2-aabbccddeeff", []string{"window", "battery"}, roleDoorSensor},
		{"shellybluht3-aabbccddeeff", []string{"temperature", "humidity", "battery"}, roleTempSensor},
		{"shellyblumotion1-aabbccddeeff", []string{"motion", "battery"}, roleMotionSensor},
		// Gen1 H&T
		{"shellyht-abc123", nil, roleTempSensor},
		// Other Gen1 devices have no KVS: not heaters (#800)
		{"shellyflood-485519EE11F0", nil, roleUnknown},
		{"shellydw-aabbcc", nil, roleUnknown},
		// Heaters
		{"shellyplus1-aabbcc", nil, roleHeater},
		{"shellyplus2pm-xxyyzz", nil, roleHeater},
	}

	for _, c := range cases {
		d := makeDevice(c.id, c.caps)
		got := classifyDevice(d)
		if got != c.want {
			t.Errorf("classifyDevice(%q, caps=%v) = %v, want %v", c.id, c.caps, got, c.want)
		}
	}
}

func TestSensorMQTTTopic(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		// Generic BLU fallback
		{"shellyblu-aabbccddeeff", "shelly-blu/events/aa:bb:cc:dd:ee:ff"},
		// Typed BLU device IDs — same topic pattern, MAC extracted from suffix
		{"shellybludoorwindow2-aabbccddeeff", "shelly-blu/events/aa:bb:cc:dd:ee:ff"},
		{"shellybluht3-aabbccddeeff", "shelly-blu/events/aa:bb:cc:dd:ee:ff"},
		{"shellyblumotion1-aabbccddeeff", "shelly-blu/events/aa:bb:cc:dd:ee:ff"},
		// Gen1 H&T
		{"shellyht-abc123", "shellies/shellyht-abc123/sensor/temperature"},
		// Heaters have no sensor topic
		{"shellyplus1-aabb", ""},
	}

	for _, c := range cases {
		d := makeDevice(c.id, nil)
		got := sensorMQTTTopic(d)
		if got != c.want {
			t.Errorf("sensorMQTTTopic(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

// roomRegistry is a DeviceRegistry stub; only the two room-related methods are implemented.
type roomRegistry struct {
	mhd.DeviceRegistry
	devices []*myhome.Device
}

func (r roomRegistry) GetAllDevices(context.Context) ([]*myhome.Device, error) { return r.devices, nil }

func (r roomRegistry) GetDevicesByRoom(_ context.Context, room string) ([]*myhome.Device, error) {
	var out []*myhome.Device
	for _, d := range r.devices {
		if d.RoomId == room {
			out = append(out, d)
		}
	}
	return out, nil
}

// Regression for #800: a room holding a device with an invalid id must not panic
// SetupAllRooms; the bad device is skipped and the heater is still processed.
func TestSetupAllRooms_BadIdDeviceDoesNotPanic(t *testing.T) {
	flood := makeDevice("shellyflood-485519EE11F0", nil)      // Gen1: not a heater, never built
	badHeater := makeDevice("shellyplus1-AABBCCDDEEFF", nil) // heater-classed but invalid id
	heater := makeDevice("shellyplus1-aabbccddeeff", nil)
	for _, d := range []*myhome.Device{flood, badHeater, heater} {
		d.RoomId = "sdb"
	}
	dm := &DeviceManager{log: logr.Discard(), dr: roomRegistry{devices: []*myhome.Device{flood, badHeater, heater}}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res := dm.SetupAllRooms(ctx) // must not panic

	if res.RoomsProcessed != 1 {
		t.Fatalf("RoomsProcessed = %d, want 1", res.RoomsProcessed)
	}
	got := map[string]myhome.RoomSetupDeviceResult{}
	for _, dr := range res.Devices {
		got[dr.DeviceID] = dr
	}
	if _, ok := got[flood.Id()]; ok {
		t.Errorf("Gen1 flood sensor must not be set up: %+v", got[flood.Id()])
	}
	if bad, ok := got[badHeater.Id()]; !ok || bad.Error == "" {
		t.Errorf("invalid-id device must be reported with an error, got %+v (present=%v)", bad, ok)
	}
	// The valid heater was attempted (no host: its KVS writes fail, but it was not skipped).
	if _, ok := got[heater.Id()]; !ok {
		t.Errorf("valid heater was not processed; results: %+v", res.Devices)
	}
}
