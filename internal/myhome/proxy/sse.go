package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

// SSEBroadcaster manages Server-Sent Events clients and broadcasts updates
type SSEBroadcaster struct {
	clients map[chan string]struct{}
	mu      sync.RWMutex
	log     logr.Logger
}

// NewSSEBroadcaster creates a new SSE broadcaster
func NewSSEBroadcaster(log logr.Logger) *SSEBroadcaster {
	return &SSEBroadcaster{
		clients: make(map[chan string]struct{}),
		log:     log.WithName("SSEBroadcaster").V(1),
	}
}

// Subscribe adds a new SSE client and returns a channel for receiving events
func (b *SSEBroadcaster) Subscribe() chan string {
	ch := make(chan string, 10) // Buffer to prevent blocking
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	b.log.Info("SSE client subscribed")
	return ch
}

// Unsubscribe removes an SSE client
func (b *SSEBroadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	clientCount := len(b.clients)
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
	b.log.Info("SSE client unsubscribed", "former clients", clientCount)
}

// broadcast sends a message to all connected clients
func (b *SSEBroadcaster) broadcast(event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		b.log.Error(err, "Failed to marshal SSE data")
		return
	}

	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(jsonData))

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		select {
		case ch <- msg:
			// Successfully sent
		default:
			// Channel full, skip this client to avoid blocking
			b.log.Info("SSE client channel full, skipping message")
		}
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
	b.log.Info("Broadcasting sensor update", "device_id", deviceID, "sensor", sensor, "value", value)
	b.broadcast("sensor-update", SensorUpdateData{
		DeviceID: deviceID,
		Sensor:   sensor,
		Value:    value,
	})
}
