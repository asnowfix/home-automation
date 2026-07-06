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

func TestParseOnValue(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"true", false},   // normally closed -> "on" is false
		{"false", true},   // not normally closed -> "on" is true
		{"garbage", true}, // ParseBool fails -> nc defaults false -> on true
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := parseOnValue(tt.value); got != tt.want {
				t.Errorf("parseOnValue(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseOnValueIndexed(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		switchId int
		want     bool
	}{
		{"index 0 of array: normally closed", `[true,false]`, 0, false},
		{"index 1 of array: not normally closed", `[true,false]`, 1, true},
		{"index 2 of longer array", `[false,false,true]`, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseOnValueIndexed(tt.value, tt.switchId); got != tt.want {
				t.Errorf("parseOnValueIndexed(%q, %d) = %v, want %v", tt.value, tt.switchId, got, tt.want)
			}
		})
	}
}
