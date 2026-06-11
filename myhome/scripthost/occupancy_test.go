package scripthost

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// testMqtt is an injectable/recording MQTT client for workflow tests.
type testMqtt struct {
	mu        sync.Mutex
	subs      map[string][]chan []byte
	published []publishedMsg
}

type publishedMsg struct {
	topic    string
	payload  string
	retained bool
}

func newTestMqtt() *testMqtt {
	return &testMqtt{subs: make(map[string][]chan []byte)}
}

func (m *testMqtt) GetServer() string { return "mock://localhost:1883" }
func (m *testMqtt) BrokerUrl() *url.URL {
	u, _ := url.Parse(m.GetServer())
	return u
}
func (m *testMqtt) Id() string { return "test-mqtt" }

func (m *testMqtt) Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan []byte, qlen)
	m.subs[topic] = append(m.subs[topic], ch)
	return ch, nil
}

func (m *testMqtt) SubscribeWithHandler(ctx context.Context, topic string, qlen uint, subscriber string, handle func(topic string, payload []byte, subscriber string) error) error {
	return nil
}

func (m *testMqtt) Publisher(ctx context.Context, topic string, qlen uint, qos byte, retained bool, publisherName string) (chan<- []byte, error) {
	ch := make(chan []byte, qlen)
	go func() {
		for range ch {
		}
	}()
	return ch, nil
}

func (m *testMqtt) Publish(ctx context.Context, topic string, msg []byte, qos byte, retained bool, publisherName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, publishedMsg{topic: topic, payload: string(msg), retained: retained})
	return nil
}

// inject delivers a payload to every subscription of the given filter.
func (m *testMqtt) inject(topic string, payload string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.subs[topic] {
		ch <- []byte(payload)
	}
}

// lastPublished returns the most recent message published on a topic.
func (m *testMqtt) lastPublished(topic string) (publishedMsg, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.published) - 1; i >= 0; i-- {
		if m.published[i].topic == topic {
			return m.published[i], true
		}
	}
	return publishedMsg{}, false
}

// TestOccupancyWorkflow runs the real occupancy.js on the script host: an
// injected Gen2 NotifyStatus input event must yield a retained occupied=true
// publication and flip the occupancy.getstatus RPC verb.
// No t.Parallel(): registers handlers in the shared myhome methods map.
func TestOccupancyWorkflow(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "shelly", "scripts", "occupancy.js"))
	if err != nil {
		t.Fatalf("read occupancy.js: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := testr.New(t)
	ctx = logr.NewContext(ctx, log)

	mc := newTestMqtt()
	mqtt.ResetClient()
	mqtt.SetClient(mc)
	t.Cleanup(mqtt.ResetClient)

	// lan.hosts infrastructure fake (normally registered by the daemon)
	myhome.RegisterMethodHandler(myhome.LanHosts, func(ctx context.Context, in any) (any, error) {
		return &myhome.LanHostsResult{Hosts: []myhome.LanHostInfo{}}, nil
	})

	svc := NewService(log, Config{
		Enabled:  true,
		Run:      []string{"occupancy"},
		StateDir: t.TempDir(),
	}, fstest.MapFS{"occupancy.js": &fstest.MapFile{Data: src}}, nil, "test-instance")
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait until the script subscribed to device events (i.e. start() ran
	// after its KVS config loads).
	deadline := time.Now().Add(5 * time.Second)
	for {
		mc.mu.Lock()
		n := len(mc.subs["+/events/rpc"])
		mc.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("occupancy.js never subscribed to +/events/rpc")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Inject an input state change (button press / motion)
	mc.inject("+/events/rpc", `{"src":"shellyplus1-aabbcc","method":"NotifyStatus","params":{"input:0":{"id":0,"state":true}}}`)

	// Expect a retained occupied=true publication
	deadline = time.Now().Add(5 * time.Second)
	for {
		if msg, ok := mc.lastPublished("myhome/occupancy"); ok {
			if !msg.retained {
				t.Errorf("occupancy publication not retained: %+v", msg)
			}
			var status struct {
				Occupied bool   `json:"occupied"`
				Reason   string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(msg.payload), &status); err != nil {
				t.Fatalf("bad occupancy payload %q: %v", msg.payload, err)
			}
			if !status.Occupied {
				t.Errorf("occupied = false, want true (reason %q)", status.Reason)
			}
			if !strings.Contains(status.Reason, "input change") {
				t.Errorf("reason = %q, want input change", status.Reason)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no occupancy publication observed")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The occupancy.getstatus RPC verb (registered from JS) must agree
	out, err := myhome.CallLocalE(ctx, myhome.OccupancyGetStatus, nil)
	if err != nil {
		t.Fatalf("CallLocalE(occupancy.getstatus): %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["occupied"] != true {
		t.Fatalf("occupancy.getstatus = %v (%T), want occupied=true", out, out)
	}
}
