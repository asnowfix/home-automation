package impl

import (
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	shellyshelly "github.com/asnowfix/home-automation/pkg/shelly/shelly"
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
