package impl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	mhd "github.com/asnowfix/home-automation/myhome/devices"
	"github.com/go-logr/logr"
)

// regStub answers only the lookups setRoomKVS makes.
type regStub struct {
	mhd.DeviceRegistry
	devices map[string]*myhome.Device
}

func (r *regStub) GetDeviceByAny(_ context.Context, id string) (*myhome.Device, error) {
	if d, ok := r.devices[id]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func newSetRoomManager(writer func(context.Context, logr.Logger, *myhome.Device, string) error) *DeviceManager {
	return &DeviceManager{
		dr: &regStub{devices: map[string]*myhome.Device{
			"shellyplus1-aaa":        newTestDevice("shellyplus1-aaa", "hall-light"),
			"shellyht-208500":        newTestDevice("shellyht-208500", "gen1-sensor"),
			"shellyblu-112233445566": newTestDevice("shellyblu-112233445566", "blu"),
		}},
		kvsWriter: writer,
	}
}

// The three outcomes (plus lookup failure) of device.setroom's KVS step.
func TestSetRoomKVS_Outcomes(t *testing.T) {
	ok := func(context.Context, logr.Logger, *myhome.Device, string) error { return nil }
	fail := func(context.Context, logr.Logger, *myhome.Device, string) error { return errors.New("host unreachable") }
	noKVS := func(context.Context, logr.Logger, *myhome.Device, string) error { return errNoKVS }
	cases := []struct {
		name   string
		id     string
		writer func(context.Context, logr.Logger, *myhome.Device, string) error
		want   string
	}{
		{"written", "shellyplus1-aaa", ok, myhome.KVSSet},
		{"write fails", "shellyplus1-aaa", fail, myhome.KVSUnreachable},
		{"gen1 has no KVS", "shellyht-208500", fail, myhome.KVSNotApplicable},
		{"blu has no KVS", "shellyblu-112233445566", fail, myhome.KVSNotApplicable},
		{"not a Gen2 shelly", "shellyplus1-aaa", noKVS, myhome.KVSNotApplicable},
		{"unknown device", "nope", ok, myhome.KVSUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := newSetRoomManager(c.writer).setRoomKVS(context.Background(), logr.Discard(), c.id, "hall")
			if got.KVS != c.want {
				t.Errorf("got %+v, want KVS=%s", got, c.want)
			}
			if c.want == myhome.KVSUnreachable && got.KVSError == "" {
				t.Error("unreachable without a reason")
			}
		})
	}
}

// An offline device: the device side takes far longer than the client is
// willing to wait. The handler must still answer "unreachable" inside the
// client's budget, otherwise the caller times out first and aborts.
func TestSetRoomKVS_OfflineDeviceAnswersBeforeClientTimeout(t *testing.T) {
	const clientBudget = 1 * time.Second
	release := make(chan struct{})
	defer close(release)
	// Ignores its context, like a device layer stuck in a network call.
	hang := func(context.Context, logr.Logger, *myhome.Device, string) error {
		<-release
		return nil
	}
	dm := newSetRoomManager(hang)
	dm.kvsTimeout = 100 * time.Millisecond

	start := time.Now()
	got := dm.setRoomKVS(context.Background(), logr.Discard(), "shellyplus1-aaa", "hall")
	if elapsed := time.Since(start); elapsed >= clientBudget {
		t.Fatalf("handler took %v, client gives up after %v", elapsed, clientBudget)
	}
	if got.KVS != myhome.KVSUnreachable {
		t.Errorf("got %+v, want unreachable", got)
	}
	if defaultKVSTimeout >= 14*time.Second {
		t.Errorf("default KVS timeout %v must stay below the default client timeout (14s)", defaultKVSTimeout)
	}
}
