package occupancy

import (
	"context"
	"encoding/json"
	"fmt"
	mqttclient "myhome/mqtt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
)

// Service implements a simple HTTP server that reports home occupancy based on
// recent motion/input events observed on MQTT.
//
// Heuristic:
// - Subscribe to Shelly Gen2 events: "+/events/rpc" for NotifyStatus messages
//   containing input state changes (e.g., button presses, motion sensors)
// - Any NotifyStatus event with "input:N" updates the lastEvent timestamp
// - Occupied if now - lastEvent <= window (default 12 hours).
//
// HTTP:
// - GET /status -> {"occupied":true|false}
// - Optional query: window=<seconds> to override window for this check only.

type Service struct {
	ctx       context.Context
	log       logr.Logger
	mc        *mqttclient.Client
	httpSrv   *http.Server
	lastEvent atomic.Int64 // unix nano of last relevant input event
	window    time.Duration
}

func NewService(ctx context.Context, log logr.Logger, mc *mqttclient.Client, window time.Duration) *Service {
	if window <= 0 {
		window = 12 * time.Hour // Default: 12 hours
	}
	s := &Service{
		ctx:    ctx,
		log:    log.WithName("occupancy.Service"),
		mc:     mc,
		window: window,
	}
	return s
}

// Start runs the MQTT subscriptions and the HTTP server on the given port.
func (s *Service) Start(port int) error {
	// Subscribe to Shelly Gen2 input status topics
	go s.subscribeInputs()

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		<-s.ctx.Done()
		_ = s.httpSrv.Close()
	}()

	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error(err, "occupancy HTTP server crashed")
		}
	}()

	s.log.Info("Occupancy HTTP service started", "port", port)
	return nil
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Optional per-request window override
	win := s.window
	if v := r.URL.Query().Get("window"); v != "" {
		if secs, err := time.ParseDuration(v + "s"); err == nil {
			win = secs
		}
	}
	occupied := s.isOccupied(win)
	resp := map[string]any{"occupied": occupied}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Service) isOccupied(window time.Duration) bool {
	last := time.Unix(0, s.lastEvent.Load())
	if last.IsZero() {
		return false
	}
	return time.Since(last) <= window
}

func (s *Service) subscribeInputs() {
	// Gen2 events/rpc for NotifyStatus with input activity
	topic := "+/events/rpc"
	ch, err := s.mc.Subscriber(s.ctx, topic, 16)
	if err != nil {
		s.log.Error(err, "Failed to subscribe to events", "topic", topic)
		return
	}
	s.log.Info("Subscribed to events", "topic", topic)

	// Monitor for NotifyStatus events containing input state changes
	// Example: {"method":"NotifyStatus","params":{"input:0":{"id":0,"state":true}}}
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Filter for NotifyStatus events with input state changes
			payload := string(msg)
			// s.log.Info("Received event", "payload", payload)
			if strings.Contains(payload, "\"NotifyStatus\"") && strings.Contains(payload, "\"input:") {
				s.log.Info("Received event with input state change", "payload", payload)
				s.lastEvent.Store(time.Now().UnixNano())
			}
		}
	}
}

// Start launches the occupancy HTTP service listening on port with the given MQTT client.
// Pass window=0 to use the default window (12 hours).
func Start(ctx context.Context, port int, mc *mqttclient.Client) error {
	return StartWithWindow(ctx, port, mc, 0)
}

// StartWithWindow launches the occupancy HTTP service with a custom occupancy window.
// Pass window=0 to use the default window (12 hours).
func StartWithWindow(ctx context.Context, port int, mc *mqttclient.Client, window time.Duration) error {
	log := logr.FromContextOrDiscard(ctx)
	svc := NewService(ctx, log, mc, window)
	return svc.Start(port)
}
