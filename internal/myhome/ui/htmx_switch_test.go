package ui

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/myhome/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly"
	pkgsswitch "github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// switchProvider is the sswitch.DeviceProvider the real switch handlers use.
type switchProvider struct {
	dev *shelly.Device
	err error
}

func (p *switchProvider) GetDeviceByAny(ctx context.Context, id string) (*myhome.Device, error) {
	if p.err != nil {
		return nil, p.err
	}
	return myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyplus1-aabbccddeeff"), nil
}

func (p *switchProvider) GetShellyDevice(ctx context.Context, d *myhome.Device) (*shelly.Device, error) {
	return p.dev, nil
}

// registerRealSwitchHandlers registers the real sswitch handlers on top of a
// fake HTTP device caller, and returns the Switch.Toggle call count. It
// restores the shared registries on cleanup (no t.Parallel in callers).
func registerRealSwitchHandlers(t *testing.T, p *switchProvider) *int {
	t.Helper()
	reg := shelly.GetRegistrar()
	reg.Init(logr.Discard())
	pkgsswitch.Init(logr.Discard(), reg)
	var mu sync.Mutex
	toggles := 0
	reg.RegisterDeviceCaller(types.ChannelHttp, func(ctx context.Context, d types.Device, mh types.MethodHandler, out any, params any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if mh.Method == "Switch.Toggle" {
			toggles++
			return &pkgsswitch.ToogleSetResponse{WasOn: true}, nil
		}
		return nil, http.ErrNotSupported // KVS polarity lookup etc.: default polarity
	})
	p.dev = &shelly.Device{Id_: "shellyplus1-aabbccddeeff", Host_: net.ParseIP("192.168.1.50")}

	sswitch.NewService(logr.Discard(), p).RegisterHandlers()
	t.Cleanup(func() {
		for _, v := range []myhome.Verb{myhome.SwitchToggle, myhome.SwitchOn, myhome.SwitchOff, myhome.SwitchStatus, myhome.SwitchAll} {
			myhome.UnregisterMethodHandler(v)
		}
		reg.Init(logr.Discard())
	})
	return &toggles
}

// Issue #809: the UI passed a SwitchParams value to handlers that asserted a
// pointer, so a click panicked and switched nothing.
func TestSwitchButton_TogglesThroughRealHandlers(t *testing.T) {
	toggles := registerRealSwitchHandlers(t, &switchProvider{})
	h := NewHTMXHandler(context.Background(), logr.Discard(), nil, nil, nil, nil)

	rec := postToggle(http.HandlerFunc(h.SwitchButton), url.Values{"device_id": {"lamp"}, "switch_id": {"0"}, "label": {"Lamp"}})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if *toggles != 1 {
		t.Errorf("Switch.Toggle reached the device %d times, want 1", *toggles)
	}
}

// A failed toggle answers 500 (which htmx turns into the "couldn't toggle"
// pill, see kiosk.failed in static/myhome.js), never a panic or a 200.
func TestSwitchButton_FailedToggleIs500(t *testing.T) {
	toggles := registerRealSwitchHandlers(t, &switchProvider{err: errors.New("device not found")})
	h := NewHTMXHandler(context.Background(), logr.Discard(), nil, nil, nil, nil)

	rec := postToggle(http.HandlerFunc(h.SwitchButton), url.Values{"device_id": {"lamp"}, "switch_id": {"0"}})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if *toggles != 0 {
		t.Errorf("device was toggled %d times despite the lookup failure", *toggles)
	}
}
