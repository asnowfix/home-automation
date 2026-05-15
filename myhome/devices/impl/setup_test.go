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
		{"shellyblu-aabbccddeeff", []string{"window", "temperature", "battery"}, roleDoorSensor},
		{"shellyblu-aabbccddeeff", []string{"temperature", "humidity", "battery"}, roleTempSensor},
		{"shellyblu-aabbccddeeff", []string{"battery"}, roleUnknown},
		{"shellyblu-aabbccddeeff", nil, roleUnknown},
		{"shellyht-abc123", nil, roleTempSensor},
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
		{
			"shellyblu-aabbccddeeff",
			"shelly-blu/events/aa:bb:cc:dd:ee:ff",
		},
		{
			"shellyht-abc123",
			"shellies/shellyht-abc123/sensor/temperature",
		},
		{
			"shellyplus1-aabb",
			"", // heaters have no sensor topic
		},
	}

	for _, c := range cases {
		d := makeDevice(c.id, nil)
		got := sensorMQTTTopic(d)
		if got != c.want {
			t.Errorf("sensorMQTTTopic(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestMacWithColons(t *testing.T) {
	cases := []struct{ in, want string }{
		{"aabbccddeeff", "aa:bb:cc:dd:ee:ff"},
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"}, // already has colons
		{"short", "short"},                          // not 12 hex chars, returned as-is
	}
	for _, c := range cases {
		got := macWithColons(c.in)
		if got != c.want {
			t.Errorf("macWithColons(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
