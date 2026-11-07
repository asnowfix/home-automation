package ui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// unreachableDevices installs a registrar whose every device call is counted and
// then hangs for 3 s, like a device that does not answer (#732). It returns the counter.
func unreachableDevices(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	r := shelly.GetRegistrar()
	r.Init(logr.Discard())
	r.RegisterMethodHandler(string(kvs.Get), types.MethodHandler{Allocate: func() any { return new(kvs.GetResponse) }})
	r.RegisterDeviceCaller(types.ChannelDefault, func(ctx context.Context, _ types.Device, _ types.MethodHandler, _ any, _ any) (any, error) {
		calls.Add(1)
		time.Sleep(3 * time.Second)
		return nil, context.DeadlineExceeded
	})
	return &calls
}

func switchDevice(t *testing.T, id string, output bool) *myhome.Device {
	t.Helper()
	d := myhome.NewDevice(logr.Discard(), myhome.SHELLY, id).WithName(id)
	impl, err := shelly.NewDeviceFromSummary(context.Background(), logr.Discard(), d)
	if err != nil {
		t.Fatal(err)
	}
	sd := impl.(*shelly.Device)
	sd.SetStatus(&shellyapi.Status{Switch0: &sswitch.Status{Id: 0, Output: output}})
	d.WithImpl(sd)
	d.Config = &shellyapi.Config{Switch0: &sswitch.Config{Id: 0}}
	return d
}

func renderCtx() context.Context { return logr.NewContext(context.Background(), logr.Discard()) }

func TestRenderMakesNoDeviceCall(t *testing.T) {
	calls := unreachableDevices(t)

	// polarity resolved earlier by a refresh: normally-closed, so output=true means "off"
	fake := types.NewFakeDevice()
	fake.IdValue = "shellyplus1-aabbccddee01"
	fake.SetResult(string(kvs.Get), &kvs.GetResponse{Value: "true"})
	if _, err := kvs.GetValue(context.Background(), logr.Discard(), types.ChannelDefault, fake, "normally-closed"); err != nil {
		t.Fatal(err)
	}

	known := switchDevice(t, "shellyplus1-aabbccddee01", true) // cached polarity
	cold := switchDevice(t, "shellyplus1-aabbccddee02", true)  // never refreshed
	noCfg := switchDevice(t, "shellyplus1-aabbccddee03", true)
	noCfg.Config = nil

	start := time.Now()
	kv := DeviceToView(renderCtx(), known)
	cv := DeviceToView(renderCtx(), cold)
	nv := DeviceToView(renderCtx(), noCfg)
	elapsed := time.Since(start)

	if n := calls.Load(); n != 0 {
		t.Errorf("rendering made %d device call(s)", n)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("rendering three devices took %v with unreachable devices", elapsed)
	}
	t.Logf("rendered 3 devices with unreachable stubs in %v", elapsed)

	if sw, ok := kv.Switches[0]; !ok || sw.On || kv.SwitchUnknown[0] {
		t.Errorf("known normally-closed device: switch=%+v unknown=%v; want off and known", sw, kv.SwitchUnknown[0])
	}
	if _, ok := cv.Switches[0]; !ok || !cv.SwitchUnknown[0] {
		t.Errorf("cold cache: switches=%v unknown=%v; want the switch listed as unknown", cv.Switches, cv.SwitchUnknown)
	}
	if len(nv.Switches) != 0 {
		t.Errorf("device without config lists switches %v; want none", nv.Switches)
	}
}

func TestRenderEndpointsMakeNoDeviceCall(t *testing.T) {
	calls := unreachableDevices(t)
	devs := []*myhome.Device{switchDevice(t, "shellyplus1-aabbccddee04", true), switchDevice(t, "shellyplus1-aabbccddee05", false)}
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: devs}, nil, nil, "", nil, nil)

	start := time.Now()
	for _, p := range []string{"/htmx/devices", "/htmx/device/shellyplus1-aabbccddee04"} {
		if rec := get(h, p); rec.Code != 200 {
			t.Fatalf("%s = %d", p, rec.Code)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("render endpoints made %d device call(s)", n)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("render endpoints took %v with unreachable devices", d)
	} else {
		t.Logf("/htmx/devices + /htmx/device/{id} with unreachable devices: %v", d)
	}
}
