package shelly

import (
	"context"
	"net"
	"testing"

	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// stubDevice is a minimal devices.Device for tests.
type stubDevice struct {
	id   string
	name string
	host string
}

func (s stubDevice) Manufacturer() string      { return "shelly" }
func (s stubDevice) Id() string                { return s.id }
func (s stubDevice) Name() string              { return s.name }
func (s stubDevice) Host() string              { return s.host }
func (s stubDevice) Ip() net.IP               { return net.ParseIP(s.host) }
func (s stubDevice) Mac() net.HardwareAddr    { return nil }

func TestNewDeviceFromSummary_EmptyId_ReturnsError(t *testing.T) {
	_, err := NewDeviceFromSummary(context.Background(), logr.Discard(), stubDevice{id: "", name: "unknown", host: "192.168.1.99"})
	if err == nil {
		t.Fatal("expected error for empty device ID, got nil")
	}
}

func TestNewDeviceFromSummary_ValidId_Succeeds(t *testing.T) {
	d, err := NewDeviceFromSummary(context.Background(), logr.Discard(), stubDevice{id: "shellyplus1pm-aabbccddeeff", name: "test", host: "192.168.1.50"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Id() != "shellyplus1pm-aabbccddeeff" {
		t.Errorf("expected id shellyplus1pm-aabbccddeeff, got %q", d.Id())
	}
}

func TestForeach_SkipsEmptyIdDevice(t *testing.T) {
	called := false
	do := func(ctx context.Context, log logr.Logger, via types.Channel, dev devices.Device, args []string) (any, error) {
		called = true
		return nil, nil
	}

	deviceList := []devices.Device{
		stubDevice{id: "", name: "partial", host: "192.168.1.10"},
	}

	_, err := Foreach(context.Background(), logr.Discard(), deviceList, types.ChannelDefault, do, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("do function should not be called for a device with empty ID")
	}
}

func TestForeach_SkipsGen1Device(t *testing.T) {
	called := false
	do := func(ctx context.Context, log logr.Logger, via types.Channel, dev devices.Device, args []string) (any, error) {
		called = true
		return nil, nil
	}

	deviceList := []devices.Device{
		stubDevice{id: "shellyht-aabbccddeeff"},
	}

	_, err := Foreach(context.Background(), logr.Discard(), deviceList, types.ChannelDefault, do, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("do function should not be called for a Gen1 device")
	}
}

func TestForeach_SkipsBluDevice(t *testing.T) {
	called := false
	do := func(ctx context.Context, log logr.Logger, via types.Channel, dev devices.Device, args []string) (any, error) {
		called = true
		return nil, nil
	}

	deviceList := []devices.Device{
		stubDevice{id: "shellybluht3-aabbccddeeff"},
	}

	_, err := Foreach(context.Background(), logr.Discard(), deviceList, types.ChannelDefault, do, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("do function should not be called for a BLU device")
	}
}

func TestForeach_EmptyList(t *testing.T) {
	do := func(ctx context.Context, log logr.Logger, via types.Channel, dev devices.Device, args []string) (any, error) {
		return nil, nil
	}
	_, err := Foreach(context.Background(), logr.Discard(), nil, types.ChannelDefault, do, nil)
	if err != nil {
		t.Fatalf("unexpected error on empty device list: %v", err)
	}
}

func TestIsGen1Device(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"shellyht-aabbccddeeff", true},
		{"shellyflood-aabbccddeeff", true},
		{"shelly1-aabbccddeeff", true},
		{"shelly1pm-aabbccddeeff", true},
		{"shelly25-aabbccddeeff", true},
		{"shellyplus1pm-aabbccddeeff", false},
		{"shellyplug-aabbccddeeff", true},
		{"", false},
	}
	for _, c := range cases {
		got := IsGen1Device(c.id)
		if got != c.want {
			t.Errorf("IsGen1Device(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestIsBluDevice(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"shellyblu-aabbccddeeff", true},
		{"shellybluht3-aabbccddeeff", true},
		{"shellybludoorwindow2-aabbccddeeff", true},
		{"shellyblumotion1-aabbccddeeff", true},
		{"shellyblubutton1-aabbccddeeff", true},
		{"sbht-aabbccddeeff", true},
		{"shellyplus1pm-aabbccddeeff", false},
		{"", false},
	}
	for _, c := range cases {
		got := IsBluDevice(c.id)
		if got != c.want {
			t.Errorf("IsBluDevice(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestMacFromShellyID(t *testing.T) {
	mac := MacFromShellyID("shellyplus1pm-aabbccddeeff")
	if mac == nil {
		t.Fatal("expected non-nil MAC for valid device ID")
	}

	if MacFromShellyID("nohyphen") != nil {
		t.Error("expected nil MAC for ID with no hyphen")
	}
	if MacFromShellyID("shellyplus1pm-short") != nil {
		t.Error("expected nil MAC for ID with short suffix")
	}
	if MacFromShellyID("shellyplus1pm-zzzzzzzzzzzz") != nil {
		t.Error("expected nil MAC for ID with non-hex suffix")
	}
}

func TestNewDeviceFromMqttId_Empty(t *testing.T) {
	_, err := NewDeviceFromMqttId(context.Background(), logr.Discard(), "")
	if err == nil {
		t.Fatal("expected error for empty MQTT device ID")
	}
}

func TestNewDeviceFromMqttId_NilString(t *testing.T) {
	_, err := NewDeviceFromMqttId(context.Background(), logr.Discard(), "<nil>")
	if err == nil {
		t.Fatal("expected error for <nil> MQTT device ID")
	}
}

func TestNewDeviceFromMqttId_Valid(t *testing.T) {
	d, err := NewDeviceFromMqttId(context.Background(), logr.Discard(), "shellyplus1pm-aabbccddeeff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Id() != "shellyplus1pm-aabbccddeeff" {
		t.Errorf("unexpected id: %q", d.Id())
	}
}
