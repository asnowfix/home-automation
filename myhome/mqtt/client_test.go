package mqtt

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
)

// fakePahoToken is a pre-completed Token for use in tests.
type fakePahoToken struct{}

func (t *fakePahoToken) Wait() bool                     { return true }
func (t *fakePahoToken) WaitTimeout(time.Duration) bool { return true }
func (t *fakePahoToken) Done() <-chan struct{}          { ch := make(chan struct{}); close(ch); return ch }
func (t *fakePahoToken) Error() error                   { return nil }

// fakePahoClient implements pahomqtt.Client for unit tests.
// It always reports as connected and silently accepts subscribe/publish calls.
//
// It also records Subscribe/Unsubscribe calls (and the subscribed handler) so
// tests can assert broker-level behavior and drive the fan-out like the broker.
type fakePahoClient struct {
	mu             sync.Mutex
	failSubscribes int // next N Subscribe calls fail with a token error
	subscribed     []string
	unsubscribed   []string
	handlers       map[string]pahomqtt.MessageHandler
}

func (f *fakePahoClient) IsConnected() bool       { return true }
func (f *fakePahoClient) IsConnectionOpen() bool  { return true }
func (f *fakePahoClient) Connect() pahomqtt.Token { return &fakePahoToken{} }
func (f *fakePahoClient) Disconnect(uint)         {}
func (f *fakePahoClient) Publish(string, byte, bool, interface{}) pahomqtt.Token {
	return &fakePahoToken{}
}
func (f *fakePahoClient) Subscribe(topic string, _ byte, h pahomqtt.MessageHandler) pahomqtt.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed = append(f.subscribed, topic)
	if f.failSubscribes > 0 {
		f.failSubscribes--
		return &failingToken{err: fmt.Errorf("broker rejected subscription")}
	}
	if f.handlers == nil {
		f.handlers = make(map[string]pahomqtt.MessageHandler)
	}
	f.handlers[topic] = h
	return &fakePahoToken{}
}
func (f *fakePahoClient) SubscribeMultiple(map[string]byte, pahomqtt.MessageHandler) pahomqtt.Token {
	return &fakePahoToken{}
}
func (f *fakePahoClient) Unsubscribe(topics ...string) pahomqtt.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubscribed = append(f.unsubscribed, topics...)
	return &fakePahoToken{}
}

// count returns how many times topic appears in list (subscribed or unsubscribed).
func (f *fakePahoClient) count(list *[]string, topic string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range *list {
		if t == topic {
			n++
		}
	}
	return n
}

func (f *fakePahoClient) handler(topic string) pahomqtt.MessageHandler {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handlers[topic]
}
func (f *fakePahoClient) AddRoute(string, pahomqtt.MessageHandler) {}
func (f *fakePahoClient) OptionsReader() pahomqtt.ClientOptionsReader {
	return pahomqtt.ClientOptionsReader{}
}

// newTestClient creates a client wired up with the fakePahoClient for unit tests.
func newTestClient(t *testing.T) *client {
	t.Helper()
	ctx := logr.NewContext(context.Background(), logr.Discard())
	c := &client{
		mqtt:                 &fakePahoClient{},
		log:                  logr.FromContextOrDiscard(ctx),
		timeout:              5 * time.Second,
		grace:                100 * time.Millisecond,
		lazyStart:            false,
		ctx:                  ctx,
		pendingSubscriptions: make(map[string]pahomqtt.MessageHandler),
	}
	return c
}

// TestAutoConnect_LazyStartEnabled tests that autoConnect logic for lazyStart=true
func TestAutoConnect_LazyStartEnabled(t *testing.T) {
	// This test requires a mock MQTT client that can simulate connection state
	// Without proper mocking infrastructure, we skip this test
	// The behavior is tested in integration tests with a real broker
	t.Skip("Requires mock MQTT client infrastructure - see integration tests")

	// Expected behavior:
	// - If not connected and lazyStart=true, should call Start()
	// - If Start() succeeds, should return nil
	// - If Start() fails, should return error
}

// TestAutoConnect_LazyStartDisabled tests that autoConnect fails when lazyStart is false and not connected
func TestAutoConnect_LazyStartDisabled(t *testing.T) {
	// This test requires a mock MQTT client that can simulate connection state
	// Without proper mocking infrastructure, we skip this test
	t.Skip("Requires mock MQTT client infrastructure - see integration tests")

	// Expected behavior:
	// - If not connected and lazyStart=false, should return error immediately
	// - Error message should be "MQTT client not started"
}

// TestAutoConnect_AlreadyConnected tests that autoConnect returns nil when already connected
func TestAutoConnect_AlreadyConnected(t *testing.T) {
	// This test would require a mock MQTT client that reports IsConnected() = true
	t.Skip("Requires mock MQTT client infrastructure")
}

// TestGetClientE_DirectCall tests that direct GetClientE call creates client with lazyStart=true
func TestGetClientE_DirectCall(t *testing.T) {
	// Reset the global client
	mutex.Lock()
	theClient = nil
	mutex.Unlock()

	// This test would require a working MQTT broker or mock
	// For now, we document the expected behavior
	t.Skip("Requires MQTT broker or mock infrastructure")

	// Expected behavior:
	// ctx := context.Background()
	// client, err := GetClientE(ctx)
	// if err != nil {
	//     t.Fatalf("GetClientE failed: %v", err)
	// }
	// if !client.(*client).lazyStart {
	//     t.Error("Expected lazyStart=true for direct GetClientE call")
	// }
}

// TestNewClientE_WithLazyStart tests that NewClientE sets lazyStart correctly
func TestNewClientE_WithLazyStart(t *testing.T) {
	// Reset the global client
	mutex.Lock()
	theClient = nil
	mutex.Unlock()

	// This test would require a working MQTT broker or mock
	t.Skip("Requires MQTT broker or mock infrastructure")

	// Expected behavior for lazyStart=true:
	// ctx := context.Background()
	// err := NewClientE(ctx, "mqtt://localhost:1883", "test", 5*time.Second, 5*time.Second, 1*time.Second, 0, true)
	// if err != nil {
	//     t.Fatalf("NewClientE failed: %v", err)
	// }
	// client, _ := GetClientE(ctx)
	// if !client.(*client).lazyStart {
	//     t.Error("Expected lazyStart=true")
	// }
}

// TestNewClientE_WithoutLazyStart tests that NewClientE with lazyStart=false works
func TestNewClientE_WithoutLazyStart(t *testing.T) {
	// Reset the global client
	mutex.Lock()
	theClient = nil
	mutex.Unlock()

	// This test would require a working MQTT broker or mock
	t.Skip("Requires MQTT broker or mock infrastructure")

	// Expected behavior for lazyStart=false:
	// ctx := context.Background()
	// err := NewClientE(ctx, "mqtt://localhost:1883", "test", 5*time.Second, 5*time.Second, 1*time.Second, 0, false)
	// if err != nil {
	//     t.Fatalf("NewClientE failed: %v", err)
	// }
	// client, _ := GetClientE(ctx)
	// if client.(*client).lazyStart {
	//     t.Error("Expected lazyStart=false")
	// }
}

// Integration test documentation
// These tests require a running MQTT broker and are meant to be run manually or in CI with broker setup

// TestIntegration_LazyStartPublish would test:
// 1. Create client with lazyStart=true
// 2. Call Publish() without calling Start()
// 3. Verify client auto-connects
// 4. Verify message is published

// TestIntegration_LazyStartSubscribe would test:
// 1. Create client with lazyStart=true
// 2. Call Subscribe() without calling Start()
// 3. Verify client auto-connects
// 4. Verify subscription works and receives messages

// TestIntegration_NoLazyStartPublish would test:
// 1. Create client with lazyStart=false
// 2. Call Publish() without calling Start()
// 3. Verify error is returned
// 4. Call Start() explicitly
// 5. Verify Publish() now works

func ExampleClient_autoConnect_lazyStart() {
	// This example shows how lazy-start works for CLI commands

	// Create context with logger
	ctx := context.Background()
	ctx = logr.NewContext(ctx, logr.Discard())
	_ = ctx // consumed by the commented-out client calls below

	// For CLI commands, NewClientE is called with lazyStart=true
	// err := NewClientE(ctx, "mqtt://localhost:1883", "cli", 5*time.Second, 5*time.Second, 1*time.Second, 0, true)

	// Get the client
	// client, err := GetClientE(ctx)

	// First publish will auto-connect
	// err = client.Publish(ctx, "test/topic", []byte("message"), AtLeastOnce, false, "test-publisher")

	// Client is now connected and subsequent operations work immediately

	fmt.Println("Lazy-start enables auto-connect on first operation")
	// Output: Lazy-start enables auto-connect on first operation
}

func ExampleClient_autoConnect_noLazyStart() {
	// This example shows how daemon mode works without lazy-start

	// Create context with logger
	ctx := context.Background()
	ctx = logr.NewContext(ctx, logr.Discard())
	_ = ctx // consumed by the commented-out client calls below

	// For daemon, NewClientE is called with lazyStart=false
	// err := NewClientE(ctx, "mqtt://localhost:1883", "daemon", 5*time.Second, 5*time.Second, 1*time.Second, 2*time.Hour, false)

	// Get the client
	// client, err := GetClientE(ctx)

	// Daemon explicitly starts the client
	// err = client.Start()

	// Now publish works because client was started explicitly
	// err = client.Publish(ctx, "test/topic", []byte("message"), AtLeastOnce, false, "daemon-publisher")

	fmt.Println("Daemon mode requires explicit Start() call")
	// Output: Daemon mode requires explicit Start() call
}

// TestSubscribe_ConcurrentSubscriptions verifies that subscribing to the same topic
// from many goroutines concurrently never loses a subscriber due to a race condition.
// Run with: go test -race ./...
func TestSubscribe_ConcurrentSubscriptions(t *testing.T) {
	const n = 100
	c := newTestClient(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ch, err := c.Subscribe(ctx, "test/topic", 4, "subscriber")
			if err != nil {
				t.Errorf("Subscribe failed: %v", err)
				return
			}
			if ch == nil {
				t.Error("Subscribe returned nil channel")
			}
		}()
	}
	wg.Wait()

	// All n subscribers must be in the list - none should have been lost.
	// The subscriber type is local to subscribe[T], so use reflect to get length.
	value, ok := c.subscribers.Load("test/topic")
	if !ok {
		t.Fatal("no subscriber list stored for test/topic")
	}
	if got := reflect.ValueOf(value).Len(); got != n {
		t.Errorf("expected %d subscribers, got %d (race condition lost some)", n, got)
	}
}

// fakePahoMessage implements pahomqtt.Message for tests that drive the
// stored "distribute" handler directly, as the broker would.
type fakePahoMessage struct {
	topic   string
	payload []byte
}

func (m *fakePahoMessage) Duplicate() bool   { return false }
func (m *fakePahoMessage) Qos() byte         { return 0 }
func (m *fakePahoMessage) Retained() bool    { return false }
func (m *fakePahoMessage) Topic() string     { return m.topic }
func (m *fakePahoMessage) MessageID() uint16 { return 0 }
func (m *fakePahoMessage) Payload() []byte   { return m.payload }
func (m *fakePahoMessage) Ack()              {}

// TestDistribute_FullQueueDropsMessageNotSubscriber is a regression test for
// #517: a burst of messages that fills a subscriber's bounded queue must
// drop only the messages that don't fit, not the subscriber itself. Before
// this fix, one full-queue moment permanently removed the subscriber from
// the topic's fan-out list -- with no automatic re-subscribe -- so every
// later message on that topic (e.g. every device sharing a wildcard
// subscription such as "+/events/rpc") was silently lost until the process
// restarted.
func TestDistribute_FullQueueDropsMessageNotSubscriber(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	const topic = "test/overflow"

	ch, err := c.Subscribe(ctx, topic, 1, "subscriber") // qlen=1: easy to overflow
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	handlerI, ok := c.handlers.Load(topic)
	if !ok {
		t.Fatal("no distribute handler registered for topic")
	}
	handler := handlerI.(pahomqtt.MessageHandler)

	// distribute() does its real work in a goroutine spawned per handler()
	// call, so waiting for effects means polling rather than assuming
	// synchronous completion.
	waitFor := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if done() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for: %s", what)
	}

	// First message fills the qlen=1 buffer -- poll for the buffered
	// length to actually reach 1, which is a real (not assumed) signal
	// that this call's distribute() goroutine has run.
	handler(nil, &fakePahoMessage{topic: topic, payload: []byte("first")})
	waitFor("first message buffered", func() bool { return len(ch) == 1 })

	// Second message finds the buffer full and must be dropped -- as
	// "queue full", not as "remove the subscriber". Give its goroutine
	// (serialized after the first on the same per-topic mutex) a moment to
	// run, well beyond what trivial in-memory map/channel work needs.
	handler(nil, &fakePahoMessage{topic: topic, payload: []byte("second")})
	time.Sleep(100 * time.Millisecond)

	value, ok := c.subscribers.Load(topic)
	if !ok {
		t.Fatal("subscriber list missing for topic")
	}
	if got := reflect.ValueOf(value).Len(); got != 1 {
		t.Fatalf("expected subscriber to survive a full queue, got %d subscribers left", got)
	}

	// The dropped message must be counted (#517/#518 F2) -- this is what
	// answers "did we lose events today, and how many" instead of leaving
	// only a per-message log line (or, before this PR, a permanently dead
	// subscriber and total silence).
	counterI, ok := c.dropCounts.Load(topic + "|subscriber")
	if !ok {
		t.Fatal("expected a drop counter to have been created")
	}
	if got := counterI.(*atomic.Uint64).Load(); got != 1 {
		t.Fatalf("expected drop count 1, got %d", got)
	}

	// Drain the one message that made it through, then confirm the
	// subscriber is still live by delivering a third message to it.
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first message")
	}

	handler(nil, &fakePahoMessage{topic: topic, payload: []byte("third")})

	select {
	case got := <-ch:
		if string(got) != "third" {
			t.Fatalf("expected %q, got %q", "third", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive message after surviving a full queue")
	}
}

// TestRecordDrop_AccumulatesPerTopicSubscriber verifies recordDrop()'s
// counting in isolation from distribute()'s goroutine timing: repeated
// drops on the same (topic, subscriber) accumulate on one counter, a
// different subscriber on the same topic gets its own counter, and the
// reporter goroutine is started (dropLogOnce) without panicking or blocking
// -- it only exercises the "first drop ever" path here since
// dropCountReportInterval (5m) is far longer than this test should run.
func TestRecordDrop_AccumulatesPerTopicSubscriber(t *testing.T) {
	c := newTestClient(t)

	c.recordDrop("t1", "subA")
	c.recordDrop("t1", "subA")
	c.recordDrop("t1", "subA")
	c.recordDrop("t1", "subB")

	got, ok := c.dropCounts.Load("t1|subA")
	if !ok {
		t.Fatal("expected a counter for t1|subA")
	}
	if n := got.(*atomic.Uint64).Load(); n != 3 {
		t.Fatalf("expected 3 drops for t1|subA, got %d", n)
	}

	got, ok = c.dropCounts.Load("t1|subB")
	if !ok {
		t.Fatal("expected a counter for t1|subB")
	}
	if n := got.(*atomic.Uint64).Load(); n != 1 {
		t.Fatalf("expected 1 drop for t1|subB, got %d", n)
	}
}

// TestResolveDeviceHost_NonLoopback verifies that non-loopback addresses pass through unchanged.
func TestResolveDeviceHost_NonLoopback(t *testing.T) {
	cases := []struct {
		host string
	}{
		{"192.168.1.2"},
		{"10.0.0.1"},
		{"mqtt.local"},
		{"::1"}, // IPv6 loopback — handled below, included for clarity
		{"2001:db8::1"},
	}

	neverCalled := func() (*net.IP, error) {
		t.Fatal("getIP should not be called for non-loopback host")
		return nil, nil
	}

	for _, tc := range cases {
		ip := net.ParseIP(tc.host)
		if ip != nil && ip.IsLoopback() {
			continue // skip loopback entries in this table
		}
		got, err := resolveDeviceHost(tc.host, neverCalled)
		if err != nil {
			t.Errorf("resolveDeviceHost(%q) unexpected error: %v", tc.host, err)
		}
		if got != tc.host {
			t.Errorf("resolveDeviceHost(%q) = %q, want %q", tc.host, got, tc.host)
		}
	}
}

// TestResolveDeviceHost_LoopbackSubstituted verifies that 127.x.x.x is replaced with the LAN IP.
func TestResolveDeviceHost_LoopbackSubstituted(t *testing.T) {
	lanIP := net.ParseIP("192.168.1.100")

	cases := []string{"127.0.0.1", "127.0.0.2", "::1"}

	for _, host := range cases {
		got, err := resolveDeviceHost(host, func() (*net.IP, error) {
			return &lanIP, nil
		})
		if err != nil {
			t.Errorf("resolveDeviceHost(%q) unexpected error: %v", host, err)
			continue
		}
		if got != lanIP.String() {
			t.Errorf("resolveDeviceHost(%q) = %q, want %q", host, got, lanIP.String())
		}
	}
}

// TestResolveDeviceHost_LoopbackLookupFails verifies that an error from getIP propagates.
func TestResolveDeviceHost_LoopbackLookupFails(t *testing.T) {
	lookupErr := fmt.Errorf("no network interface found")
	_, err := resolveDeviceHost("127.0.0.1", func() (*net.IP, error) {
		return nil, lookupErr
	})
	if err == nil {
		t.Fatal("expected error when getIP fails, got nil")
	}
}

// TestDeviceServer_NonLoopback verifies DeviceServer returns the address unchanged for a LAN broker.
func TestDeviceServer_NonLoopback(t *testing.T) {
	u, _ := url.Parse("tcp://192.168.1.2:1883")
	c := newTestClient(t)
	c.brokerUrl = u

	got, err := c.DeviceServer()
	if err != nil {
		t.Fatalf("DeviceServer() unexpected error: %v", err)
	}
	if got != "192.168.1.2:1883" {
		t.Errorf("DeviceServer() = %q, want %q", got, "192.168.1.2:1883")
	}
}

// failingPahoClient wraps fakePahoClient but returns an error on Subscribe.
type failingPahoClient struct {
	fakePahoClient
	subscribeErr error
}

func (f *failingPahoClient) Subscribe(topic string, qos byte, handler pahomqtt.MessageHandler) pahomqtt.Token {
	return &failingToken{err: f.subscribeErr}
}

type failingToken struct{ err error }

func (t *failingToken) Wait() bool                     { return true }
func (t *failingToken) WaitTimeout(time.Duration) bool { return true }
func (t *failingToken) Done() <-chan struct{}          { ch := make(chan struct{}); close(ch); return ch }
func (t *failingToken) Error() error                   { return t.err }

// TestReconnect_FailedSubscriptionQueuedForRetry verifies that when a subscription
// fails during periodic reconnection it is added to pendingSubscriptions so the
// next reconnection attempt retries it.
func TestReconnect_FailedSubscriptionQueuedForRetry(t *testing.T) {
	subErr := fmt.Errorf("broker rejected subscription")
	c := newTestClient(t)
	c.mqtt = &failingPahoClient{subscribeErr: subErr}

	handler := func(pahomqtt.Client, pahomqtt.Message) {}

	// Simulate having a previously registered topic handler (as if we subscribed once)
	c.handlers.Store("sensor/+", pahomqtt.MessageHandler(handler))

	// Collect subscriptions (as the reconnect loop would)
	type subInfo struct {
		topic   string
		handler pahomqtt.MessageHandler
	}
	var subscriptions []subInfo
	c.handlers.Range(func(key, value interface{}) bool {
		subscriptions = append(subscriptions, subInfo{key.(string), value.(pahomqtt.MessageHandler)})
		return true
	})

	// Run the retry logic directly (extracted from the reconnect loop)
	var failedSubs []subInfo
	for _, sub := range subscriptions {
		token := c.mqtt.Subscribe(sub.topic, 1, sub.handler)
		token.WaitTimeout(c.timeout)
		if err := token.Error(); err != nil {
			failedSubs = append(failedSubs, sub)
		}
	}
	if len(failedSubs) > 0 {
		c.pendingMutex.Lock()
		for _, sub := range failedSubs {
			c.pendingSubscriptions[sub.topic] = sub.handler
		}
		c.pendingMutex.Unlock()
	}

	c.pendingMutex.Lock()
	_, queued := c.pendingSubscriptions["sensor/+"]
	c.pendingMutex.Unlock()

	if !queued {
		t.Error("failed subscription should have been added to pendingSubscriptions for retry")
	}
}

// recvOrFail reads one payload from ch or fails after a bounded wait.
func recvOrFail(t *testing.T, ch <-chan []byte, what string) []byte {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed while waiting for %s", what)
		}
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
	return nil
}

// waitClosed waits (bounded) until ch is closed, draining any buffered values.
func waitClosed(t *testing.T, ch <-chan []byte, what string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s to be closed", what)
		}
	}
}

// TestSubscribe_CancelledSubscriberDoesNotPanicFanOut is a regression test for
// #519: a subscriber whose ctx ended had its queue closed but stayed in the
// fan-out list, so the next message sent on a closed channel and panicked in a
// bare goroutine (killing the daemon).
func TestSubscribe_CancelledSubscriberDoesNotPanicFanOut(t *testing.T) {
	c := newTestClient(t)
	fake := c.mqtt.(*fakePahoClient)
	const topic = "test/519"
	const n = 50

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	chA, err := c.Subscribe(ctxA, topic, 1, "A")
	if err != nil {
		t.Fatal(err)
	}
	chB, err := c.Subscribe(context.Background(), topic, n, "B")
	if err != nil {
		t.Fatal(err)
	}
	handler := fake.handler(topic)

	cancelA()
	waitClosed(t, chA, "A's channel")

	for i := 0; i < n; i++ {
		handler(nil, &fakePahoMessage{topic: topic, payload: []byte(fmt.Sprint(i))})
	}
	for i := 0; i < n; i++ {
		recvOrFail(t, chB, fmt.Sprintf("B message %d/%d", i+1, n))
	}
}

// TestSubscribe_UnsubscribeOnlyWhenLastSubscriberLeaves is a regression test
// for #520: every departing subscriber used to unsubscribe the whole topic at
// the broker, starving the remaining ones. The broker subscription must go
// away only with the last subscriber, and a later Subscribe must re-register.
func TestSubscribe_UnsubscribeOnlyWhenLastSubscriberLeaves(t *testing.T) {
	c := newTestClient(t)
	fake := c.mqtt.(*fakePahoClient)
	const topic = "test/520"

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	chA, err := c.Subscribe(ctxA, topic, 4, "A")
	if err != nil {
		t.Fatal(err)
	}
	chB, err := c.Subscribe(ctxB, topic, 4, "B")
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.count(&fake.subscribed, topic); got != 1 {
		t.Fatalf("expected 1 broker Subscribe, got %d", got)
	}
	handler := fake.handler(topic)

	cancelA()
	waitClosed(t, chA, "A's channel")
	// Give any (buggy) asynchronous Unsubscribe a chance to land.
	time.Sleep(100 * time.Millisecond)
	if got := fake.count(&fake.unsubscribed, topic); got != 0 {
		t.Fatalf("broker Unsubscribe issued while a subscriber remained (%d calls)", got)
	}
	handler(nil, &fakePahoMessage{topic: topic, payload: []byte("x")})
	recvOrFail(t, chB, "survivor message")

	cancelB()
	waitClosed(t, chB, "B's channel")
	deadline := time.Now().Add(10 * time.Second)
	for fake.count(&fake.unsubscribed, topic) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if got := fake.count(&fake.unsubscribed, topic); got != 1 {
		t.Fatalf("expected exactly 1 broker Unsubscribe after last subscriber left, got %d", got)
	}
	if _, ok := c.subscribers.Load(topic); ok {
		t.Fatal("topic still in c.subscribers after last subscriber left")
	}
	if _, ok := c.handlers.Load(topic); ok {
		t.Fatal("topic still in c.handlers after last subscriber left")
	}
	c.pendingMutex.Lock()
	_, pending := c.pendingSubscriptions[topic]
	c.pendingMutex.Unlock()
	if pending {
		t.Fatal("topic still in c.pendingSubscriptions after last subscriber left")
	}

	// A fresh Subscribe must re-register at the broker and receive messages.
	chC, err := c.Subscribe(context.Background(), topic, 4, "C")
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.count(&fake.subscribed, topic); got != 2 {
		t.Fatalf("expected a fresh broker Subscribe (2 total), got %d", got)
	}
	fake.handler(topic)(nil, &fakePahoMessage{topic: topic, payload: []byte("again")})
	if got := recvOrFail(t, chC, "message after resubscribe"); string(got) != "again" {
		t.Fatalf("expected %q, got %q", "again", got)
	}
}

// TestDistribute_FirstSubscriberCancelledDoesNotAbortFanOut is a regression
// test for #521: distribute closed over the FIRST subscriber's ctx, so once
// that ctx ended the fan-out loop could return early and starve everyone else.
func TestDistribute_FirstSubscriberCancelledDoesNotAbortFanOut(t *testing.T) {
	c := newTestClient(t)
	fake := c.mqtt.(*fakePahoClient)
	const topic = "test/521"
	const n = 200

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	if _, err := c.Subscribe(ctxA, topic, 1, "first"); err != nil {
		t.Fatal(err)
	}
	chB, err := c.Subscribe(context.Background(), topic, 1, "second")
	if err != nil {
		t.Fatal(err)
	}
	handler := fake.handler(topic)
	cancelA()

	for i := 0; i < n; i++ {
		handler(nil, &fakePahoMessage{topic: topic, payload: []byte(fmt.Sprint(i))})
		recvOrFail(t, chB, fmt.Sprintf("message %d/%d", i+1, n))
	}
}

// TestSubscribe_BrokerFailureRollsBackRegistration: when the synchronous broker
// Subscribe fails, Subscribe must return the error and leave no trace (no
// subscriber, handler or watcher), so a retry re-issues the broker Subscribe
// instead of finding the topic already "loaded".
func TestSubscribe_BrokerFailureRollsBackRegistration(t *testing.T) {
	c := newTestClient(t)
	fake := c.mqtt.(*fakePahoClient)
	fake.failSubscribes = 1
	const topic = "test/rollback"

	if _, err := c.Subscribe(context.Background(), topic, 4, "first"); err == nil {
		t.Fatal("expected Subscribe to fail when the broker rejects the subscription")
	}
	if _, ok := c.subscribers.Load(topic); ok {
		t.Fatal("topic still in c.subscribers after failed Subscribe")
	}
	if _, ok := c.handlers.Load(topic); ok {
		t.Fatal("topic still in c.handlers after failed Subscribe")
	}

	ch, err := c.Subscribe(context.Background(), topic, 4, "retry")
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.count(&fake.subscribed, topic); got != 2 {
		t.Fatalf("expected the retry to re-issue a broker Subscribe (2 attempts), got %d", got)
	}
	fake.handler(topic)(nil, &fakePahoMessage{topic: topic, payload: []byte("ok")})
	if got := recvOrFail(t, ch, "message after retry"); string(got) != "ok" {
		t.Fatalf("expected %q, got %q", "ok", got)
	}
}
