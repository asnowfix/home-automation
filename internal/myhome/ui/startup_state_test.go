package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// TestDeviceRowStartingToKnown: a switch nobody has heard from renders
// "Starting…" (disabled, labelled) while the post-start phase is on, and the
// first status that reaches it replaces that with the real state.
func TestDeviceRowStartingToKnown(t *testing.T) {
	calls := unreachableDevices(t)
	const id = "shellyplus1-aabbccddee31"

	fake := types.NewFakeDevice()
	fake.IdValue = id
	fake.SetResult(string(kvs.Get), &kvs.GetResponse{Value: "false"})
	if _, err := kvs.GetValue(context.Background(), logr.Discard(), types.ChannelDefault, fake, "normally-closed"); err != nil {
		t.Fatal(err)
	}

	d := switchDevice(t, id, true)
	sd := d.Impl().(*shelly.Device)
	sd.SetStatus(nil) // never refreshed since start

	SetStarting(true)
	t.Cleanup(func() { SetStarting(false) })

	out := renderDevice(t, DeviceToView(renderCtx(), d))
	if !strings.Contains(out, "Starting…") {
		t.Errorf("unheard switch during startup: want Starting…, got\n%s", out)
	}
	if !strings.Contains(out, `class="mh-power is-unknown"`) || strings.Contains(out, `hx-post=`) {
		t.Errorf("Starting… must keep the disabled button, got\n%s", out)
	}

	// The first notification/refresh lands: the status is set.
	sd.SetStatus(&shellyapi.Status{Switch0: &sswitch.Status{Id: 0, Output: true}})
	out = renderDevice(t, DeviceToView(renderCtx(), d))
	if strings.Contains(out, "Starting…") {
		t.Errorf("known switch must not say Starting…, got\n%s", out)
	}
	if !strings.Contains(out, `class="mh-power is-on"`) || !strings.Contains(out, `hx-post="/htmx/switch/toggle"`) {
		t.Errorf("known switch: want an enabled on button, got\n%s", out)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("render made %d device call(s)", n)
	}
}

// TestDeviceRowStartingToUnknown: when the post-start phase ends and the
// device never reported, the same row degrades to plain unknown: no
// "Starting…", still disabled.
func TestDeviceRowStartingToUnknown(t *testing.T) {
	calls := unreachableDevices(t)
	d := switchDevice(t, "shellyplus1-aabbccddee32", true) // polarity never resolved: unknown

	SetStarting(true)
	t.Cleanup(func() { SetStarting(false) })
	if out := renderDevice(t, DeviceToView(renderCtx(), d)); !strings.Contains(out, "Starting…") {
		t.Fatalf("during startup: want Starting…, got\n%s", out)
	}

	SetStarting(false) // first refresh pass over
	out := renderDevice(t, DeviceToView(renderCtx(), d))
	if strings.Contains(out, "Starting…") {
		t.Errorf("after the phase: must not say Starting…, got\n%s", out)
	}
	if !strings.Contains(out, `class="mh-power is-unknown"`) || strings.Contains(out, `hx-post=`) {
		t.Errorf("after the phase: want a disabled unknown button, got\n%s", out)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("render made %d device call(s)", n)
	}
}
