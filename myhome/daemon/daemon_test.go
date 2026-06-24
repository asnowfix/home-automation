package daemon

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly"
	shellycloud "github.com/asnowfix/home-automation/pkg/shelly/cloud"
	shellyshelly "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	shellytypes "github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// initShellyRegistrarOnce registers the Cloud.GetStatus and Shelly.DetectLocation
// method handlers on the pkg/shelly package-level registrar singleton exactly
// once per test binary; RegisterMethodHandler panics on duplicate registration.
var initShellyRegistrarOnce sync.Once

func initShellyRegistrar() {
	initShellyRegistrarOnce.Do(func() {
		reg := shelly.GetRegistrar()
		reg.Init(logr.Discard())
		shellycloud.Init(logr.Discard(), reg)
		shellyshelly.Init(logr.Discard(), reg, 0)
	})
}

// fakeDeviceCaller dispatches by method name, used to stand in for the real
// HTTP device caller without making any network calls.
func fakeDeviceCaller(responses map[string]func() (any, error)) shellytypes.DeviceCaller {
	return func(_ context.Context, _ shellytypes.Device, mh shellytypes.MethodHandler, _ any, _ any) (any, error) {
		fn, ok := responses[mh.Method]
		if !ok {
			return nil, fmt.Errorf("unexpected method call: %s", mh.Method)
		}
		return fn()
	}
}

func TestShellyLocation(t *testing.T) {
	initShellyRegistrar()
	reg := shelly.GetRegistrar()

	sd := &shelly.Device{Id_: "shellyplus1-aabbccddeeff", Host_: net.ParseIP("127.0.0.1")}

	tests := []struct {
		name      string
		responses map[string]func() (any, error)
		wantLat   float64
		wantLon   float64
		wantErr   string
	}{
		{
			name: "cloud GetStatus error",
			responses: map[string]func() (any, error){
				shellycloud.GetStatus.String(): func() (any, error) {
					return nil, fmt.Errorf("connection refused")
				},
			},
			wantErr: "Cloud.GetStatus",
		},
		{
			name: "not cloud-connected",
			responses: map[string]func() (any, error){
				shellycloud.GetStatus.String(): func() (any, error) {
					return &shellycloud.Status{Connected: false}, nil
				},
			},
			wantErr: "not cloud-connected",
		},
		{
			name: "DetectLocation error",
			responses: map[string]func() (any, error){
				shellycloud.GetStatus.String(): func() (any, error) {
					return &shellycloud.Status{Connected: true}, nil
				},
				shellyshelly.DetectLocation.String(): func() (any, error) {
					return nil, fmt.Errorf("timeout")
				},
			},
			wantErr: "Shelly.DetectLocation",
		},
		{
			name: "empty location response",
			responses: map[string]func() (any, error){
				shellycloud.GetStatus.String(): func() (any, error) {
					return &shellycloud.Status{Connected: true}, nil
				},
				shellyshelly.DetectLocation.String(): func() (any, error) {
					return &shellyshelly.DetectLocationResponse{Lat: 0, Lon: 0}, nil
				},
			},
			wantErr: "empty location response",
		},
		{
			name: "success",
			responses: map[string]func() (any, error){
				shellycloud.GetStatus.String(): func() (any, error) {
					return &shellycloud.Status{Connected: true}, nil
				},
				shellyshelly.DetectLocation.String(): func() (any, error) {
					return &shellyshelly.DetectLocationResponse{Lat: 48.8566, Lon: 2.3522}, nil
				},
			},
			wantLat: 48.8566,
			wantLon: 2.3522,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg.RegisterDeviceCaller(shellytypes.ChannelHttp, fakeDeviceCaller(tt.responses))

			lat, lon, err := shellyLocation(context.Background(), sd)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if got := err.Error(); !strings.Contains(got, tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lat != tt.wantLat || lon != tt.wantLon {
				t.Fatalf("got (%v, %v), want (%v, %v)", lat, lon, tt.wantLat, tt.wantLon)
			}
		})
	}
}
