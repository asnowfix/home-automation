package impl

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/myhome/ui"
	mhd "github.com/asnowfix/home-automation/myhome/devices"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/go-logr/logr"
)

type listRegistry struct {
	mhd.DeviceRegistry // only GetAllDevices is called
	devs               []*myhome.Device
}

func (f listRegistry) GetAllDevices(context.Context) ([]*myhome.Device, error) { return f.devs, nil }

type syncSSE struct {
	mu sync.Mutex
	dv []ui.DeviceView
}

func (s *syncSSE) BroadcastSensorUpdate(string, string, string) {}
func (s *syncSSE) BroadcastDeviceUpdate(dv ui.DeviceView) {
	s.mu.Lock()
	s.dv = append(s.dv, dv)
	s.mu.Unlock()
}
func (s *syncSSE) ids() (out []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.dv {
		out = append(out, d.Id)
	}
	return
}

// TestRefreshJobEndsStarting pins the end-of-"Starting…" rule (#763): the UI's
// post-start phase is on until the refresh round-robin has visited every Gen2
// device once, then it is off and open browsers are told to re-fetch the
// devices that have switches.
func TestRefreshJobEndsStarting(t *testing.T) {
	ui.SetStarting(true)
	t.Cleanup(func() { ui.SetStarting(false) })

	sw := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyplus1-aabbccddee41")
	sw.Config = &shelly.Config{Switch0: &sswitch.Config{Id: 0}}
	plain := myhome.NewDevice(logr.Discard(), myhome.SHELLY, "shellyplus1-aabbccddee42")
	sse := &syncSSE{}
	dm := &DeviceManager{
		sseBroadcaster: sse,
		update:         make(chan *myhome.Device, 8),
		dr:             listRegistry{devs: []*myhome.Device{sw, plain}},
	}

	ctx, cancel := context.WithCancel(logr.NewContext(context.Background(), logr.Discard()))
	defer cancel()
	go dm.runDeviceRefreshJob(ctx, 10*time.Millisecond)

	// Ticks 1 and 2 visit the two devices; the phase must still be on after the first.
	<-dm.update
	if !ui.IsStarting() {
		t.Fatal("Starting… ended before the first pass was complete")
	}
	<-dm.update
	deadline := time.Now().Add(2 * time.Second)
	for ui.IsStarting() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ui.IsStarting() {
		t.Fatal("Starting… still on after a full pass")
	}
	// The flag clears before the re-fetch broadcast goes out: wait for it.
	var got []string
	for time.Now().Before(deadline) {
		if got = sse.ids(); len(got) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let a wrong extra broadcast show up
	got = sse.ids()
	if len(got) != 1 || got[0] != sw.Id() {
		t.Errorf("re-fetch broadcast for %v; want only the switch device %s", got, sw.Id())
	}
}

// TestRefreshJobExitEndsStarting: however the job exits, the phase ends, so a
// device can never stay "Starting…" forever.
func TestRefreshJobExitEndsStarting(t *testing.T) {
	ui.SetStarting(true)
	t.Cleanup(func() { ui.SetStarting(false) })
	dm := &DeviceManager{update: make(chan *myhome.Device, 1), dr: listRegistry{}}
	ctx, cancel := context.WithCancel(logr.NewContext(context.Background(), logr.Discard()))
	done := make(chan struct{})
	go func() { dm.runDeviceRefreshJob(ctx, time.Hour); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not exit")
	}
	if ui.IsStarting() {
		t.Error("Starting… still on after the refresh job exited")
	}
}
