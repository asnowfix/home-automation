package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"myhome/mqtt"

	"github.com/go-logr/logr"
)

// MetricsCache stores the latest metrics from each device
type MetricsCache struct {
	mu      sync.RWMutex
	metrics map[string]string // deviceID -> metrics payload
}

func NewMetricsCache() *MetricsCache {
	return &MetricsCache{
		metrics: make(map[string]string),
	}
}

func (mc *MetricsCache) Set(deviceID, metrics string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.metrics[deviceID] = metrics
}

func (mc *MetricsCache) GetAll() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := ""
	for _, metrics := range mc.metrics {
		result += metrics
	}
	return result
}

// Exporter handles MQTT subscription and HTTP serving for Prometheus metrics
type Exporter struct {
	mqttClient   *mqtt.Client
	cache        *MetricsCache
	httpServer   *http.Server
	mqttTopic    string
	httpAddr     string
	log          logr.Logger
	ctx          context.Context
	subscription <-chan mqtt.Message
}

// NewExporter creates a new metrics exporter
func NewExporter(ctx context.Context, log logr.Logger, mqttClient *mqtt.Client, mqttTopic, httpAddr string) *Exporter {
	return &Exporter{
		ctx:        ctx,
		mqttClient: mqttClient,
		cache:      NewMetricsCache(),
		mqttTopic:  mqttTopic,
		httpAddr:   httpAddr,
		log:        log,
	}
}

// Start begins the metrics exporter service
func (e *Exporter) Start() error {
	// Subscribe to metrics topic
	topic := e.mqttTopic + "/#"
	e.log.Info("Subscribing to MQTT topic", "topic", topic)

	sub, err := e.mqttClient.MultiSubscribe(e.ctx, topic, 8, "myhome/metrics")
	if err != nil {
		return fmt.Errorf("failed to subscribe to MQTT topic %s: %w", topic, err)
	}
	e.subscription = sub

	e.log.Info("Subscribed to MQTT topic", "topic", topic)

	// Start message processor
	go e.processMessages()

	// Start HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", e.handleMetrics)
	mux.HandleFunc("/health", e.handleHealth)

	e.httpServer = &http.Server{
		Addr:    e.httpAddr,
		Handler: mux,
	}

	go func() {
		e.log.Info("Starting HTTP server for Prometheus metrics", "addr", e.httpAddr)
		if err := e.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			e.log.Error(err, "HTTP server error")
		}
	}()

	return nil
}

// processMessages handles incoming MQTT messages
func (e *Exporter) processMessages() {
	for {
		select {
		case <-e.ctx.Done():
			e.log.Info("Stopping message processor")
			return
		case msg, ok := <-e.subscription:
			if !ok {
				e.log.Info("Subscription channel closed")
				return
			}
			e.handleMessage(msg)
		}
	}
}

// handleMessage processes a single MQTT message
func (e *Exporter) handleMessage(msg mqtt.Message) {
	// Extract device ID from topic: shelly/metrics/<device-id>
	topic := msg.Topic()
	prefix := e.mqttTopic + "/"
	if !strings.HasPrefix(topic, prefix) {
		e.log.V(1).Info("Ignoring message with unexpected topic", "topic", topic)
		return
	}

	deviceID := strings.TrimPrefix(topic, prefix)
	payload := string(msg.Payload())
	e.cache.Set(deviceID, payload)

	e.log.V(1).Info("Received metrics", "device_id", deviceID, "size_bytes", len(payload))
}

// Stop shuts down the metrics exporter
func (e *Exporter) Stop() error {
	e.log.Info("Shutting down metrics exporter")

	// Shutdown HTTP server
	if e.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := e.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown HTTP server: %w", err)
		}
	}

	return nil
}

func (e *Exporter) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := e.cache.GetAll()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(metrics))

	e.log.V(2).Info("Served metrics", "size_bytes", len(metrics))
}

func (e *Exporter) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := "ok"
	if e.mqttClient == nil || !e.mqttClient.IsConnected() {
		status = "mqtt_disconnected"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"%s"}`, status)
}
