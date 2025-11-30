package mqtt

import (
	"context"
	"net/url"
)

// MockClient is a minimal MQTT client implementation for testing
type MockClient struct {
	server string
}

// NewMockClient creates a new mock MQTT client
func NewMockClient() *MockClient {
	return &MockClient{
		server: "mock://localhost:1883",
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
	// Return empty channel - no messages will be sent
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

func (m *MockClient) Publish(ctx context.Context, topic string, msg []byte, qos byte, retained bool, publisherName string) error {
	// No-op for mock
	return nil
}
