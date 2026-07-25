package beem

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-logr/logr"
)

const (
	// MQTTTopic is the retained MQTT topic where Beem power samples are published.
	MQTTTopic = "myhome/energy/beem/power"
)

// Publisher is the minimal MQTT capability the Watcher needs: publishing a
// (possibly retained) payload to a topic. Declared locally, rather than
// importing the app's MQTT client type, so pkg/beem stays a standalone
// module with no dependency on the app module. Any richer MQTT client
// (e.g. myhome/mqtt.Client) satisfies this interface as-is.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool, publisherName string) error
}

// Watcher polls the Beem Energy REST API and publishes power samples to MQTT.
type Watcher struct {
	// PowerCh delivers every successfully fetched PowerSample to callers.
	PowerCh <-chan PowerSample

	// OnResult, if set, is called after every poll attempt with the outcome
	// (nil error on success). Lets the daemon track account connection status
	// without this package knowing anything about that concept.
	OnResult func(err error)

	client  *Client
	pub     Publisher
	powerCh chan PowerSample
	log     logr.Logger
}

// NewWatcher creates a Watcher but does not start polling yet.
// Call Start to begin the polling loop. log receives watcher activity; pass
// logr.Discard() for no logging.
func NewWatcher(ctx context.Context, cfg ClientConfig, pub Publisher, log logr.Logger) *Watcher {
	ch := make(chan PowerSample, 16)
	return &Watcher{
		PowerCh: ch,
		client:  NewClient(cfg, log),
		pub:     pub,
		powerCh: ch,
		log:     log,
	}
}

// Start launches the polling goroutine.  It returns immediately; the goroutine
// runs until ctx is cancelled.  If either Email or Password is empty, Start is
// a no-op and returns nil — the caller is responsible for checking credentials
// before calling, but a double guard here prevents accidental unauthenticated loops.
func (w *Watcher) Start(ctx context.Context) error {
	if w.client.cfg.Email == "" || w.client.cfg.Password == "" {
		w.log.Info("Beem Energy watcher: email or password not set, skipping start")
		return nil
	}
	if w.client.cfg.PollInterval <= 0 {
		w.client.cfg.PollInterval = 60 * time.Second
	}

	w.log.Info("Starting Beem Energy watcher",
		"poll_interval", w.client.cfg.PollInterval,
		"topic", MQTTTopic,
	)

	go w.run(ctx)
	return nil
}

func (w *Watcher) run(ctx context.Context) {
	ticker := time.NewTicker(w.client.cfg.PollInterval)
	defer ticker.Stop()

	// Poll once immediately so we don't have to wait for the first tick.
	w.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Beem Energy watcher stopping")
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Watcher) poll(ctx context.Context) {
	sample, err := w.client.PollSummary(ctx)
	if w.OnResult != nil {
		w.OnResult(err)
	}
	if err != nil {
		w.log.Error(err, "Beem Energy poll failed")
		return
	}

	w.log.V(1).Info("Beem Energy sample",
		"solar_w", sample.SolarW,
		"daily_wh", sample.DailyWh,
		"monthly_wh", sample.MonthlyWh,
		"source", sample.Source,
		"ts", sample.TS,
	)

	// Publish to MQTT as a retained message.
	payload, err := json.Marshal(sample)
	if err != nil {
		w.log.Error(err, "Beem Energy: failed to marshal power sample")
		return
	}

	if err := w.pub.Publish(ctx, MQTTTopic, payload, 0, true, "beem-watcher"); err != nil {
		w.log.Error(err, "Beem Energy: failed to publish to MQTT", "topic", MQTTTopic)
		return
	}

	// Forward to PowerCh (non-blocking; drop if consumer is slow).
	select {
	case w.powerCh <- sample:
	default:
		w.log.V(1).Info("Beem Energy: PowerCh full, dropping sample")
	}

	// Ensure payload string shows up in logs as a human-readable JSON string.
	w.log.Info("Beem Energy: published sample",
		"topic", MQTTTopic,
		"payload", string(payload),
	)
}
