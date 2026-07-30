package daemon

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

func TestRoundTo(t *testing.T) {
	cases := []struct {
		v      float64
		places int
		want   float64
	}{
		{3.14159, 2, 3.14},
		{3.145, 2, 3.15},
		{5, 2, 5},
		{-1.006, 2, -1.01},
		{0, 2, 0},
	}
	for _, c := range cases {
		if got := roundTo(c.v, c.places); got != c.want {
			t.Errorf("roundTo(%v, %d) = %v, want %v", c.v, c.places, got, c.want)
		}
	}
}

// TestPoolNoticesOnEventNilReceiver verifies that daemon.go can call
// poolNotices.OnEvent(...) unconditionally even when NewPoolNotices returned
// nil (pool device unreachable, or tracker/events disabled) — the broadcast
// hook has no nil check, so this must never panic.
func TestPoolNoticesOnEventNilReceiver(t *testing.T) {
	var p *PoolNotices
	p.OnEvent(context.Background(), events.Event{Event: "pool.pump_stop"})
}

// TestPoolNoticesOnEventIgnoresUnrelatedEvents verifies the event-name filter
// short-circuits before touching the (here nil) device handle, so unrelated
// events — including the device's own pool.pump_start — never attempt a KVS
// read.
func TestPoolNoticesOnEventIgnoresUnrelatedEvents(t *testing.T) {
	s := newTestEventsStorage(t)
	p := &PoolNotices{
		log:      logr.Discard(),
		events:   events.NewService(logr.Discard(), s, nil, nil, 0),
		device:   nil,
		deviceID: "pool-device",
	}

	for _, evName := range []string{"pool.pump_start", "pool.run_window", "switch.on", "pool.water_supply_protected"} {
		p.OnEvent(context.Background(), events.Event{Event: evName})
	}
}

// fakeKVSDevice is a minimal types.Device stub that answers KVS.Get calls
// from a canned map, so ComputeTurnover can be exercised without a live
// MQTT-connected device. Every method beyond CallE/Id is an unused no-op —
// ComputeTurnover (via readPoolKVSInt/readPoolKVSFloat -> kvs.GetValue) only
// calls those two.
type fakeKVSDevice struct {
	id  string
	kvs map[string]string
}

func (f *fakeKVSDevice) String() string                                               { return f.id }
func (f *fakeKVSDevice) Name() string                                                 { return f.id }
func (f *fakeKVSDevice) Host() string                                                 { return "" }
func (f *fakeKVSDevice) Manufacturer() string                                         { return "" }
func (f *fakeKVSDevice) Id() string                                                   { return f.id }
func (f *fakeKVSDevice) Mac() net.HardwareAddr                                        { return nil }
func (f *fakeKVSDevice) ReplyTo() string                                              { return "" }
func (f *fakeKVSDevice) To() chan<- []byte                                            { return nil }
func (f *fakeKVSDevice) From() <-chan []byte                                          { return nil }
func (f *fakeKVSDevice) StartDialog(ctx context.Context) uint32                       { return 0 }
func (f *fakeKVSDevice) StopDialog(ctx context.Context, id uint32)                    {}
func (f *fakeKVSDevice) IsHttpReady() bool                                            { return false }
func (f *fakeKVSDevice) IsMqttReady() bool                                            { return true }
func (f *fakeKVSDevice) Channel(ctx context.Context, via types.Channel) types.Channel { return via }
func (f *fakeKVSDevice) UpdateName(name string)                                       {}
func (f *fakeKVSDevice) UpdateHost(host string)                                       {}
func (f *fakeKVSDevice) ClearHost()                                                   {}
func (f *fakeKVSDevice) UpdateMac(mac string)                                         {}
func (f *fakeKVSDevice) UpdateId(id string)                                           {}
func (f *fakeKVSDevice) IsModified() bool                                             { return false }
func (f *fakeKVSDevice) ResetModified()                                               {}

func (f *fakeKVSDevice) CallE(ctx context.Context, via types.Channel, method string, params any) (any, error) {
	if method != string(kvs.Get) {
		return nil, fmt.Errorf("fakeKVSDevice: unsupported method %q", method)
	}
	req, ok := params.(*kvs.GetRequest)
	if !ok {
		return nil, fmt.Errorf("fakeKVSDevice: unexpected params type %T", params)
	}
	val, found := f.kvs[req.Key]
	if !found {
		return nil, fmt.Errorf("fakeKVSDevice: key %q not found", req.Key)
	}
	return &kvs.GetResponse{Value: val}, nil
}

// TestPoolNotices_ComputeTurnover verifies the #402 rewrite: ComputeTurnover
// reads the on-device runtime/turnover accumulators pool-pump.js maintains
// (script/pool-pump/runtime-sec, script/pool-pump/turnover-today) plus the
// unchanged configured target (script/pool-pump/turnover), instead of
// re-deriving flow rate from the non-numeric preferred-speed KVS key.
func TestPoolNotices_ComputeTurnover(t *testing.T) {
	dev := &fakeKVSDevice{
		id: "pool-device-compute-turnover",
		kvs: map[string]string{
			"script/pool-pump/runtime-sec":    "3600",
			"script/pool-pump/turnover-today": "0.756",
			"script/pool-pump/turnover":       "5",
		},
	}
	p := &PoolNotices{log: logr.Discard(), device: dev, deviceID: dev.id}

	achieved, target, runtimeSec, err := p.ComputeTurnover(context.Background())
	if err != nil {
		t.Fatalf("ComputeTurnover: %v", err)
	}
	if runtimeSec != 3600 {
		t.Errorf("runtimeSec = %d, want 3600", runtimeSec)
	}
	if achieved != 0.76 { // rounded to 2 decimals
		t.Errorf("achieved = %v, want 0.76", achieved)
	}
	if target != 5 {
		t.Errorf("target = %v, want 5", target)
	}
}

// TestPoolNotices_ComputeTurnover_MissingKey verifies a missing KVS key
// (e.g. an older pool-pump.js version that hasn't written runtime-sec yet)
// surfaces as an error rather than a silently-zeroed result.
func TestPoolNotices_ComputeTurnover_MissingKey(t *testing.T) {
	dev := &fakeKVSDevice{
		id: "pool-device-compute-turnover-missing",
		kvs: map[string]string{
			"script/pool-pump/turnover-today": "0.5",
			"script/pool-pump/turnover":       "5",
			// runtime-sec deliberately absent
		},
	}
	p := &PoolNotices{log: logr.Discard(), device: dev, deviceID: dev.id}

	if _, _, _, err := p.ComputeTurnover(context.Background()); err == nil {
		t.Fatal("expected error for missing runtime-sec KVS key, got nil")
	}
}
