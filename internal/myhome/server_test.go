package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"

	"github.com/go-logr/logr"
)

// stubServer is a minimal Server implementation for testing.
// All MethodE calls return method (or nil, err if err is non-nil).
type stubServer struct {
	method *Method
	err    error
}

func (s *stubServer) MethodE(_ Verb) (*Method, error) {
	return s.method, s.err
}

// noopAction returns "ok" without inspecting params.
func noopAction(_ context.Context, _ json.RawMessage) (any, error) { return "ok", nil }

// newServerCtx returns a context with a discarded logr.Logger.
func newServerCtx() context.Context {
	return logr.NewContext(context.Background(), logr.Discard())
}

// waitPublished polls mc.Published(topic) until at least one message arrives
// or the 200 ms deadline elapses.
func waitPublished(t *testing.T, mc *mqtt.RecordingMockClient, topic string) []byte {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if msgs := mc.Published(topic); len(msgs) > 0 {
			return msgs[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout: no message published to %q within 200 ms", topic)
	return nil
}

// feedRequest marshals req and delivers it to ServerTopic() via mc.Feed.
func feedRequest(t *testing.T, mc *mqtt.RecordingMockClient, req request) {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("feedRequest marshal: %v", err)
	}
	mc.Feed(ServerTopic(), payload)
}

// --- Tests ---

// TestNewServerE_SubscribesToServerTopic verifies that NewServerE subscribes
// before returning: a message fed to ServerTopic() immediately after
// construction must reach the server and produce a response.
func TestNewServerE_SubscribesToServerTopic(t *testing.T) {
	ctx, cancel := context.WithCancel(newServerCtx())
	defer cancel()

	mc := mqtt.NewRecordingMockClient()
	handler := &stubServer{
		method: &Method{Name: "test.noop", Action: noopAction},
	}

	_, err := NewServerE(ctx, mc, handler)
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	const src = "sub-client"
	req := request{
		Dialog: Dialog{Id: "sub-1", Src: src, Dst: InstanceName},
		Method: "test.noop",
	}
	feedRequest(t, mc, req)
	waitPublished(t, mc, ClientTopic(src)) // panics via t.Fatalf if nothing arrives
}

// TestServer_DispatchKnownMethod verifies that a well-formed request reaches
// the handler and that the response is published to ClientTopic(src) with
// the correct dialog fields.
func TestServer_DispatchKnownMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(newServerCtx())
	defer cancel()

	mc := mqtt.NewRecordingMockClient()
	called := make(chan struct{}, 1)
	handler := &stubServer{
		method: &Method{
			Name: "test.dispatch",
			Action: func(_ context.Context, _ json.RawMessage) (any, error) {
				called <- struct{}{}
				return "dispatched", nil
			},
		},
	}

	_, err := NewServerE(ctx, mc, handler)
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	const src = "dispatch-client"
	req := request{
		Dialog: Dialog{Id: "disp-1", Src: src, Dst: InstanceName},
		Method: "test.dispatch",
	}
	feedRequest(t, mc, req)

	raw := waitPublished(t, mc, ClientTopic(src))

	select {
	case <-called:
	default:
		t.Error("handler Action was not called")
	}

	var res response
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Error != nil {
		t.Errorf("unexpected error in response: %+v", res.Error)
	}
	if res.Id != "disp-1" {
		t.Errorf("response id: got %q, want %q", res.Id, "disp-1")
	}
	if res.Dst != src {
		t.Errorf("response Dst: got %q, want %q", res.Dst, src)
	}
	if res.Src != mc.Id() {
		t.Errorf("response Src: got %q, want %q", res.Src, mc.Id())
	}
}

// TestServer_UnknownMethod_ReturnsError verifies that when the handler returns
// an error from MethodE, the server publishes an error response to
// ClientTopic(src) with a non-nil Error field.
func TestServer_UnknownMethod_ReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(newServerCtx())
	defer cancel()

	mc := mqtt.NewRecordingMockClient()
	handler := &stubServer{err: fmt.Errorf("method not found")}

	_, err := NewServerE(ctx, mc, handler)
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	const src = "err-client"
	req := request{
		Dialog: Dialog{Id: "err-1", Src: src, Dst: InstanceName},
		Method: "nonexistent.method",
	}
	feedRequest(t, mc, req)

	raw := waitPublished(t, mc, ClientTopic(src))

	var res response
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if res.Error.Code != 1 {
		t.Errorf("error code: got %d, want 1", res.Error.Code)
	}
}

// TestServer_InvalidJSON_ReturnsError verifies that a malformed JSON payload
// results in an error response published to ClientTopic("") (req.Src is empty
// when unmarshal fails entirely).
func TestServer_InvalidJSON_ReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(newServerCtx())
	defer cancel()

	mc := mqtt.NewRecordingMockClient()
	handler := &stubServer{err: fmt.Errorf("unused")}

	_, err := NewServerE(ctx, mc, handler)
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	mc.Feed(ServerTopic(), []byte("not-valid-json{{{"))

	// When unmarshal fails, req.Src is "" so fail() publishes to ClientTopic("").
	raw := waitPublished(t, mc, ClientTopic(""))
	var res response
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected error field in response")
	}
}

// TestServer_InvalidDialog_ReturnsError verifies that a request with an empty
// Id field (invalid dialog) results in an error response.
func TestServer_InvalidDialog_ReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(newServerCtx())
	defer cancel()

	mc := mqtt.NewRecordingMockClient()
	handler := &stubServer{err: fmt.Errorf("unused")}

	_, err := NewServerE(ctx, mc, handler)
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	const src = "bad-dialog-client"
	// Id is empty — ValidateDialog rejects this.
	req := request{
		Dialog: Dialog{Id: "", Src: src, Dst: InstanceName},
		Method: "temperature.get",
	}
	feedRequest(t, mc, req)

	raw := waitPublished(t, mc, ClientTopic(src))
	var res response
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected error in response for invalid dialog")
	}
}

// TestServer_ContextCancellation verifies that cancelling the context does not
// deadlock: Feed should complete and the server goroutine should stop.
func TestServer_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(newServerCtx())

	mc := mqtt.NewRecordingMockClient()
	// Use an error handler so that if the goroutine races to process one more
	// message before exiting, fail() is called instead of dereferencing a nil method.
	handler := &stubServer{err: fmt.Errorf("cancelled")}

	_, err := NewServerE(ctx, mc, handler)
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	cancel()

	// Feed after cancellation; the buffered channel (cap 16) accepts it without
	// blocking even if the goroutine has already exited.
	done := make(chan struct{})
	go func() {
		defer close(done)
		mc.Feed(ServerTopic(), []byte(
			`{"id":"x","src":"cancel-client","dst":"myhome","method":"temperature.get"}`,
		))
	}()

	select {
	case <-done:
		// Feed completed without blocking — no deadlock.
	case <-time.After(500 * time.Millisecond):
		t.Error("Feed after context cancellation deadlocked")
	}
}

// TestServer_SlowHandlerDoesNotBlockConcurrentRPC proves that the server
// dispatches messages concurrently: a request whose handler blocks until
// released must not delay the response to a second, unrelated, fast request
// fed immediately afterward. Before per-message dispatch, the server
// processed inMsg strictly sequentially in one goroutine, so a slow handler
// head-of-line-blocked every other RPC (see #361).
func TestServer_SlowHandlerDoesNotBlockConcurrentRPC(t *testing.T) {
	ctx, cancel := context.WithCancel(newServerCtx())
	defer cancel()

	mc := mqtt.NewRecordingMockClient()

	release := make(chan struct{})
	slowStarted := make(chan struct{})
	handler := &stubServer{
		method: &Method{
			Name: "test.slow-or-fast",
			Action: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var p struct {
					Slow bool `json:"slow"`
				}
				_ = json.Unmarshal(raw, &p)
				if p.Slow {
					close(slowStarted)
					select {
					case <-release:
					case <-ctx.Done():
					}
					return "slow-done", nil
				}
				return "fast-done", nil
			},
		},
	}

	_, err := NewServerE(ctx, mc, handler)
	if err != nil {
		t.Fatalf("NewServerE: %v", err)
	}

	const slowSrc = "slow-client"
	const fastSrc = "fast-client"

	feedRequest(t, mc, request{
		Dialog: Dialog{Id: "slow-1", Src: slowSrc, Dst: InstanceName},
		Method: "test.slow-or-fast",
		Params: json.RawMessage(`{"slow":true}`),
	})

	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow handler never started")
	}

	feedRequest(t, mc, request{
		Dialog: Dialog{Id: "fast-1", Src: fastSrc, Dst: InstanceName},
		Method: "test.slow-or-fast",
		Params: json.RawMessage(`{"slow":false}`),
	})

	// The fast response must arrive well before the slow handler is released.
	fastRaw := waitPublished(t, mc, ClientTopic(fastSrc))
	var fastRes response
	if err := json.Unmarshal(fastRaw, &fastRes); err != nil {
		t.Fatalf("unmarshal fast response: %v", err)
	}
	var fastResult string
	if err := json.Unmarshal(fastRes.Result, &fastResult); err != nil {
		t.Fatalf("unmarshal fast result: %v", err)
	}
	if fastResult != "fast-done" {
		t.Errorf("fast result: got %q, want %q", fastResult, "fast-done")
	}

	close(release)
	waitPublished(t, mc, ClientTopic(slowSrc))
}
