package daemon

import (
	"context"
	"errors"
	"testing"

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
	if s.DeviceID != "pool-1" || s.WaterSupplyActive != nil || s.Turnover24h != nil || s.TurnoverTarget != nil {
		t.Errorf("snapshot = %+v, want only DeviceID set", s)
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
