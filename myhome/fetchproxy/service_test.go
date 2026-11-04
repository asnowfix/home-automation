package fetchproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
)

// countingDoer records how many times Do was called and always returns body
// with a 200 status, unless err is set.
type countingDoer struct {
	mu    sync.Mutex
	calls int
	body  []byte
	err   error
}

func (d *countingDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(d.body)),
	}, nil
}

func (d *countingDoer) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// blockingDoer is like countingDoer but the first call blocks until release
// is closed, so a test can guarantee two concurrent runOnce calls overlap
// inside the same singleflight.Group.Do window.
type blockingDoer struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	body    []byte
}

func newBlockingDoer(body []byte) *blockingDoer {
	return &blockingDoer{
		started: make(chan struct{}),
		release: make(chan struct{}),
		body:    body,
	}
}

func (d *blockingDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.calls++
	first := d.calls == 1
	d.mu.Unlock()
	if first {
		close(d.started)
	}
	<-d.release
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(d.body)),
	}, nil
}

func (d *blockingDoer) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func newTestServiceForDeps(t *testing.T, doer httpDoer) (*Service, *mqtt.RecordingMockClient) {
	t.Helper()
	mc := mqtt.NewRecordingMockClient()
	storage := newTestStorage(t)
	svc := &Service{
		ctx:     context.Background(),
		log:     logr.Discard(),
		mc:      mc,
		storage: storage,
		limits:  testLimits(),
		http:    doer,
		cancels: make(map[string]context.CancelFunc),
		states:  make(map[string]*fetchState),
	}
	return svc, mc
}

// TestRunOnceDedupsConcurrentFetchesByURLAndHeaders is the direct test of
// #465's dedup requirement: "Two devices subscribing to the same URL with
// different transforms cause one upstream fetch."
func TestRunOnceDedupsConcurrentFetchesByURLAndHeaders(t *testing.T) {
	doer := newBlockingDoer([]byte(`{"v":1}`))
	svc, mc := newTestServiceForDeps(t, doer)

	subA := Subscription{
		DeviceID: "pump-1", Name: "forecast",
		URL:       "https://api.open-meteo.com/v1/forecast?x=1",
		Transform: `function(body){ var d = JSON.parse(body); return {v: d.v}; }`,
		Topic:     "myhome/fetch/pump-1/forecast",
	}
	subB := Subscription{
		DeviceID: "heater-1", Name: "forecast-full",
		URL:       subA.URL, // same URL, same (absent) headers
		Transform: `function(body){ var d = JSON.parse(body); return {full: d.v}; }`,
		Topic:     "myhome/fetch/heater-1/forecast-full",
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		svc.runOnce(context.Background(), subA)
	}()
	go func() {
		defer wg.Done()
		<-doer.started // wait until the first fetch is in flight and blocked
		svc.runOnce(context.Background(), subB)
	}()

	// Give the second goroutine time to reach fetchGrp.Do and join the
	// in-flight call before we let the (indefinitely blocked) first call
	// return.
	time.Sleep(50 * time.Millisecond)
	close(doer.release)
	wg.Wait()

	if got := doer.Calls(); got != 1 {
		t.Fatalf("expected exactly 1 upstream fetch for two subscriptions sharing URL+headers, got %d", got)
	}

	if len(mc.Published(subA.Topic)) != 1 {
		t.Fatalf("expected subA's transform to be published once, got %d", len(mc.Published(subA.Topic)))
	}
	if len(mc.Published(subB.Topic)) != 1 {
		t.Fatalf("expected subB's transform to be published once, got %d", len(mc.Published(subB.Topic)))
	}

	var gotA map[string]any
	if err := json.Unmarshal(mc.Published(subA.Topic)[0], &gotA); err != nil {
		t.Fatalf("unmarshal subA publish: %v", err)
	}
	if gotA["v"] != float64(1) {
		t.Fatalf("subA published unexpected payload: %v", gotA)
	}
	if _, ok := gotA["ts"]; !ok {
		t.Fatalf("subA published payload missing ts field: %v", gotA)
	}
}

// TestRunOnceFetchFailureLeavesRetainedValueUntouched covers #465's failure
// semantics: "Never publish a bad payload... keep the last good value
// retained."
func TestRunOnceFetchFailureLeavesRetainedValueUntouched(t *testing.T) {
	doer := &countingDoer{body: []byte(`{"v":1}`)}
	svc, mc := newTestServiceForDeps(t, doer)

	sub := Subscription{
		DeviceID: "pump-1", Name: "forecast",
		URL:       "https://api.open-meteo.com/v1/forecast",
		Transform: `function(body){ var d = JSON.parse(body); return {v: d.v}; }`,
		Topic:     "myhome/fetch/pump-1/forecast",
	}

	svc.runOnce(context.Background(), sub)
	published := mc.Published(sub.Topic)
	if len(published) != 1 {
		t.Fatalf("expected one publish after a successful fetch, got %d", len(published))
	}
	firstGood := published[0]

	// Now the upstream starts failing.
	doer.mu.Lock()
	doer.err = context.DeadlineExceeded
	doer.mu.Unlock()

	svc.runOnce(context.Background(), sub)

	published = mc.Published(sub.Topic)
	if len(published) != 1 {
		t.Fatalf("expected no additional publish after a fetch failure, got %d total", len(published))
	}
	if !bytes.Equal(published[0], firstGood) {
		t.Fatalf("retained payload changed after a fetch failure: got %s, want %s", published[0], firstGood)
	}

	svc.mu.Lock()
	st := svc.states[sub.key()]
	svc.mu.Unlock()
	if st == nil || st.lastOK {
		t.Fatalf("expected in-memory state to record the failure as not-OK, got %+v", st)
	}
}

// TestRunOnceTransformFailureLeavesRetainedValueUntouched is the same
// guarantee, but the failure is in the transform rather than the fetch.
func TestRunOnceTransformFailureLeavesRetainedValueUntouched(t *testing.T) {
	doer := &countingDoer{body: []byte(`{"v":1}`)}
	svc, mc := newTestServiceForDeps(t, doer)

	sub := Subscription{
		DeviceID: "pump-1", Name: "forecast",
		URL:       "https://api.open-meteo.com/v1/forecast",
		Transform: `function(body){ var d = JSON.parse(body); return {v: d.v}; }`,
		Topic:     "myhome/fetch/pump-1/forecast",
	}
	svc.runOnce(context.Background(), sub)
	if len(mc.Published(sub.Topic)) != 1 {
		t.Fatalf("expected one publish after a successful fetch")
	}

	broken := sub
	broken.Transform = `function(body){ throw new Error("boom"); }`
	svc.runOnce(context.Background(), broken)

	if len(mc.Published(sub.Topic)) != 1 {
		t.Fatalf("expected no additional publish after a transform failure, got %d", len(mc.Published(sub.Topic)))
	}
}
