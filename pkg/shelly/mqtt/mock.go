package mqtt

import (
	"context"
	"net/url"
	"sync"
)

// MockClient is a minimal in-process MQTT client implementation for testing.
//
// Subscribe/Publish are wired together in-memory: Publish delivers a copy of
// the payload to every channel currently registered (via Subscribe) for the
// exact same topic. This lets tests exercise a script's MQTT.subscribe
// callback by calling Publish with the topic/payload it expects — e.g.
// simulating the daemon's retained `myhome/energy/solar/available` message.
// There is no wildcard ("+"/"#") matching; topics must match exactly, which
// is sufficient for the fixed, non-wildcard topics pool-pump.js and similar
// scripts use.
type MockClient struct {
	server string

	mu   sync.Mutex
	subs map[string][]chan []byte // topic -> subscriber channels
}

// NewMockClient creates a new mock MQTT client
func NewMockClient() *MockClient {
	return &MockClient{
		server: "mock://localhost:1883",
		subs:   make(map[string][]chan []byte),
	}
}

func (m *MockClient) GetServer() string {
	return m.server
}

func (m *MockClient) BrokerUrl() *url.URL {
	u, _ := url.Parse(m.server)
	return u
}

func (m *MockClient) Id() string {
	return "mock-client"
}

func (m *MockClient) Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error) {
	ch := make(chan []byte, qlen)
	m.mu.Lock()
	m.subs[topic] = append(m.subs[topic], ch)
	m.mu.Unlock()
	return ch, nil
}

func (m *MockClient) SubscribeWithHandler(ctx context.Context, topic string, qlen uint, subscriber string, handle func(topic string, payload []byte, subscriber string) error) error {
	// No-op for mock
	return nil
}

func (m *MockClient) Publisher(ctx context.Context, topic string, qlen uint, qos byte, retained bool, publisherName string) (chan<- []byte, error) {
	ch := make(chan []byte, qlen)
	// Drain channel in background
	go func() {
		for range ch {
			// Discard messages
		}
	}()
	return ch, nil
}

// Publish delivers msg to every channel currently subscribed to topic (see
// MockClient doc comment). Delivery is best-effort/non-blocking: a full
// subscriber channel skips the message rather than blocking the publisher,
// matching real broker QoS-0-ish behavior for this test double.
func (m *MockClient) Publish(ctx context.Context, topic string, msg []byte, qos byte, retained bool, publisherName string) error {
	m.mu.Lock()
	chans := append([]chan []byte(nil), m.subs[topic]...)
	m.mu.Unlock()

	for _, ch := range chans {
		cp := make([]byte, len(msg))
		copy(cp, msg)
		select {
		case ch <- cp:
		default:
		}
	}
	return nil
}
