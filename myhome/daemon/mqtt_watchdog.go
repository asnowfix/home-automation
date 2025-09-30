package daemon

import (
	"context"
	"myhome/mqtt"
	"time"

	"github.com/go-logr/logr"
)

// MqttWatchdog monitors MQTT connection health and triggers reconnection if needed
type MqttWatchdog struct {
	client        *mqtt.Client
	log           logr.Logger
	checkInterval time.Duration
	maxFailures   int
}

func NewMqttWatchdog(client *mqtt.Client, log logr.Logger, checkInterval time.Duration, maxFailures int) *MqttWatchdog {
	return &MqttWatchdog{
		client:        client,
		log:           log.WithName("MqttWatchdog"),
		checkInterval: checkInterval,
		maxFailures:   maxFailures,
	}
}

func (w *MqttWatchdog) Start(ctx context.Context) {
	ticker := time.NewTicker(w.checkInterval)
	defer ticker.Stop()

	consecutiveFailures := 0

	w.log.Info("Starting MQTT watchdog", "check_interval", w.checkInterval, "max_failures", w.maxFailures)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("MQTT watchdog stopped")
			return

		case <-ticker.C:
			if w.client.IsConnected() {
				if consecutiveFailures > 0 {
					w.log.Info("MQTT connection recovered", "previous_failures", consecutiveFailures)
					consecutiveFailures = 0
				}
			} else {
				consecutiveFailures++
				w.log.Error(nil, "MQTT connection lost", "consecutive_failures", consecutiveFailures, "max_failures", w.maxFailures)

				if consecutiveFailures >= w.maxFailures {
					w.log.Error(nil, "MQTT connection failed too many times, daemon needs restart", 
						"consecutive_failures", consecutiveFailures)
					// In a production environment, you might want to:
					// 1. Send an alert
					// 2. Trigger a graceful restart
					// 3. Exit with a specific error code for systemd to restart
					panic("MQTT connection permanently lost")
				}
			}
		}
	}
}
