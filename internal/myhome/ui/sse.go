package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

type sseBroadcasterKey struct{}

// NewContextWithSSE adds an SSE broadcaster to the context
func NewContextWithSSE(ctx context.Context, broadcaster *SSEBroadcaster) context.Context {
	return context.WithValue(ctx, sseBroadcasterKey{}, broadcaster)
}

// SSEFromContext retrieves the SSE broadcaster from the context
func SSEFromContext(ctx context.Context) *SSEBroadcaster {
	if broadcaster, ok := ctx.Value(sseBroadcasterKey{}).(*SSEBroadcaster); ok {
		return broadcaster
	}
	return nil
}

// sseSlowClientMaxSkips is the number of consecutive full-channel skips before
// a slow client is forcibly disconnected.
const sseSlowClientMaxSkips = 10

// ssePingInterval is how often each SSE connection receives a "ping" event.
// It is the daemon liveness signal for the browser: while the daemon is up the
// stream never stays silent for longer than this. A variable (not a constant)
// only so tests can shorten it.
var ssePingInterval = 15 * time.Second

// sseRetryMillis is the EventSource reconnect delay advertised to browsers.
const sseRetryMillis = 3000

// ssePingData is the payload of the "ping" SSE event. started and version let
// a client tell a daemon restart (different started) or upgrade (different
// version) apart from a plain network blip.
type ssePingData struct {
	TS      int64  `json:"ts"`
	Started int64  `json:"started"`
	Version string `json:"version"`
}

// sseClient tracks per-client state inside the broadcaster.
type sseClient struct {
	ch               chan string
	consecutiveSkips int
}

// SSEBroadcaster manages Server-Sent Events clients and broadcasts updates
type SSEBroadcaster struct {
	clients map[chan string]*sseClient
	mu      sync.Mutex
	log     logr.Logger
	nameFor func(string) string // optional: resolves device id → friendly name
	started time.Time           // daemon start, reported in ping events
	version string              // daemon version, reported in ping events
	now     func() time.Time    // clock, injectable for tests
}

// NewSSEBroadcaster creates a new SSE broadcaster
func NewSSEBroadcaster(log logr.Logger) *SSEBroadcaster {
	return &SSEBroadcaster{
		clients: make(map[chan string]*sseClient),
		log:     log.WithName("SSEBroadcaster"),
		started: time.Now(),
		now:     time.Now,
	}
}

// SetBuildInfo sets the daemon start time and version reported in "ping"
// events. Without it, started is the broadcaster's creation time and version
// is empty.
func (b *SSEBroadcaster) SetBuildInfo(started time.Time, version string) {
	b.mu.Lock()
	b.started = started
	b.version = version
	b.mu.Unlock()
}

// pingFrame renders one "ping" SSE frame.
func (b *SSEBroadcaster) pingFrame() string {
	b.mu.Lock()
	d := ssePingData{TS: b.now().Unix(), Started: b.started.Unix(), Version: b.version}
	b.mu.Unlock()
	jsonData, _ := json.Marshal(d) // fixed struct of ints and a string: cannot fail
	return fmt.Sprintf("event: ping\ndata: %s\n\n", jsonData)
}

// SetDeviceNameResolver registers a function that maps a device id to a
// friendly name. When set, BroadcastEvent includes a device_name field in the
// SSE payload so live rows match the server-rendered table.
func (b *SSEBroadcaster) SetDeviceNameResolver(fn func(string) string) {
	b.mu.Lock()
	b.nameFor = fn
	b.mu.Unlock()
}

// Subscribe adds a new SSE client and returns a channel for receiving events
func (b *SSEBroadcaster) Subscribe() chan string {
	ch := make(chan string, 10) // Buffer to prevent blocking
	b.mu.Lock()
	b.clients[ch] = &sseClient{ch: ch}
	clientCount := len(b.clients)
	b.mu.Unlock()
	b.log.Info("SSE client subscribed", "total_clients", clientCount)
	return ch
}

// Unsubscribe removes an SSE client. It is safe to call even if the client was
// already evicted by broadcast() due to being too slow.
func (b *SSEBroadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	beforeCount := len(b.clients)
	if _, exists := b.clients[ch]; exists {
		delete(b.clients, ch)
		close(ch)
	}
	afterCount := len(b.clients)
	b.mu.Unlock()
	b.log.Info("SSE client unsubscribed", "before", beforeCount, "after", afterCount)
}

// broadcast sends a message to all connected clients, evicting any client whose
// channel has been full for sseSlowClientMaxSkips consecutive broadcasts.
func (b *SSEBroadcaster) broadcast(event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		b.log.Error(err, "Failed to marshal SSE data")
		return
	}

	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(jsonData))

	b.mu.Lock()
	defer b.mu.Unlock()

	clientCount := len(b.clients)
	b.log.V(1).Info("Broadcasting to clients", "event", event, "data", data, "client_count", clientCount, "message", msg)

	sentCount := 0
	skippedCount := 0
	var toEvict []chan string
	for ch, client := range b.clients {
		select {
		case ch <- msg:
			client.consecutiveSkips = 0
			sentCount++
		default:
			client.consecutiveSkips++
			skippedCount++
			if client.consecutiveSkips >= sseSlowClientMaxSkips {
				b.log.Info("Evicting slow SSE client", "consecutive_skips", client.consecutiveSkips)
				toEvict = append(toEvict, ch)
			} else {
				b.log.Info("SSE client channel full, skipping message", "consecutive_skips", client.consecutiveSkips)
			}
		}
	}
	for _, ch := range toEvict {
		delete(b.clients, ch)
		close(ch)
	}
	if len(toEvict) > 0 {
		b.log.Info("Evicted slow SSE clients", "event", event, "sent", sentCount, "skipped", skippedCount, "evicted", len(toEvict))
	} else {
		b.log.V(1).Info("Broadcast complete", "event", event, "sent", sentCount, "skipped", skippedCount)
	}
}

// SensorUpdateData represents a sensor value update
type SensorUpdateData struct {
	DeviceID string `json:"device_id"`
	Sensor   string `json:"sensor"`
	Value    string `json:"value"`
}

// BroadcastSensorUpdate broadcasts a sensor value update to all SSE clients
func (b *SSEBroadcaster) BroadcastSensorUpdate(deviceID string, sensor string, value string) {
	b.log.V(1).Info("Broadcasting sensor update", "device_id", deviceID, "sensor", sensor, "value", value)
	b.broadcast("sensor-update", SensorUpdateData{
		DeviceID: deviceID,
		Sensor:   sensor,
		Value:    value,
	})
}

// BroadcastDeviceUpdate broadcasts a device update to all SSE clients
func (b *SSEBroadcaster) BroadcastDeviceUpdate(dv DeviceView) {
	b.log.V(1).Info("Broadcasting device update", "device_id", dv.Id, "name", dv.Name)
	b.broadcast("device-update", dv)
}

// eventLogPayload is the JSON structure sent over SSE for new event log entries.
// It extends events.Event with an optional device_name field so live rows can
// display a friendly name without a round-trip. Existing fields are kept as-is
// so CLI consumers (event follow) continue to work without changes.
type eventLogPayload struct {
	events.Event
	DeviceName string `json:"device_name,omitempty"`
}

// BroadcastEvent broadcasts a new event log entry as JSON to all SSE clients.
// The payload is sent as SSE event "eventlog". The web UI renders the row from JSON;
// the CLI follow command also parses JSON from this event type.
func (b *SSEBroadcaster) BroadcastEvent(e events.Event) {
	b.mu.Lock()
	nameFor := b.nameFor
	b.mu.Unlock()
	payload := eventLogPayload{Event: e}
	if nameFor != nil {
		payload.DeviceName = nameFor(e.DeviceID)
	}
	b.broadcast("eventlog", payload)
}

// ServeHTTP handles SSE client connections using the broadcaster's client list
func (b *SSEBroadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	// Reconnect delay for EventSource, then the initial event to confirm the
	// connection (byte-identical to what it always was), then a first ping.
	_, _ = fmt.Fprintf(w, "retry: %d\n\n", sseRetryMillis)
	_, _ = w.Write([]byte("event: connected\ndata: ok\n\n"))
	_, _ = w.Write([]byte(b.pingFrame()))
	flusher.Flush()
	b.log.Info("SSE connection established, entering message loop")

	// Pings are written by this goroutine, not through broadcast(), so they
	// never count toward slow-client eviction. A failed write to a dead peer
	// surfaces here instead of lingering until the next real event.
	ticker := time.NewTicker(ssePingInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ticker.C:
			_, _ = w.Write([]byte(b.pingFrame()))
			flusher.Flush()
		case <-ctx.Done():
			b.log.Info("SSE client context done, disconnecting")
			return
		case msg, ok := <-ch:
			if !ok {
				b.log.Info("SSE client channel closed")
				return
			}
			b.log.V(1).Info("SSE sending message to client", "message", msg)
			_, _ = w.Write([]byte(msg))
			flusher.Flush()
		}
	}
}
