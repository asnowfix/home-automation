package sswitch

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/go-logr/logr"
)

// fakeProvider is a test double for DeviceProvider.
type fakeProvider struct {
	device    *myhome.Device
	deviceErr error
	sd        *shelly.Device
	sdErr     error
}

func (f *fakeProvider) GetDeviceByAny(ctx context.Context, identifier string) (*myhome.Device, error) {
	return f.device, f.deviceErr
}

func (f *fakeProvider) GetShellyDevice(ctx context.Context, device *myhome.Device) (*shelly.Device, error) {
	return f.sd, f.sdErr
}

func TestGetDevice_DeviceNotFound(t *testing.T) {
	wantErr := errors.New("no such device")
	s := NewService(logr.Discard(), &fakeProvider{deviceErr: wantErr})

	_, _, err := s.getDevice(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped %v, got %v", wantErr, err)
	}
}

func TestGetDevice_ShellyDeviceError(t *testing.T) {
	wantErr := errors.New("no transport")
	s := NewService(logr.Discard(), &fakeProvider{
		device: &myhome.Device{},
		sdErr:  wantErr,
	})

	_, _, err := s.getDevice(context.Background(), "some-device")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped %v, got %v", wantErr, err)
	}
}

func TestParseOn(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		switchId int
		on, ok   bool
	}{
		{"bool true: normally closed", "true", 0, false, true},
		{"bool false", "false", 0, true, true},
		{"garbage for switch 0 is unknown", "garbage", 0, false, false},
		{"array index 0: normally closed", `[true,false]`, 0, false, true},
		{"array index 1", `[true,false]`, 1, true, true},
		{"array index 2", `[false,false,true]`, 2, false, true},
		{"out of range is unknown, not a panic", `[true]`, 3, false, false},
		{"bool for switch 1 is unknown", "true", 1, false, false},
		{"garbage for switch 2 is unknown", "garbage", 2, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			on, ok := parseOn(tt.value, tt.switchId)
			if on != tt.on || ok != tt.ok {
				t.Errorf("parseOn(%q, %d) = %v,%v want %v,%v", tt.value, tt.switchId, on, ok, tt.on, tt.ok)
			}
		})
	}
}

// Issue #809: the registered handlers must accept both a value and a pointer
// (in-process callers vs RPC decoding) and reject anything else with an error,
// never a panic. The provider fails the lookup, so reaching that error proves
// the parameters were accepted.
func TestRegisteredHandlers_ParamsValueOrPointer(t *testing.T) {
	lookupErr := errors.New("no such device")
	NewService(logr.Discard(), &fakeProvider{deviceErr: lookupErr}).RegisterHandlers()
	verbs := []myhome.Verb{myhome.SwitchToggle, myhome.SwitchOn, myhome.SwitchOff, myhome.SwitchStatus, myhome.SwitchAll}
	t.Cleanup(func() {
		for _, v := range verbs {
			myhome.UnregisterMethodHandler(v)
		}
	})

	for _, v := range verbs {
		var value, ptr any = myhome.SwitchParams{Identifier: "x"}, &myhome.SwitchParams{Identifier: "x"}
		if v == myhome.SwitchAll {
			value, ptr = myhome.SwitchAllParams{Identifier: "x"}, &myhome.SwitchAllParams{Identifier: "x"}
		}
		var nilPtr any = (*myhome.SwitchParams)(nil)
		cases := []struct {
			name   string
			in     any
			accept bool
		}{
			{"value", value, true},
			{"pointer", ptr, true},
			{"nil pointer", nilPtr, false},
			{"nil", nil, false},
			{"wrong type", "x", false},
		}
		for _, c := range cases {
			t.Run(string(v)+"/"+c.name, func(t *testing.T) {
				m, err := myhome.Methods(v)
				if err != nil {
					t.Fatal(err)
				}
				_, err = m.ActionE(context.Background(), c.in)
				if err == nil {
					t.Fatal("expected an error from the failing provider")
				}
				if got := errors.Is(err, lookupErr); got != c.accept {
					t.Errorf("params accepted = %v, want %v (err: %v)", got, c.accept, err)
				}
			})
		}
	}
}
