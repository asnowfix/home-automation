package ui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/go-logr/logr"
)

// stubPoolVerbs registers a pool.snapshot answering snap and a pool.getstatus
// that counts calls (it must never be reached from a render path, #731). Both
// are restored when the test ends.
func stubPoolVerbs(t *testing.T, snap *myhome.PoolSnapshotResult) *atomic.Int32 {
	t.Helper()
	var live atomic.Int32
	// Save what was registered before, to restore it (not just unregister) at the end.
	for _, v := range []myhome.Verb{myhome.PoolGetStatus, myhome.PoolSnapshot} {
		prev, err := myhome.Methods(v)
		t.Cleanup(func() {
			if err == nil {
				myhome.RegisterMethodHandler(v, prev.ActionE)
			} else {
				myhome.UnregisterMethodHandler(v)
			}
		})
	}
	myhome.RegisterMethodHandler(myhome.PoolGetStatus, func(context.Context, any) (any, error) {
		live.Add(1)
		return nil, context.DeadlineExceeded
	})
	myhome.RegisterMethodHandler(myhome.PoolSnapshot, func(context.Context, any) (any, error) { return snap, nil })
	return &live
}

// Rendering the Home view 100 times makes no device call and no pool.getstatus
// call, even with the pool device unreachable; unknown pool fields render "--",
// never "OK".
func TestRenderDevicesPoolFromMemory(t *testing.T) {
	calls := unreachableDevices(t)
	live := stubPoolVerbs(t, &myhome.PoolSnapshotResult{DeviceID: "shellypro1-aabbccddee10"}) // everything unknown
	pool := switchDevice(t, "shellypro1-aabbccddee10", false)
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: []*myhome.Device{pool}}, nil, nil, "", nil, nil)

	var body string
	for i := 0; i < 100; i++ {
		rec := get(h, "/htmx/devices")
		if rec.Code != 200 {
			t.Fatalf("/htmx/devices = %d", rec.Code)
		}
		body = rec.Body.String()
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("100 renders made %d device call(s), want 0", n)
	}
	if n := live.Load(); n != 0 {
		t.Errorf("100 renders called pool.getstatus %d time(s), want 0", n)
	}
	if !strings.Contains(body, `id="water-supply-shellypro1-aabbccddee10"`) {
		t.Fatalf("pool pill missing:\n%s", body)
	}
	if !strings.Contains(body, "Water supply --") || strings.Contains(body, "Water supply OK") {
		t.Errorf("unknown water supply must render --, not OK:\n%s", body)
	}
	if !strings.Contains(body, "Turnover 24 h --") {
		t.Errorf("unknown turnover must render --:\n%s", body)
	}
}

// Known snapshot values render, still without a device call.
func TestRenderDevicesPoolSnapshotValues(t *testing.T) {
	calls := unreachableDevices(t)
	ok, achieved, target, runtime := false, 3.2, 5.0, int64(5400)
	live := stubPoolVerbs(t, &myhome.PoolSnapshotResult{
		DeviceID: "shellypro1-aabbccddee11", WaterSupplyActive: &ok,
		Turnover24h: &achieved, TurnoverTarget: &target, Runtime24hSec: &runtime,
	})
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{devs: []*myhome.Device{switchDevice(t, "shellypro1-aabbccddee11", false)}}, nil, nil, "", nil, nil)

	body := get(h, "/htmx/device/shellypro1-aabbccddee11").Body.String()
	for _, want := range []string{"Water supply OK", "3.2/5.0 x/day"} {
		if !strings.Contains(body, want) {
			t.Errorf("card lacks %q:\n%s", want, body)
		}
	}
	if calls.Load() != 0 || live.Load() != 0 {
		t.Errorf("device calls = %d, pool.getstatus calls = %d, want 0", calls.Load(), live.Load())
	}
}

// The Pool section reads the snapshot too.
func TestPoolPanelFromMemory(t *testing.T) {
	live := stubPoolVerbs(t, &myhome.PoolSnapshotResult{DeviceID: "shellypro1-aabbccddee12"})
	h := newMux(renderCtx(), logr.Discard(), nil, fakeRegistry{}, nil, nil, "", nil, nil)

	body := get(h, "/htmx/pool").Body.String()
	if !strings.Contains(body, "Water supply: --") || strings.Contains(body, "Water supply: OK") {
		t.Errorf("unknown water supply must render --:\n%s", body)
	}
	if live.Load() != 0 {
		t.Errorf("Pool section called pool.getstatus %d time(s), want 0", live.Load())
	}
}
