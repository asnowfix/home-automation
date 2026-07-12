package impl

import (
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
)

func newTestDevice(id, name string) *myhome.Device {
	d := &myhome.Device{}
	d.Id_ = id
	d.Name_ = name
	return d
}

func TestClassifyThermometers(t *testing.T) {
	t.Run("no devices", func(t *testing.T) {
		got := classifyThermometers(nil)
		if len(got) != 0 {
			t.Errorf("expected no thermometers, got %+v", got)
		}
	})

	t.Run("gen1 by id prefix", func(t *testing.T) {
		d := newTestDevice("shellyht-208500", "Bedroom Sensor")
		got := classifyThermometers([]*myhome.Device{d})
		if len(got) != 1 {
			t.Fatalf("expected 1 thermometer, got %+v", got)
		}
		if got[0].Type != "Gen1" || got[0].MqttTopic != "shellies/shellyht-208500/sensor/temperature" {
			t.Errorf("unexpected result: %+v", got[0])
		}
	})

	t.Run("gen1 by Info.Application", func(t *testing.T) {
		d := newTestDevice("some-other-id", "Kitchen Sensor")
		d.Info = &shelly.DeviceInfo{Product: shelly.Product{Application: "shellyht"}}
		got := classifyThermometers([]*myhome.Device{d})
		if len(got) != 1 || got[0].Type != "Gen1" {
			t.Fatalf("expected 1 Gen1 thermometer, got %+v", got)
		}
	})

	t.Run("blu device with temperature capability", func(t *testing.T) {
		d := newTestDevice("blu-abc123", "BLU Sensor")
		d.MAC = "AA:BB:CC:DD:EE:FF"
		d.Info = &shelly.DeviceInfo{BTHome: &shelly.BTHomeInfo{Capabilities: []string{"battery", "temperature"}}}
		got := classifyThermometers([]*myhome.Device{d})
		if len(got) != 1 {
			t.Fatalf("expected 1 thermometer, got %+v", got)
		}
		if got[0].Type != "BLU" || got[0].MqttTopic != "shelly-blu/events/AA:BB:CC:DD:EE:FF" {
			t.Errorf("unexpected result: %+v", got[0])
		}
	})

	t.Run("blu device without temperature capability is excluded", func(t *testing.T) {
		d := newTestDevice("blu-abc123", "BLU Sensor")
		d.Info = &shelly.DeviceInfo{BTHome: &shelly.BTHomeInfo{Capabilities: []string{"window"}}}
		got := classifyThermometers([]*myhome.Device{d})
		if len(got) != 0 {
			t.Errorf("expected no thermometers, got %+v", got)
		}
	})

	t.Run("plain switch device is excluded", func(t *testing.T) {
		d := newTestDevice("shellyplus1-abc", "Living Room Switch")
		got := classifyThermometers([]*myhome.Device{d})
		if len(got) != 0 {
			t.Errorf("expected no thermometers, got %+v", got)
		}
	})

	t.Run("room id is carried through", func(t *testing.T) {
		d := newTestDevice("shellyht-1", "Sensor")
		d.RoomId = "bedroom"
		got := classifyThermometers([]*myhome.Device{d})
		if len(got) != 1 || got[0].RoomId != "bedroom" {
			t.Fatalf("expected RoomId 'bedroom', got %+v", got)
		}
	})
}

func TestClassifyDoors(t *testing.T) {
	t.Run("no devices", func(t *testing.T) {
		got := classifyDoors(nil)
		if len(got) != 0 {
			t.Errorf("expected no doors, got %+v", got)
		}
	})

	t.Run("blu device with window capability", func(t *testing.T) {
		d := newTestDevice("blu-window1", "Front Door")
		d.MAC = "11:22:33:44:55:66"
		d.Info = &shelly.DeviceInfo{BTHome: &shelly.BTHomeInfo{Capabilities: []string{"window"}}}
		got := classifyDoors([]*myhome.Device{d})
		if len(got) != 1 {
			t.Fatalf("expected 1 door, got %+v", got)
		}
		if got[0].Type != "BLU" || got[0].MqttTopic != "shelly-blu/events/11:22:33:44:55:66" {
			t.Errorf("unexpected result: %+v", got[0])
		}
	})

	t.Run("blu device without window capability is excluded", func(t *testing.T) {
		d := newTestDevice("blu-temp1", "Temp Sensor")
		d.Info = &shelly.DeviceInfo{BTHome: &shelly.BTHomeInfo{Capabilities: []string{"temperature"}}}
		got := classifyDoors([]*myhome.Device{d})
		if len(got) != 0 {
			t.Errorf("expected no doors, got %+v", got)
		}
	})

	t.Run("gen1 thermometer-like id is excluded", func(t *testing.T) {
		d := newTestDevice("shellyht-208500", "Bedroom Sensor")
		got := classifyDoors([]*myhome.Device{d})
		if len(got) != 0 {
			t.Errorf("expected no doors (door.list only recognizes BLU window sensors), got %+v", got)
		}
	})
}
