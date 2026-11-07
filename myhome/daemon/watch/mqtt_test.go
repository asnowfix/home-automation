package watch

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	shellymqtt "github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	shellyschema "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// loadEventFixture reads a real captured MQTT event, classified into
// pkg/shelly/mqtt/testdata by tools/classify-events, and unmarshals it into
// a shellymqtt.Event ready to feed into UpdateFromMqttEvent.
func loadEventFixture(t *testing.T, name string) *shellymqtt.Event {
	t.Helper()
	data, err := os.ReadFile("../../../pkg/shelly/mqtt/testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var e shellymqtt.Event
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return &e
}

// TestMqttWatcherStopsOnChannelClose is a regression test: a closed
// subscription channel used to make mqttWatcher spin forever on nil receives
// (logging "nil message" in a tight loop). The watcher must exit instead.
func TestMqttWatcherStopsOnChannelClose(t *testing.T) {
	ctx := context.Background()
	log := testr.New(t)

	ch := make(chan mqtt.Message)
	done := make(chan struct{})
	go func() {
		mqttWatcher(ctx, log, "+/events/rpc", nil, nil, ch, nil)
		close(done)
	}()

	// A nil message on an open channel is skipped, not fatal
	ch <- nil
	select {
	case <-done:
		t.Fatal("watcher exited on a nil message; it should only skip it")
	case <-time.After(50 * time.Millisecond):
	}

	// Closing the channel must end the watcher (not busy-loop)
	close(ch)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop after the subscription channel closed")
	}
}

// TestMqttWatcherStopsOnContextCancel covers the pre-existing shutdown path.
func TestMqttWatcherStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	log := testr.New(t)

	ch := make(chan mqtt.Message)
	done := make(chan struct{})
	go func() {
		mqttWatcher(ctx, log, "+/events/rpc", nil, nil, ch, nil)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop on context cancellation")
	}
}

// TestUpdateFromMqttEvent_ExternalKvsWriteInvalidatesCache replays two real
// NotifyStatus events captured live from a Shelly device (development.local,
// shellyplus1-08b61fd98f44) around a KVS write made directly against the
// device over HTTP — i.e. a write myhome's own kvs cache never saw. It
// asserts that observing the device's sys.kvs_rev counter change between the
// two events causes the daemon to treat the change as external and drop its
// cached KVS reads for that device.
func TestUpdateFromMqttEvent_ExternalKvsWriteInvalidatesCache(t *testing.T) {
	log := testr.New(t)
	ctx := logr.NewContext(context.Background(), log)

	deviceId := "shellyplus1-08b61fd98f44"
	d := myhome.NewDevice(log, myhome.SHELLY, deviceId)

	fd := types.NewFakeDevice()
	fd.IdValue = deviceId
	fd.SetResult(string(kvs.Get), &kvs.GetResponse{Value: "true"})

	// Prime the cache the same way DeviceToView/OnValue would.
	if _, err := kvs.GetValue(ctx, log, types.ChannelDefault, fd, "normally-closed"); err != nil {
		t.Fatalf("unexpected error priming cache: %v", err)
	}
	if len(fd.Calls) != 1 {
		t.Fatalf("expected 1 device call after priming, got %d", len(fd.Calls))
	}

	before := loadEventFixture(t, "notify_status__shellyplus1__sys_kvs_rev_before.json")
	if _, err := UpdateFromMqttEvent(ctx, d, before); err != nil {
		t.Fatalf("unexpected error on baseline event: %v", err)
	}

	// Baseline observation must not evict anything yet.
	if _, err := kvs.GetValue(ctx, log, types.ChannelDefault, fd, "normally-closed"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fd.Calls) != 1 {
		t.Fatalf("expected cache hit after baseline event (still 1 call), got %d", len(fd.Calls))
	}

	after := loadEventFixture(t, "notify_status__shellyplus1__sys_kvs_rev_after.json")
	if _, err := UpdateFromMqttEvent(ctx, d, after); err != nil {
		t.Fatalf("unexpected error on follow-up event: %v", err)
	}

	// The kvs_rev bump between the two real events was never preceded by a
	// SetKeyValue/DeleteKey call from this process, so it must be treated as
	// external and the cache entry must be gone.
	if _, err := kvs.GetValue(ctx, log, types.ChannelDefault, fd, "normally-closed"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fd.Calls) != 2 {
		t.Fatalf("expected a fresh device call after external kvs_rev change, got %d total calls", len(fd.Calls))
	}
}

// TestUpdateFromMqttEvent_AppliesSwitchNotification replays a real captured
// NotifyStatus (switch:0 output flip) and checks that it lands in the
// device's in-memory status and is reported as a user-visible change, while
// an event repeating the same output is not.
func TestUpdateFromMqttEvent_AppliesSwitchNotification(t *testing.T) {
	log := testr.New(t)
	ctx := logr.NewContext(context.Background(), log)

	ev := loadEventFixture(t, "notify_status__shelly1minig3__switch_0.json") // output:false
	sd, err := shellyapi.NewDeviceFromMqttId(ctx, log, ev.Src)
	if err != nil {
		t.Fatal(err)
	}
	dev := sd.(*shellyapi.Device)
	dev.SetStatus(&shellyschema.Status{Switch0: &sswitch.Status{Output: true}})
	d := myhome.NewDevice(log, myhome.SHELLY, ev.Src).WithImpl(dev)

	changed, err := UpdateFromMqttEvent(ctx, d, ev)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want true,nil", changed, err)
	}
	if dev.Status().Switch0.Output {
		t.Error("switch output not updated from the notification")
	}

	changed, err = UpdateFromMqttEvent(ctx, d, ev)
	if err != nil || changed {
		t.Fatalf("replayed event: changed=%v err=%v, want false,nil", changed, err)
	}
}

// TestApplyEvent_CallsOnStateChange covers the notification -> status ->
// callback path: the callback fires once for a flip and not for a replay.
func TestApplyEvent_CallsOnStateChange(t *testing.T) {
	log := testr.New(t)
	ctx := logr.NewContext(context.Background(), log)

	ev := loadEventFixture(t, "notify_status__shelly1minig3__switch_0.json") // output:false
	sd, err := shellyapi.NewDeviceFromMqttId(ctx, log, ev.Src)
	if err != nil {
		t.Fatal(err)
	}
	dev := sd.(*shellyapi.Device)
	dev.SetStatus(&shellyschema.Status{Switch0: &sswitch.Status{Output: true}})
	d := myhome.NewDevice(log, myhome.SHELLY, ev.Src).WithImpl(dev)

	var got []string
	cb := func(x *myhome.Device) { got = append(got, x.Id()) }
	for i := 0; i < 2; i++ {
		if err := applyEvent(ctx, log, d, ev, cb); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 1 || got[0] != ev.Src {
		t.Fatalf("callback calls = %v, want exactly one for %s", got, ev.Src)
	}
}
