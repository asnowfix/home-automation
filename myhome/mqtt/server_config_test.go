package mqtt

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	mochiServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/spf13/viper"
)

func mochiDefaultPending() int32 {
	return mochiServer.NewDefaultServerCapabilities().MaximumClientWritesPending
}

func TestLoadBrokerConfig_WritesPending(t *testing.T) {
	def := mochiDefaultPending()
	fromBlock := func(n int) *viper.Viper {
		v := viper.New()
		v.Set("mqtt.broker", map[string]any{"capabilities": map[string]any{"MaximumClientWritesPending": n}})
		return v
	}
	tests := []struct {
		name    string
		v       *viper.Viper
		setting int
		want    int32
	}{
		{"default (nil viper, 0)", nil, 0, def},
		{"default (empty viper, 0)", viper.New(), 0, def},
		{"override", viper.New(), 500, 500},
		{"negative setting ignored", viper.New(), -3, def},
		{"setting beats mqtt.broker block", fromBlock(77), 500, 500},
		{"0 keeps mqtt.broker block", fromBlock(77), 0, 77},
		{"negative in block replaced by default", fromBlock(-5), 0, def},
		{"zero in block replaced by default", fromBlock(0), 0, def},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadBrokerConfig(context.Background(), logr.Discard(), tt.v, tt.setting)
			if got.Capabilities.MaximumClientWritesPending != tt.want {
				t.Fatalf("MaximumClientWritesPending = %d, want %d", got.Capabilities.MaximumClientWritesPending, tt.want)
			}
		})
	}
	if def != 8192 {
		t.Logf("note: Mochi default is %d", def)
	}
}

func TestDropLogger_RateLimitedPerClient(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	log := funcr.New(func(prefix, args string) {
		mu.Lock()
		lines = append(lines, args)
		mu.Unlock()
	}, funcr.Options{})
	now := time.Unix(1000, 0)
	h := newDropLogger(log, time.Minute, func() time.Time { return now })

	a := &mochiServer.Client{ID: "slow-a"}
	b := &mochiServer.Client{ID: "slow-b"}
	pk := packetFor("t/1")

	h.OnPublishDropped(a, pk) // logged
	h.OnPublishDropped(a, pk) // suppressed
	h.OnPublishDropped(a, pk) // suppressed
	h.OnPublishDropped(b, pk) // other client: logged
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %v", len(lines), lines)
	}
	now = now.Add(61 * time.Second)
	h.OnPublishDropped(a, pk) // interval elapsed: logged with the count of 3 (2 suppressed + this one)
	if len(lines) != 3 || !strings.Contains(lines[2], `"dropped_since_last_log"=3`) ||
		!strings.Contains(lines[2], `"client_id"="slow-a"`) || !strings.Contains(lines[2], `"topic"="t/1"`) {
		t.Fatalf("unexpected third line: %v", lines)
	}
	h.OnDisconnect(a, nil, false)
	if _, ok := h.clients["slow-a"]; ok {
		t.Fatal("OnDisconnect must forget the client")
	}
}

// TestBroker_TinyCapDropsAndLogs runs an in-process broker built from loadBrokerConfig with a cap of 1.
// The subscriber's goroutine is not stopped, but its TCP reader is never drained: we use a raw
// connection that subscribes and then stops reading, and publish enough to overflow.
func TestBroker_TinyCapDropsAndLogs(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	log := funcr.New(func(prefix, args string) {
		mu.Lock()
		lines = append(lines, args)
		mu.Unlock()
	}, funcr.Options{})

	opts := loadBrokerConfig(context.Background(), logr.Discard(), viper.New(), 1)
	if opts.Capabilities.MaximumClientWritesPending != 1 {
		t.Fatalf("cap = %d, want 1", opts.Capabilities.MaximumClientWritesPending)
	}
	opts.InlineClient = true
	srv := mochiServer.New(opts)
	if err := srv.AddHook(newDropLogger(log, time.Minute, time.Now), nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	l := listeners.NewTCP(listeners.Config{ID: "t", Address: "127.0.0.1:0"})
	if err := srv.AddListener(l); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	var addr string
	waitFor(t, func() bool { addr = l.Address(); return addr != "" && addr != "127.0.0.1:0" })

	// A slow consumer: subscribed, then its network write path is blocked by never reading while a
	// tiny queue overflows. Paho reads continuously, so instead block inside the message handler.
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + addr).SetClientID("slow").
		SetOrderMatters(true).SetAutoReconnect(false))
	if tok := c.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("connect: %v", tok.Error())
	}
	t.Cleanup(func() { c.Disconnect(0) })
	if tok := c.Subscribe("flood/#", 0, func(mqtt.Client, mqtt.Message) { <-block }); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("subscribe: %v", tok.Error())
	}

	// Pad payloads so the socket buffers fill quickly once the client stops reading.
	payload := make([]byte, 64*1024)
	waitFor(t, func() bool {
		_ = srv.Publish("flood/x", payload, false, 0)
		mu.Lock()
		defer mu.Unlock()
		return len(lines) > 0
	})
	if got := srv.Info.MessagesDropped; got < 1 {
		t.Fatalf("MessagesDropped = %d, want >= 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(lines[0], `"client_id"="slow"`) || !strings.Contains(lines[0], `"topic"="flood/x"`) {
		t.Fatalf("drop line lacks client id / topic: %s", lines[0])
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		runtimeGosched()
	}
}

func TestLoadBrokerConfig_SnakeCaseAndUnknownKeys(t *testing.T) {
	var lines []string
	log := funcr.New(func(prefix, args string) { lines = append(lines, args) }, funcr.Options{})

	set := func(m map[string]any) *viper.Viper {
		v := viper.New()
		v.Set("mqtt.broker", m)
		return v
	}

	t.Run("snake_case honoured, no warning", func(t *testing.T) {
		lines = nil
		got := loadBrokerConfig(context.Background(), log, set(map[string]any{
			"sys_topic_resend_interval": 42,
			"capabilities":              map[string]any{"maximum_client_writes_pending": 123, "receive_maximum": 7},
		}), 0)
		if got.Capabilities.MaximumClientWritesPending != 123 || got.Capabilities.ReceiveMaximum != 7 || got.SysTopicResendInterval != 42 {
			t.Fatalf("snake_case keys not applied: %+v %+v", got, got.Capabilities)
		}
		for _, l := range lines {
			if strings.Contains(l, "WARNING") {
				t.Fatalf("unexpected warning: %s", l)
			}
		}
	})

	t.Run("legacy lowercase still works", func(t *testing.T) {
		got := loadBrokerConfig(context.Background(), logr.Discard(), set(map[string]any{
			"capabilities": map[string]any{"maximumclientwritespending": 55},
		}), 0)
		if got.Capabilities.MaximumClientWritesPending != 55 {
			t.Fatalf("got %d, want 55", got.Capabilities.MaximumClientWritesPending)
		}
	})

	t.Run("unknown keys warn, known ones still applied", func(t *testing.T) {
		lines = nil
		got := loadBrokerConfig(context.Background(), log, set(map[string]any{
			"bogus_top":    1,
			"capabilities": map[string]any{"maximum_client_writes_pnding": 9, "receive_maximum": 7},
		}), 0)
		if got.Capabilities.ReceiveMaximum != 7 {
			t.Fatalf("known key not applied alongside unknown ones")
		}
		joined := strings.Join(lines, "\n")
		for _, want := range []string{"mqtt.broker.bogus_top", "mqtt.broker.Capabilities.maximum_client_writes_pnding"} {
			if !strings.Contains(joined, want) {
				t.Errorf("missing warning for %q in:\n%s", want, joined)
			}
		}
	})
}
