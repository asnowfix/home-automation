package myhome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"

	"github.com/go-logr/logr"
)

// fakeRPCTransport is a minimal mqtt.Client implementation for client.go
// whitebox tests. Unlike mqtt.RecordingMockClient it hands the test direct,
// synchronous access to the raw request/response channels used by the RPC
// client, so tests can drive exact interleavings deterministically (no
// polling, no time.Sleep) instead of racing against an internal drain
// goroutine.
type fakeRPCTransport struct {
	id string

	mu           sync.Mutex
	subscribeErr error
	publisherErr error

	// reqCh is returned as the Publisher() channel: the client under test
	// writes outgoing requests here and the test reads them.
	reqCh chan []byte
	// respCh is returned as the Subscribe() channel: the test writes
	// simulated server responses here and the client's dispatch goroutine
	// reads them.
	respCh chan []byte
}

func newFakeRPCTransport(id string) *fakeRPCTransport {
	return &fakeRPCTransport{
		id:     id,
		reqCh:  make(chan []byte, 8),
		respCh: make(chan []byte, 8),
	}
}

func (f *fakeRPCTransport) GetServer() string             { return "fake://broker" }
func (f *fakeRPCTransport) BrokerUrl() *url.URL           { u, _ := url.Parse(f.GetServer()); return u }
func (f *fakeRPCTransport) DeviceServer() (string, error) { return "fake:1883", nil }
func (f *fakeRPCTransport) Id() string                    { return f.id }
func (f *fakeRPCTransport) Start() error                  { return nil }
func (f *fakeRPCTransport) IsConnected() bool             { return true }
func (f *fakeRPCTransport) Close()                        {}

func (f *fakeRPCTransport) Subscribe(_ context.Context, _ string, _ uint, _ string) (<-chan []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subscribeErr != nil {
		return nil, f.subscribeErr
	}
	return f.respCh, nil
}

func (f *fakeRPCTransport) SubscribeWithHandler(_ context.Context, _ string, _ uint, _ string, _ func(topic string, payload []byte, subcriber string) error) error {
	return fmt.Errorf("fakeRPCTransport: SubscribeWithHandler not implemented")
}

func (f *fakeRPCTransport) SubscribeWithTopic(_ context.Context, _ string, _ uint, _ string) (<-chan mqtt.Message, error) {
	return nil, fmt.Errorf("fakeRPCTransport: SubscribeWithTopic not implemented")
}

func (f *fakeRPCTransport) Publish(_ context.Context, _ string, _ []byte, _ byte, _ bool, _ string) error {
	return fmt.Errorf("fakeRPCTransport: Publish not implemented")
}

func (f *fakeRPCTransport) Publisher(_ context.Context, _ string, _ uint, _ byte, _ bool, _ string) (chan<- []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publisherErr != nil {
		return nil, f.publisherErr
	}
	return f.reqCh, nil
}

// newTestClient builds a *client wired to a fresh fakeRPCTransport.
func newTestClient(t *testing.T, id string, timeout time.Duration) (*client, *fakeRPCTransport) {
	t.Helper()
	fake := newFakeRPCTransport(id)
	c, err := NewClientE(context.Background(), logr.Discard(), fake, timeout)
	if err != nil {
		t.Fatalf("NewClientE: %v", err)
	}
	hc, ok := c.(*client)
	if !ok {
		t.Fatalf("NewClientE returned %T, want *client", c)
	}
	return hc, fake
}

// mustDeviceSummaryResponse builds a well-formed response carrying a single
// DeviceSummary result, addressed to req's Dialog.Id/Src.
func mustDeviceSummaryResponse(t *testing.T, req request, deviceId string) []byte {
	t.Helper()
	result := []DeviceSummary{{DeviceIdentifier: DeviceIdentifier{Id_: deviceId}, Name_: deviceId}}
	var resultAny any = result
	resp := response{
		Dialog: Dialog{Id: req.Id, Src: InstanceName, Dst: req.Src},
		Result: &resultAny,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return b
}

func mustUnmarshalRequest(t *testing.T, raw []byte) request {
	t.Helper()
	var r request
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return r
}

// --- Bug 1: request/response correlation under concurrency ---

// TestCallE_ConcurrentCalls_CorrelateResponsesByDialogID drives two
// concurrent CallE calls and deliberately feeds their responses back in
// reversed order. Before the fix, CallE read "whatever arrives next" off a
// shared channel, so the reordering would cross-wire the two callers. With
// per-call routing by Dialog.Id, each caller must get its own response
// regardless of feed order.
func TestCallE_ConcurrentCalls_CorrelateResponsesByDialogID(t *testing.T) {
	hc, fake := newTestClient(t, "client-under-test", 5*time.Second)
	ctx := context.Background()

	type callResult struct {
		out any
		err error
	}
	resA := make(chan callResult, 1)
	resB := make(chan callResult, 1)

	go func() {
		out, err := hc.CallE(ctx, DeviceLookup, "device-a")
		resA <- callResult{out, err}
	}()
	go func() {
		out, err := hc.CallE(ctx, DeviceLookup, "device-b")
		resB <- callResult{out, err}
	}()

	// Deterministically observe both outgoing requests: a blocking channel
	// receive, not a poll loop or a sleep.
	req1 := mustUnmarshalRequest(t, <-fake.reqCh)
	req2 := mustUnmarshalRequest(t, <-fake.reqCh)

	param1, _ := req1.Params.(string)
	param2, _ := req2.Params.(string)

	resp1 := mustDeviceSummaryResponse(t, req1, param1)
	resp2 := mustDeviceSummaryResponse(t, req2, param2)

	// Feed the response for the request we read SECOND before the one we
	// read FIRST: a deliberate reordering relative to publish order.
	fake.respCh <- resp2
	fake.respCh <- resp1

	gotA := <-resA
	gotB := <-resB

	checkResult := func(t *testing.T, label string, r callResult, want string) {
		t.Helper()
		if r.err != nil {
			t.Fatalf("%s: CallE error: %v", label, r.err)
		}
		ds, ok := r.out.(*[]DeviceSummary)
		if !ok || ds == nil || len(*ds) != 1 {
			t.Fatalf("%s: got %#v, want *[]DeviceSummary with 1 element", label, r.out)
		}
		if (*ds)[0].Id_ != want {
			t.Errorf("%s: got device id %q, want %q (response cross-wired to the wrong caller)", label, (*ds)[0].Id_, want)
		}
	}
	checkResult(t, "call A (device-a)", gotA, "device-a")
	checkResult(t, "call B (device-b)", gotB, "device-b")
}

// --- Bug 2: start() failure must not deadlock CallE ---

// TestCallE_SubscribeFailure_ReturnsErrorPromptly verifies that a Subscribe
// failure in start() is propagated as an error from CallE instead of
// leaving hc.to nil and blocking "hc.to <- reqStr" forever.
func TestCallE_SubscribeFailure_ReturnsErrorPromptly(t *testing.T) {
	hc, fake := newTestClient(t, "client-b", 5*time.Second)
	fake.subscribeErr = errors.New("boom: broker unreachable")

	done := make(chan error, 1)
	go func() {
		_, err := hc.CallE(context.Background(), DeviceLookup, "device-a")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from CallE when Subscribe fails, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CallE did not return within 2s: start() error not propagated, nil/blocked-channel deadlock")
	}
}

// TestCallE_ContextCancelDuringSend_ReturnsPromptly verifies the publish
// side is also select-guarded against ctx: if hc.to is never drained (e.g.
// broker unreachable after a successful start), a canceled context must
// unblock CallE instead of leaving it stuck on "hc.to <- reqStr" forever.
func TestCallE_ContextCancelDuringSend_ReturnsPromptly(t *testing.T) {
	hc, fake := newTestClient(t, "client-d", 5*time.Second)
	// Unbuffered and never drained by the test: any unguarded send blocks.
	fake.reqCh = make(chan []byte)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := hc.CallE(ctx, DeviceLookup, "device-a")
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CallE did not return after context cancellation: send on hc.to is not ctx-guarded")
	}
}

// --- Bug 3: methods must call the receiver, not the TheClient global ---

// TestLookupDevices_UsesReceiverNotGlobalTheClient poisons the package-level
// TheClient singleton and drives LookupDevices on an independent *client
// instance. If LookupDevices (or the CallE it invokes) referenced TheClient
// instead of hc, this would nil-panic; recover() turns that into a test
// failure with a clear message instead of crashing the process.
func TestLookupDevices_UsesReceiverNotGlobalTheClient(t *testing.T) {
	prevClient := TheClient
	TheClient = nil
	t.Cleanup(func() { TheClient = prevClient })

	hc, fake := newTestClient(t, "client-c", 5*time.Second)
	ctx := context.Background()

	type lookupOutcome struct {
		count int
		id    string
		err   error
	}
	done := make(chan lookupOutcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- lookupOutcome{err: fmt.Errorf("panic (likely nil TheClient dereference): %v", r)}
			}
		}()
		out, err := hc.LookupDevices(ctx, "some-device")
		if err != nil {
			done <- lookupOutcome{err: err}
			return
		}
		id := ""
		if len(*out) == 1 {
			id = (*out)[0].Id()
		}
		done <- lookupOutcome{count: len(*out), id: id}
	}()

	req := mustUnmarshalRequest(t, <-fake.reqCh)
	fake.respCh <- mustDeviceSummaryResponse(t, req, "dev-x")

	got := <-done
	if got.err != nil {
		t.Fatalf("LookupDevices: %v", got.err)
	}
	if got.count != 1 || got.id != "dev-x" {
		t.Errorf("got count=%d id=%q, want count=1 id=%q", got.count, got.id, "dev-x")
	}
}
