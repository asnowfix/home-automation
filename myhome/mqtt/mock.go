package mqtt

import (
	"context"
	"net/url"
	"sync"
)

type handlerEntry struct {
	subscriber string
	fn         func(topic string, payload []byte, subcriber string) error
}

// RecordingMockClient implements Client for tests.
// It records every Publish call and lets test code inject incoming messages
// via Feed. All methods are safe for concurrent use.
type RecordingMockClient struct {
	mu          sync.Mutex
	published   map[string][][]byte       // topic -> ordered published payloads
	errors      map[string]error          // method name -> error to inject
	subChans    map[string][]chan []byte  // topic -> raw-payload subscriber channels
	subMsgChans map[string][]chan Message // topic -> Message subscriber channels
	handlers    map[string][]handlerEntry // topic -> SubscribeWithHandler entries
}

// NewRecordingMockClient returns a fresh RecordingMockClient ready for use.
func NewRecordingMockClient() *RecordingMockClient {
	return &RecordingMockClient{
		published:   make(map[string][][]byte),
		errors:      make(map[string]error),
		subChans:    make(map[string][]chan []byte),
		subMsgChans: make(map[string][]chan Message),
		handlers:    make(map[string][]handlerEntry),
	}
}

// SetError configures method to return err on its next (and every subsequent) call.
// method is one of: "Publish", "Publisher", "Subscribe", "SubscribeWithHandler", "SubscribeWithTopic".
// Pass nil to clear a previously injected error.
func (m *RecordingMockClient) SetError(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.errors, method)
	} else {
		m.errors[method] = err
	}
}

// Published returns a copy of all payloads published to topic in order.
// Returns nil when nothing has been published to that topic.
func (m *RecordingMockClient) Published(topic string) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.published[topic]
	if len(src) == 0 {
		return nil
	}
	out := make([][]byte, len(src))
	copy(out, src)
	return out
}

// Reset clears all recorded publishes, injected errors, and registered subscriptions.
// Channels already returned to callers are abandoned; test code should discard them.
func (m *RecordingMockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = make(map[string][][]byte)
	m.errors = make(map[string]error)
	m.subChans = make(map[string][]chan []byte)
	m.subMsgChans = make(map[string][]chan Message)
	m.handlers = make(map[string][]handlerEntry)
}

// Feed delivers payload to every subscriber currently listening on topic.
// Sends to buffered channels are non-blocking; a full channel skips the message.
// SubscribeWithHandler callbacks are called synchronously.
func (m *RecordingMockClient) Feed(topic string, payload []byte) {
	m.mu.Lock()
	chans := append([]chan []byte(nil), m.subChans[topic]...)
	msgChans := append([]chan Message(nil), m.subMsgChans[topic]...)
	hs := append([]handlerEntry(nil), m.handlers[topic]...)
	m.mu.Unlock()

	for _, ch := range chans {
		select {
		case ch <- payload:
		default:
		}
	}

	for _, ch := range msgChans {
		msg := &message{topic: topic, payload: payload}
		select {
		case ch <- msg:
		default:
		}
	}

	for _, h := range hs {
		_ = h.fn(topic, payload, h.subscriber)
	}
}

// --- Client interface ---

func (m *RecordingMockClient) GetServer() string { return "mock://localhost:1883" }

func (m *RecordingMockClient) BrokerUrl() *url.URL {
	u, _ := url.Parse(m.GetServer())
	return u
}

func (m *RecordingMockClient) Id() string { return "recording-mock" }

func (m *RecordingMockClient) Subscribe(ctx context.Context, topic string, qlen uint, subscriber string) (<-chan []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errors["Subscribe"]; ok {
		return nil, err
	}
	ch := make(chan []byte, qlen)
	m.subChans[topic] = append(m.subChans[topic], ch)
	return ch, nil
}

func (m *RecordingMockClient) SubscribeWithHandler(ctx context.Context, topic string, qlen uint, subscriber string, handle func(topic string, payload []byte, subcriber string) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errors["SubscribeWithHandler"]; ok {
		return err
	}
	m.handlers[topic] = append(m.handlers[topic], handlerEntry{subscriber: subscriber, fn: handle})
	return nil
}

func (m *RecordingMockClient) SubscribeWithTopic(ctx context.Context, topic string, qlen uint, subscriberName string) (<-chan Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errors["SubscribeWithTopic"]; ok {
		return nil, err
	}
	ch := make(chan Message, qlen)
	m.subMsgChans[topic] = append(m.subMsgChans[topic], ch)
	return ch, nil
}

func (m *RecordingMockClient) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool, publisherName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errors["Publish"]; ok {
		return err
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.published[topic] = append(m.published[topic], cp)
	return nil
}

func (m *RecordingMockClient) Publisher(ctx context.Context, topic string, qlen uint, qos byte, retained bool, publisherName string) (chan<- []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errors["Publisher"]; ok {
		return nil, err
	}
	ch := make(chan []byte, qlen)
	go func() {
		for payload := range ch {
			cp := make([]byte, len(payload))
			copy(cp, payload)
			m.mu.Lock()
			m.published[topic] = append(m.published[topic], cp)
			m.mu.Unlock()
		}
	}()
	return ch, nil
}

func (m *RecordingMockClient) Start() error { return nil }

func (m *RecordingMockClient) IsConnected() bool { return true }

func (m *RecordingMockClient) Close() {}
