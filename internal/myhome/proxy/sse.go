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
		log:     log.WithName("SSEBroadcaster"),
	}
}

// Subscribe adds a new SSE client and returns a channel for receiving events
func (b *SSEBroadcaster) Subscribe() chan string {
	ch := make(chan string, 10) // Buffer to prevent blocking
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	b.log.V(1).Info("SSE client subscribed", "total_clients", len(b.clients))
	return ch
}

// Unsubscribe removes an SSE client
func (b *SSEBroadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
	b.log.V(1).Info("SSE client unsubscribed", "total_clients", len(b.clients))
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
			b.log.V(1).Info("SSE client channel full, skipping message")
		}
	}
}

// SensorUpdateData represents a sensor value update
type SensorUpdateData struct {
	DeviceID string  `json:"device_id"`
	Sensor   string  `json:"sensor"`
	Value    float64 `json:"value"`
}

// DoorUpdateData represents a door/window status update
type DoorUpdateData struct {
	DeviceID string `json:"device_id"`
	Opened   bool   `json:"opened"` // true if open, false if closed
}

// BroadcastSensorUpdate broadcasts a sensor value update to all SSE clients
func (b *SSEBroadcaster) BroadcastSensorUpdate(deviceID string, sensor string, value float64) {
	b.log.V(1).Info("Broadcasting sensor update", "device_id", deviceID, "sensor", sensor, "value", value)
	b.broadcast("sensor-update", SensorUpdateData{
		DeviceID: deviceID,
		Sensor:   sensor,
		Value:    value,
	})
}

// BroadcastDoorStatus broadcasts a door/window status update to all SSE clients
func (b *SSEBroadcaster) BroadcastDoorStatus(deviceID string, opened bool) {
	b.log.V(1).Info("Broadcasting door status", "device_id", deviceID, "opened", opened)
	b.broadcast("door-update", DoorUpdateData{
		DeviceID: deviceID,
		Opened:   opened,
	})
}
