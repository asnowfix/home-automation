package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	shellytypes "github.com/asnowfix/home-automation/pkg/shelly/shelly"
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
	p := &PoolNotices{
		log:      logr.Discard(),
		device:   &fakeKVSDevice{id: "pool-1", kvs: map[string]string{kvsKeyRuntimeToday: "5400", kvsKeyTurnoverToday: "3.456", kvsKeyTurnover: "5"}},
		deviceID: "pool-1",
	}
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
	out, _ := NewPoolRPCHandler(logr.Discard(), p, true).WithSnapshotSource("pool-1", nil).handleSnapshot(context.Background(), nil)
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
