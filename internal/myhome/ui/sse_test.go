package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

func newTestBroadcaster(t *testing.T) *SSEBroadcaster {
	t.Helper()
	return NewSSEBroadcaster(logr.Discard())
}

// TestSSE_FastClientReceivesMessages verifies normal clients receive every broadcast.
func TestSSE_FastClientReceivesMessages(t *testing.T) {
	b := newTestBroadcaster(t)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.BroadcastSensorUpdate("dev1", "temperature", "22.5")

	select {
	case msg := <-ch:
		if msg == "" {
			t.Error("received empty message")
		}
	default:
		t.Error("expected message in channel, got none")
	}
}

// TestSSE_SlowClientEvictedAfterThreshold verifies that a client with a full
// channel is evicted after sseSlowClientMaxSkips consecutive skips.
func TestSSE_SlowClientEvictedAfterThreshold(t *testing.T) {
	b := newTestBroadcaster(t)

	// slow client: buffer=0 so every broadcast fills immediately
	slowCh := make(chan string, 0)
	b.mu.Lock()
	b.clients[slowCh] = &sseClient{ch: slowCh}
	b.mu.Unlock()

	// fast client that drains the channel
	fastCh := b.Subscribe()
	defer b.Unsubscribe(fastCh)

	// Broadcast sseSlowClientMaxSkips times — slow channel full every time
	for i := 0; i < sseSlowClientMaxSkips; i++ {
		b.BroadcastSensorUpdate("dev1", "temperature", "22.5")
		// drain fast client so it stays healthy
		<-fastCh
	}

	// After exactly sseSlowClientMaxSkips skips the slow client should be evicted
	b.mu.Lock()
	_, slowStillPresent := b.clients[slowCh]
	b.mu.Unlock()

	if slowStillPresent {
		t.Error("slow client should have been evicted but is still in the map")
	}

	// The closed channel should now return the zero value immediately
	select {
	case _, open := <-slowCh:
		if open {
			t.Error("evicted channel should be closed")
		}
	default:
		t.Error("evicted channel should be closed (readable), not blocking")
	}
}

// TestSSE_SlowClientDoesNotAffectFastClients verifies that evicting a slow client
// does not interrupt delivery to fast clients.
func TestSSE_SlowClientDoesNotAffectFastClients(t *testing.T) {
	b := newTestBroadcaster(t)

	fastCh := b.Subscribe()
	defer b.Unsubscribe(fastCh)

	// slow client
	slowCh := make(chan string, 0)
	b.mu.Lock()
	b.clients[slowCh] = &sseClient{ch: slowCh}
	b.mu.Unlock()

	const broadcasts = sseSlowClientMaxSkips + 5
	for i := 0; i < broadcasts; i++ {
		b.BroadcastSensorUpdate("dev1", "temperature", "22.5")
		// fast client drains promptly
		select {
		case msg := <-fastCh:
			if msg == "" {
				t.Errorf("broadcast %d: empty message", i)
			}
		default:
			t.Errorf("broadcast %d: fast client did not receive message", i)
		}
	}
}

// TestSSE_UnsubscribeAfterEvictionIsSafe verifies that calling Unsubscribe on a
// channel that was already closed by the broadcaster does not panic.
func TestSSE_UnsubscribeAfterEvictionIsSafe(t *testing.T) {
	b := newTestBroadcaster(t)

	slowCh := make(chan string, 0)
	b.mu.Lock()
	b.clients[slowCh] = &sseClient{ch: slowCh}
	b.mu.Unlock()

	for i := 0; i < sseSlowClientMaxSkips; i++ {
		b.BroadcastSensorUpdate("dev1", "temperature", "22.5")
	}

	// Must not panic
	b.Unsubscribe(slowCh)
}

// readFrames reads n blank-line-terminated SSE frames from the stream.
func readFrames(t *testing.T, sc *bufio.Scanner, n int) []string {
	t.Helper()
	var frames []string
	var cur []string
	for len(frames) < n {
		if !sc.Scan() {
			t.Fatalf("stream ended after %d frames: %v", len(frames), sc.Err())
		}
		if line := sc.Text(); line == "" {
			frames = append(frames, strings.Join(cur, "\n"))
			cur = nil
		} else {
			cur = append(cur, line)
		}
	}
	return frames
}

// openStream connects to the broadcaster; the request carries a deadline so a
// regression (e.g. pings stop) fails in seconds instead of hanging until the
// package timeout.
func openStream(t *testing.T, b *SSEBroadcaster) *bufio.Scanner {
	t.Helper()
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return bufio.NewScanner(resp.Body)
}

// TestSSE_PingStream verifies the stream prologue (retry, connected, first
// ping) and that further pings follow on the ticker, with started/version/ts.
func TestSSE_PingStream(t *testing.T) {
	old := ssePingInterval
	ssePingInterval = 10 * time.Millisecond
	t.Cleanup(func() { ssePingInterval = old })

	b := newTestBroadcaster(t)
	started := time.Unix(1700000000, 0)
	b.SetDaemonInfo(started, "v1.2.3")
	b.now = func() time.Time { return time.Unix(1700000100, 0) }

	frames := readFrames(t, openStream(t, b), 5)
	if frames[0] != "retry: 3000" {
		t.Errorf("frame 0 = %q, want retry line", frames[0])
	}
	if frames[1] != "event: connected\ndata: ok" {
		t.Errorf("connected frame changed: %q", frames[1])
	}
	for _, f := range frames[2:] {
		name, data, ok := strings.Cut(f, "\n")
		if !ok || name != "event: ping" || !strings.HasPrefix(data, "data: ") {
			t.Fatalf("expected ping frame, got %q", f)
		}
		var p ssePingData
		if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &p); err != nil {
			t.Fatal(err)
		}
		if p.TS != 1700000100 || p.Started != 1700000000 || p.Version != "v1.2.3" {
			t.Errorf("ping payload = %+v", p)
		}
	}
	if got := b.pingFrame(); got != "event: ping\ndata: {\"ts\":1700000100,\"started\":1700000000,\"version\":\"v1.2.3\"}\n\n" {
		t.Errorf("ping bytes = %q", got)
	}
}

// TestSSE_PingsInterleaveWithBroadcasts verifies eventlog frames arrive with
// their exact payload while pings tick (10 ms), and that a disconnect
// unsubscribes the client.
func TestSSE_PingsInterleaveWithBroadcasts(t *testing.T) {
	old := ssePingInterval
	ssePingInterval = 10 * time.Millisecond
	t.Cleanup(func() { ssePingInterval = old })

	b := newTestBroadcaster(t)
	sc := openStream(t, b)
	// The handler subscribes before writing the prologue, so once retry,
	// connected and the first ping are read, the broadcast reaches this client.
	readFrames(t, sc, 3)

	b.BroadcastEvent(events.Event{Ts: 42, DeviceID: "d1", Component: "c", Event: "e", Severity: "info"})
	ev := events.Event{Ts: 42, DeviceID: "d1", Component: "c", Event: "e", Severity: "info"}
	b.BroadcastEvent(ev)
	var got string
	for i := 0; i < 50; i++ {
		f := readFrames(t, sc, 1)[0]
		if strings.HasPrefix(f, "event: ping") {
			continue
		}
		got = f
		break
	}
	// Frozen wire format: the events.Event fields in this exact order, then "html"
	// (the server-rendered table row, see renderEventRow).
	prefix := `event: eventlog` + "\n" + `data: {"id":0,"ts":42,"received_at":0,"device_id":"d1","component":"c","event":"e","severity":"info","html":"`
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("eventlog frame = %q, want prefix %q", got, prefix)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got, "event: eventlog\ndata: ")), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if len(payload) != 8 {
		t.Errorf("payload keys = %v, want exactly id ts received_at device_id component event severity html", payload)
	}
	if html := payload["html"]; html != renderEventRow(toEventRows([]events.Event{ev}, nil)[0]) || html == "" {
		t.Errorf("html = %q, want the rendered event row", html)
	}
}

// TestSSE_DisconnectUnsubscribes verifies the handler removes its client when
// the connection goes away.
func TestSSE_DisconnectUnsubscribes(t *testing.T) {
	b := newTestBroadcaster(t)
	srv := httptest.NewServer(b)
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readFrames(t, bufio.NewScanner(resp.Body), 1)
	cancel()
	resp.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		n := len(b.clients)
		b.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("client still subscribed after disconnect (%d)", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
