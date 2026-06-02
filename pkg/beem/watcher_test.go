package beem

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
)

// TestWatcherPublishesRetainedMQTT verifies that Watcher.Start causes a
// retained MQTT message to be published to MQTTTopic with the expected
// JSON shape.
func TestWatcherPublishesRetainedMQTT(t *testing.T) {
	// Build a test HTTP server that handles login + summary.
	srv := buildTestServer(t,
		loginOK("watcher-token", 3600),
		summaryOK(800, 2200, 30000),
	)
	defer srv.Close()

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	mc := mqttclient.NewRecordingMockClient()

	cfg := ClientConfig{
		Email:        "watcher@example.com",
		Password:     "pw",
		PollInterval: 10 * time.Millisecond, // fast for tests
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewWatcher(ctx, cfg, mc)

	// Override the HTTP client so the watcher's internal Client uses our test server.
	w.client.http = *srv.Client()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Watcher.Start returned error: %v", err)
	}

	// Wait for at least one publish to arrive (up to 2 seconds).
	deadline := time.Now().Add(2 * time.Second)
	var payloads [][]byte
	for time.Now().Before(deadline) {
		payloads = mc.Published(MQTTTopic)
		if len(payloads) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(payloads) == 0 {
		t.Fatalf("no messages published to %q within timeout", MQTTTopic)
	}

	// Validate the JSON shape of the first published payload.
	var sample PowerSample
	if err := json.Unmarshal(payloads[0], &sample); err != nil {
		t.Fatalf("failed to unmarshal published payload: %v", err)
	}

	if sample.SolarW != 800 {
		t.Errorf("solar_w = %v, want 800", sample.SolarW)
	}
	if sample.DailyWh != 2200 {
		t.Errorf("daily_wh = %v, want 2200", sample.DailyWh)
	}
	if sample.MonthlyWh != 30000 {
		t.Errorf("monthly_wh = %v, want 30000", sample.MonthlyWh)
	}
	if sample.Source != "rest" {
		t.Errorf("source = %q, want \"rest\"", sample.Source)
	}
	if sample.TS.IsZero() {
		t.Error("ts is zero, want a non-zero timestamp")
	}
}

// TestWatcherMQTTRetainedFlag verifies that the publish is indeed sent with
// retained=true.  We use a custom mock that captures the retained flag.
func TestWatcherMQTTRetainedFlag(t *testing.T) {
	srv := buildTestServer(t,
		loginOK("ret-token", 3600),
		summaryOK(300, 600, 9000),
	)
	defer srv.Close()

	origLogin, origSummary := loginURL, summaryURL
	loginURL = srv.URL + "/beemapp/user/login"
	summaryURL = srv.URL + "/beemapp/box/summary"
	defer func() { loginURL = origLogin; summaryURL = origSummary }()

	rm := &retainedRecorder{}

	cfg := ClientConfig{
		Email:        "r@example.com",
		Password:     "pw",
		PollInterval: 10 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewWatcher(ctx, cfg, rm)
	w.client.http = *srv.Client()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Watcher.Start returned error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rm.mu.Lock()
		n := len(rm.calls)
		rm.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if len(rm.calls) == 0 {
		t.Fatal("no Publish calls recorded within timeout")
	}
	call := rm.calls[0]
	if call.topic != MQTTTopic {
		t.Errorf("topic = %q, want %q", call.topic, MQTTTopic)
	}
	if !call.retained {
		t.Error("retained = false, want true")
	}
}

// ---- minimal retained-flag-capturing mock ----

type publishCall struct {
	topic    string
	payload  []byte
	retained bool
}

type retainedRecorder struct {
	mu    sync.Mutex
	calls []publishCall
}

func (r *retainedRecorder) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool, publisherName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	r.calls = append(r.calls, publishCall{topic: topic, payload: cp, retained: retained})
	return nil
}

// The remaining Client interface methods are stubs.
func (r *retainedRecorder) GetServer() string                  { return "mock://localhost:1883" }
func (r *retainedRecorder) BrokerUrl() *url.URL                { u, _ := url.Parse(r.GetServer()); return u }
func (r *retainedRecorder) Id() string                         { return "retained-recorder" }
func (r *retainedRecorder) Start() error                       { return nil }
func (r *retainedRecorder) IsConnected() bool                  { return true }
func (r *retainedRecorder) Close()                             {}
func (r *retainedRecorder) Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error) {
	return nil, nil
}
func (r *retainedRecorder) SubscribeWithHandler(ctx context.Context, topic string, qlen uint, subscriber string, handle func(topic string, payload []byte, subscriber string) error) error {
	return nil
}
func (r *retainedRecorder) SubscribeWithTopic(ctx context.Context, topic string, qlen uint, subscriberName string) (<-chan mqttclient.Message, error) {
	return nil, nil
}
func (r *retainedRecorder) Publisher(ctx context.Context, topic string, qlen uint, qos byte, retained bool, publisherName string) (chan<- []byte, error) {
	return nil, nil
}
