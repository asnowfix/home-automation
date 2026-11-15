package daemon

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/sdnotify"
	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/go-logr/logr"
)

// watchdogMaxFailures: consecutive failed beats before the heartbeat stops.
// The MQTT client drops and re-subscribes every MQTT_RECONNECT_INTERVAL (2 h),
// and during that window the probe cannot succeed.
const watchdogMaxFailures = 3

// healthChecks are the liveness checks gating the systemd watchdog heartbeat
// (see sdnotify.Heartbeat). The alert evaluator (#712) registers its own check
// through RegisterHealthCheck once it exists; nothing else should need to.
var (
	healthMu     sync.Mutex
	healthChecks []sdnotify.Check
)

// RegisterHealthCheck adds a check that must pass for the daemon to keep
// petting the systemd watchdog. Safe to call at any time.
func RegisterHealthCheck(c sdnotify.Check) {
	healthMu.Lock()
	defer healthMu.Unlock()
	healthChecks = append(healthChecks, c)
}

func registeredHealthChecks() []sdnotify.Check {
	healthMu.Lock()
	defer healthMu.Unlock()
	return append([]sdnotify.Check(nil), healthChecks...)
}

// mqttRoundTripCheck proves the broker and the MQTT client loop are alive: it
// publishes a nonce on a private topic and waits for it to come back through
// its own subscription. A bare timer would keep the heartbeat going on a
// daemon whose broker is wedged, which is the failure the watchdog exists for.
func mqttRoundTripCheck(ctx context.Context, mc mqttclient.Client) (sdnotify.Check, error) {
	topic := fmt.Sprintf("myhome/%s/watchdog", myhome.InstanceName)
	ch, err := mc.Subscribe(ctx, topic, 8, "watchdog")
	if err != nil {
		return nil, fmt.Errorf("watchdog subscribe %s: %w", topic, err)
	}
	var seq int64
	return func(ctx context.Context) error {
		seq++
		nonce := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatInt(seq, 10)
		if err := mc.Publish(ctx, topic, []byte(nonce), mqttclient.AtLeastOnce, false, "watchdog"); err != nil {
			return fmt.Errorf("publish: %w", err)
		}
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return fmt.Errorf("subscription closed")
				}
				if string(msg) == nonce {
					return nil
				}
				// A late echo of an earlier probe: keep waiting for this one.
			case <-ctx.Done():
				return fmt.Errorf("no echo of probe on %s: %w", topic, ctx.Err())
			}
		}
	}, nil
}

// startWatchdog tells systemd the daemon is ready and, when the unit has
// WatchdogSec set, starts the heartbeat. All of it is a no-op outside systemd.
// A failure to set up the probe is logged, never fatal (observability must not
// block the daemon): the heartbeat then runs without the MQTT check.
func startWatchdog(ctx context.Context, log logr.Logger, mc mqttclient.Client) {
	if check, err := mqttRoundTripCheck(ctx, mc); err != nil {
		log.Error(err, "MQTT liveness probe unavailable")
	} else {
		RegisterHealthCheck(check)
	}
	if err := sdnotify.Ready(); err != nil {
		log.Error(err, "sd_notify READY failed")
	}
	if interval := sdnotify.WatchdogInterval(); interval > 0 {
		log.Info("systemd watchdog enabled", "heartbeat_every", interval)
		go sdnotify.Heartbeat(ctx, log.WithName("watchdog"), interval, watchdogMaxFailures, registeredHealthChecks)
	}
}
