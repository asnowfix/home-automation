package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	shellytypes "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/go-logr/logr"
)

func TestPoolSnapshotDisabled(t *testing.T) {
	_, err := NewPoolRPCHandler(logr.Discard(), nil, false).handleSnapshot(context.Background(), nil)
	if !errors.Is(err, myhome.ErrPoolNotEnabled) {
		t.Errorf("err = %v, want ErrPoolNotEnabled", err)
	}
}

// Nothing known yet: every field is nil (rendered "--"), never a default.
func TestPoolSnapshotUnknown(t *testing.T) {
	h := NewPoolRPCHandler(logr.Discard(), &PoolNotices{log: logr.Discard()}, true).
		WithSnapshotSource("pool-1", func(context.Context) *bool { return nil })
	out, err := h.handleSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := out.(*myhome.PoolSnapshotResult)
	if s.DeviceID != "pool-1" || s.WaterSupplyActive != nil || s.TurnoverAchieved != nil || s.TurnoverTarget != nil || s.RuntimeSec != nil || s.TurnoverTs != 0 {
		t.Errorf("snapshot = %+v, want only DeviceID set", s)
	}
}

// A turnover read (start, pool.pump_stop) fills the snapshot with a timestamp.
func TestPoolSnapshotFilledByTurnoverRead(t *testing.T) {
	dev := &fakeKVSDevice{id: "pool-1", kvs: map[string]string{kvsKeyRuntimeToday: "5400", kvsKeyTurnoverToday: "3.456", kvsKeyTurnover: "5"}}
	p := &PoolNotices{log: logr.Discard(), device: dev, deviceID: "pool-1"}
	if _, ok := p.Snapshot(); ok {
		t.Fatal("snapshot present before any reading")
	}
	if _, _, _, err := p.ComputeTurnover(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, ok := p.Snapshot()
	if !ok || s.achieved != 3.46 || s.target != 5 || s.runtimeSec != 5400 || s.ts.IsZero() {
		t.Errorf("snapshot = %+v ok=%v", s, ok)
	}
	before := dev.calls.Load()
	out, _ := NewPoolRPCHandler(logr.Discard(), p, true).WithSnapshotSource("pool-1", nil).handleSnapshot(context.Background(), nil)
	if n := dev.calls.Load(); n != before {
		t.Errorf("handleSnapshot made %d device call(s), want 0", n-before)
	}
	r := out.(*myhome.PoolSnapshotResult)
	if r.TurnoverAchieved == nil || *r.TurnoverAchieved != 3.46 || *r.RuntimeSec != 5400 || r.TurnoverTs == 0 {
		t.Errorf("result = %+v", r)
	}
}

type regOf struct{ d *myhome.Device }

func (r regOf) GetDeviceById(context.Context, string) (*myhome.Device, error) { return r.d, nil }

// The water-supply input comes from the cached status; no status means unknown.
func TestCachedWaterSupply(t *testing.T) {
	d := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellypro1-aabbccddee20")
	impl, err := shellyapi.NewDeviceFromSummary(context.Background(), logr.Discard(), d)
	if err != nil {
		t.Fatal(err)
	}
	sd := impl.(*shellyapi.Device)
	d.WithImpl(sd)
	read := cachedWaterSupply(regOf{d}, d.Id())

	if got := read(context.Background()); got != nil {
		t.Errorf("no status: got %v, want nil", *got)
	}
	sd.SetStatus(&shellytypes.Status{Input0: &sswitch.InputStatus{State: true}})
	if got := read(context.Background()); got == nil || !*got {
		t.Errorf("input:0 true: got %v", got)
	}
	sd.SetStatus(&shellytypes.Status{Input0: &sswitch.InputStatus{State: false}})
	if got := read(context.Background()); got == nil || *got {
		t.Errorf("input:0 false: got %v", got)
	}
}

// A reading from the previous local day is never shown as today's: crossing
// midnight with an injected clock drops the turnover fields (rendered "--").
func TestPoolSnapshotDropsPreviousDay(t *testing.T) {
	loc := time.FixedZone("test", 2*3600)
	p := &PoolNotices{log: logr.Discard()}
	p.snap = poolTurnoverSnapshot{achieved: 5, target: 5, runtimeSec: 7200, ts: time.Date(2026, 11, 7, 23, 30, 0, 0, loc)}
	clock := time.Date(2026, 11, 7, 23, 59, 0, 0, loc)
	h := NewPoolRPCHandler(logr.Discard(), p, true).WithSnapshotSource("pool-1", nil)
	h.now = func() time.Time { return clock }

	snap := func() *myhome.PoolSnapshotResult {
		out, err := h.handleSnapshot(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		return out.(*myhome.PoolSnapshotResult)
	}
	if r := snap(); r.TurnoverAchieved == nil || *r.TurnoverAchieved != 5 {
		t.Fatalf("same day: result = %+v, want the reading", r)
	}
	clock = time.Date(2026, 11, 8, 0, 1, 0, 0, loc) // past local midnight
	if r := snap(); r.TurnoverAchieved != nil || r.TurnoverTarget != nil || r.RuntimeSec != nil || r.TurnoverTs != 0 {
		t.Errorf("next day: result = %+v, want turnover unknown", r)
	}
	p.storeSnapshot(1, 5, 600) // a fresh reading (real clock) is shown against a real clock
	h.now = nil
	if r := snap(); r.TurnoverAchieved == nil {
		t.Error("fresh reading dropped")
	}
}
