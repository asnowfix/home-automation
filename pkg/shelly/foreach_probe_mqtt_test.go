package shelly

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// Issue #905: the default reachability probe (#902) made the real operation
// that follows it fail with "unexpected end of JSON input" (empty payload).
//
// The per-device MQTT channels are cached process-wide (deviceMqttRegistry),
// and the probe was the first to create them, with its short-lived ctx. The real
// client (myhome/mqtt/client.go) ties a subscription and a publisher to the ctx
// they were created with: when it ends, the subscriber queue is CLOSED and the
// publisher goroutine returns. The real run then read nil from the cached,
// closed reply channel. The fakes in foreach_unreachable_test.go never close
// anything, which is why #902's tests did not see it; ctxBroker below models
// the real client's lifetime rules.

// ctxBroker is an in-process MQTT client that (a) closes a subscriber's queue
// when the ctx passed to Subscribe ends and stops a publisher when the ctx
// passed to Publisher ends, exactly like myhome/mqtt/client.go, and (b) plays
// every Shelly device: a request published on "<device-id>/rpc" is answered on
// "<src>/rpc" with the same id.
type ctxBroker struct {
	mu   sync.Mutex
	subs map[string][]chan []byte
}

func newCtxBroker() *ctxBroker { return &ctxBroker{subs: map[string][]chan []byte{}} }

func (*ctxBroker) GetServer() string   { return "ctx-broker" }
func (*ctxBroker) BrokerUrl() *url.URL { return &url.URL{} }
func (*ctxBroker) Id() string          { return "ctxbroker" }

func (b *ctxBroker) Subscribe(ctx context.Context, topic string, qlen uint, _ string) (<-chan []byte, error) {
	q := make(chan []byte, qlen)
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], q)
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		list := b.subs[topic]
		for i, c := range list {
			if c == q {
				b.subs[topic] = append(list[:i:i], list[i+1:]...)
				break
			}
		}
		close(q) // what myhome/mqtt/client.go's subscribe watcher does
	}()
	return q, nil
}

func (*ctxBroker) SubscribeWithHandler(context.Context, string, uint, string, func(string, []byte, string) error) error {
	return nil
}

func (b *ctxBroker) Publisher(ctx context.Context, topic string, qlen uint, _ byte, _ bool, _ string) (chan<- []byte, error) {
	mch := make(chan []byte, qlen)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return // what myhome/mqtt/client.go's Publisher does
			case msg := <-mch:
				b.answer(topic, msg)
			}
		}
	}()
	return mch, nil
}

func (b *ctxBroker) Publish(_ context.Context, topic string, msg []byte, _ byte, _ bool, _ string) error {
	b.deliver(topic, msg)
	return nil
}

func (b *ctxBroker) deliver(topic string, msg []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, q := range b.subs[topic] {
		select {
		case q <- msg:
		default:
		}
	}
}

// answer plays the device: any request gets a Shelly.GetDeviceInfo-shaped result.
func (b *ctxBroker) answer(topic string, msg []byte) {
	deviceId := strings.TrimSuffix(topic, "/rpc")
	var req struct {
		Id  uint32 `json:"id"`
		Src string `json:"src"`
	}
	if err := json.Unmarshal(msg, &req); err != nil || req.Src == "" {
		return
	}
	res, _ := json.Marshal(map[string]any{
		"id":     req.Id,
		"src":    deviceId,
		"dst":    req.Src,
		"result": map[string]any{"id": deviceId, "mac": "A0DD6CA1C588"},
	})
	b.deliver(req.Src+"/rpc", res)
}

var initRegistrarForProbeTest sync.Once

// probeCtx wires the package for real MQTT calls through b (registrar, method
// handlers, MQTT device caller), as the ctl composition root does.
func probeCtx(t *testing.T, b *ctxBroker) context.Context {
	t.Helper()
	initRegistrarForProbeTest.Do(func() {
		Init(logr.Discard(), b, 2*time.Second, 0)
	})
	return mqtt.NewContextWithClient(context.Background(), b)
}

// probeTestDevice ids are unique to this file (deviceMqttRegistry is process-wide).
func probeTestDevice(suffix string) macDevice {
	return macDevice{stubDevice{id: "shellyplus1-905a00000" + suffix, name: "dev905-" + suffix, host: "192.168.1.21"}}
}

// TestForeach_DefaultProbeThenRealCallOverMqtt is the #905 regression test:
// the real (default) probe, then a real RPC over MQTT on the same devices.
// Without the fix, each device's reply channel was closed by the end of the
// probe's ctx and the call failed with "unexpected end of JSON input".
func TestForeach_DefaultProbeThenRealCallOverMqtt(t *testing.T) {
	b := newCtxBroker()
	ctx := probeCtx(t, b)

	var list []devices.Device
	for _, s := range []string{"001", "002", "003", "004", "005", "006", "007", "008"} {
		list = append(list, probeTestDevice(s))
	}

	do := func(ctx context.Context, _ logr.Logger, via types.Channel, d devices.Device, _ []string) (any, error) {
		// Give the end of the probe's ctx time to take effect, so the test does
		// not depend on winning a race against the subscription teardown.
		time.Sleep(50 * time.Millisecond)
		return shelly.GetDeviceInfo(ctx, d.(*Device), via)
	}

	out, err := ForeachWith(ctx, testr.New(t), list, types.ChannelMqtt, do, nil,
		ForeachOptions{ProbeTimeout: time.Second})
	if err != nil {
		t.Fatalf("probe then real call failed: %v (results: %+v)", err, out)
	}
	// Unreachable devices are left out of the results: every device must be there.
	if n := len(out.([]any)); n != len(list) {
		t.Fatalf("got %d results, want %d (devices wrongly marked unreachable)", n, len(list))
	}
}

// TestDeviceMqttChannels_OutliveInitCtx pins the invariant directly: the cached
// channels must keep working after the ctx that first created them has ended,
// because every later Device for the same id reuses them.
func TestDeviceMqttChannels_OutliveInitCtx(t *testing.T) {
	b := newCtxBroker()
	base := mqtt.NewContextWithClient(context.Background(), b)
	const id = "shellyplus1-905a00000099"

	short, cancel := context.WithCancel(base)
	ch, err := (*DeviceMqttChannels)(nil).Init(short, id)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(50 * time.Millisecond) // let the broker's teardown goroutines run

	ch.To <- []byte(`{"id":7,"src":"` + ch.ReplyTo + `","method":"Shelly.GetDeviceInfo"}`)
	select {
	case msg, ok := <-ch.From:
		if !ok {
			t.Fatal("reply channel was closed when the ctx that created it ended (#905)")
		}
		if !strings.Contains(string(msg), `"id":7`) {
			t.Fatalf("unexpected reply %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reply: publisher stopped when the ctx that created it ended (#905)")
	}
}
