package shelly

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// fakeMqttClient lets Device.init succeed without a broker: its channels never
// carry traffic, which is fine because the probe is replaced by a seam.
type fakeMqttClient struct{}

func (fakeMqttClient) GetServer() string  { return "fake" }
func (fakeMqttClient) BrokerUrl() *url.URL { return &url.URL{} }
func (fakeMqttClient) Id() string         { return "fake" }
func (fakeMqttClient) Subscribe(context.Context, string, uint, string) (<-chan []byte, error) {
	return make(chan []byte), nil
}
func (fakeMqttClient) SubscribeWithHandler(context.Context, string, uint, string, func(string, []byte, string) error) error {
	return nil
}
func (fakeMqttClient) Publisher(context.Context, string, uint, byte, bool, string) (chan<- []byte, error) {
	return make(chan []byte, 1), nil
}
func (fakeMqttClient) Publish(context.Context, string, []byte, byte, bool, string) error { return nil }

// macDevice has a MAC so that Device.init does not call the device to learn it.
type macDevice struct{ stubDevice }

func (macDevice) Mac() net.HardwareAddr { return net.HardwareAddr{0xa0, 0xdd, 0x6c, 0xa1, 0xc5, 0x88} }

// Device ids are unique to this file: deviceMqttRegistry is process-wide, so a
// shared id would leak channels between tests.
func fe(suffix string) macDevice {
	return macDevice{stubDevice{id: "shellyplus1-feed0000" + suffix, name: "dev-" + suffix, host: "192.168.1.20"}}
}

func feCtx() context.Context {
	return mqtt.NewContextWithClient(context.Background(), fakeMqttClient{})
}

var errTimeout = fmt.Errorf("probe: %w", context.DeadlineExceeded)

// probeBy answers per device id: nil = reachable, otherwise the error.
func probeBy(m map[string]error) func(context.Context, logr.Logger, types.Channel, devices.Device) error {
	return func(_ context.Context, _ logr.Logger, _ types.Channel, d devices.Device) error {
		return m[d.Id()]
	}
}

type recorder struct {
	mu   sync.Mutex
	seen []string
	fail map[string]error
}

func (r *recorder) do(_ context.Context, _ logr.Logger, _ types.Channel, d devices.Device, _ []string) (any, error) {
	r.mu.Lock()
	r.seen = append(r.seen, d.Id())
	r.mu.Unlock()
	return nil, r.fail[d.Id()]
}

func TestForeach_UnreachableSkippedAndExitCodeZero(t *testing.T) {
	up, down := fe("0001"), fe("0002")
	r := &recorder{}
	_, err := ForeachWith(feCtx(), logr.Discard(), []devices.Device{up, down}, types.ChannelDefault, r.do, nil,
		ForeachOptions{Probe: probeBy(map[string]error{down.Id(): errTimeout})})
	if err != nil {
		t.Fatalf("an unreachable device must not fail the run, got %v", err)
	}
	if len(r.seen) != 1 || r.seen[0] != up.Id() {
		t.Errorf("only the reachable device must run, got %v", r.seen)
	}
}

func TestForeach_RealFailureOnReachableDeviceStillFails(t *testing.T) {
	up, bad, down := fe("0011"), fe("0012"), fe("0013")
	r := &recorder{fail: map[string]error{bad.Id(): errors.New("upload refused")}}
	_, err := ForeachWith(feCtx(), logr.Discard(), []devices.Device{up, bad, down}, types.ChannelDefault, r.do, nil,
		ForeachOptions{Probe: probeBy(map[string]error{down.Id(): errTimeout})})
	if err == nil {
		t.Fatal("a real failure on a reachable device must give a non-nil error")
	}
	// 1 failure out of 3 listed devices (the unreachable one is not a failure).
	if !strings.Contains(err.Error(), "1 of 3 device(s) failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestForeach_AllUnreachableIsAnError(t *testing.T) {
	down := fe("0021")
	r := &recorder{}
	_, err := ForeachWith(feCtx(), logr.Discard(), []devices.Device{down}, types.ChannelDefault, r.do, nil,
		ForeachOptions{Probe: probeBy(map[string]error{down.Id(): errTimeout})})
	if !errors.Is(err, ErrNoReachableDevice) {
		t.Fatalf("nothing reachable must be ErrNoReachableDevice, got %v", err)
	}
}

// The flap case: a device the registry (or the events) may show as offline but
// that answers the probe now must be processed, not skipped.
func TestForeach_FlappedDeviceThatAnswersIsNotSkipped(t *testing.T) {
	flapped := fe("0031")
	r := &recorder{}
	_, err := ForeachWith(feCtx(), logr.Discard(), []devices.Device{flapped}, types.ChannelDefault, r.do, nil,
		ForeachOptions{Probe: probeBy(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.seen) != 1 {
		t.Errorf("a device that answers the probe must run, got %v", r.seen)
	}
}

// A probe that fails for a reason other than silence (local error, device RPC
// error) is inconclusive: the device is processed and reports its own result.
func TestForeach_NonTimeoutProbeErrorDoesNotSkip(t *testing.T) {
	d := fe("0041")
	r := &recorder{}
	_, err := ForeachWith(feCtx(), logr.Discard(), []devices.Device{d}, types.ChannelDefault, r.do, nil,
		ForeachOptions{Probe: probeBy(map[string]error{d.Id(): errors.New("some local error")})})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.seen) != 1 {
		t.Errorf("device must still run, got %v", r.seen)
	}
}

func TestForeach_NoProbeKeepsLegacyBehaviour(t *testing.T) {
	d := fe("0051")
	r := &recorder{fail: map[string]error{d.Id(): errTimeout}}
	_, err := Foreach(feCtx(), logr.Discard(), []devices.Device{d}, types.ChannelDefault, r.do, nil)
	if err == nil || !strings.Contains(err.Error(), "all 1 device(s) failed") {
		t.Errorf("without a probe a timeout is a plain failure, got %v", err)
	}
}

func TestForeach_ProbeSkippedForGen1AndBlu(t *testing.T) {
	probed := false
	p := func(context.Context, logr.Logger, types.Channel, devices.Device) error { probed = true; return errTimeout }
	list := []devices.Device{stubDevice{id: "shellyht-aabbccddeeff"}, stubDevice{id: "shellybluht3-aabbccddeeff"}}
	if _, err := ForeachWith(feCtx(), logr.Discard(), list, types.ChannelDefault, (&recorder{}).do, nil, ForeachOptions{Probe: p}); err != nil {
		t.Fatal(err)
	}
	if probed {
		t.Error("Gen1 and BLU devices are skipped before any probe")
	}
}

func TestWriteForeachSummary_ListsUnreachableApartInStableOrder(t *testing.T) {
	var b bytes.Buffer
	failed := []DeviceResult{{Device: fe("0061"), Error: errors.New("boom")}}
	unreach := []DeviceResult{{Device: fe("0062")}, {Device: fe("0063")}}
	writeForeachSummary(&b, 5, failed, unreach)
	out := b.String()
	for _, want := range []string{
		"✓ 5 device(s) succeeded",
		"✗ 1 device(s) failed:\n  - dev-0061 (shellyplus1-feed00000061): boom",
		"⚠ 2 device(s) unreachable (skipped, not counted as failures):\n  - dev-0062 (shellyplus1-feed00000062)\n  - dev-0063 (shellyplus1-feed00000063)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "failed:") > strings.Index(out, "unreachable") {
		t.Error("failures must be listed before unreachable devices")
	}
	b.Reset()
	writeForeachSummary(&b, 3, nil, nil)
	if b.Len() != 0 {
		t.Errorf("a clean run prints no summary, got %q", b.String())
	}
}

func TestIsUnreachable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{context.DeadlineExceeded, true},
		{fmt.Errorf("wrapped: %w", context.DeadlineExceeded), true},
		{errors.New("timeout waiting for response from shellypro3-a0dd6ca1c588 (x)"), true},
		{&net.DNSError{IsTimeout: true}, true},
		{context.Canceled, false},
		{&types.RpcError{Code: 404, Message: "no such method"}, false},
		{errors.New("MQTT client not initialized"), false},
	} {
		if got := IsUnreachable(tc.err); got != tc.want {
			t.Errorf("IsUnreachable(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// Cancelled by the caller (ctrl-C) while probing: not the device's fault.
func TestForeach_CancelledContextIsNotUnreachable(t *testing.T) {
	d := fe("0071")
	ctx, cancel := context.WithCancel(feCtx())
	cancel()
	r := &recorder{}
	_, err := ForeachWith(ctx, logr.Discard(), []devices.Device{d}, types.ChannelDefault, r.do, nil,
		ForeachOptions{Probe: probeBy(map[string]error{d.Id(): context.DeadlineExceeded}), ProbeTimeout: time.Second})
	if errors.Is(err, ErrNoReachableDevice) {
		t.Errorf("cancellation must not be reported as unreachable: %v", err)
	}
}
