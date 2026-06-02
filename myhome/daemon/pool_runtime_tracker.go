package daemon

import (
	"context"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
)

// PoolRuntimeTracker reports how long the pool pump has run today and how much
// runtime remains toward the daily filtration target.  It reads directly from
// the shared events database populated by the gen2 listener — no separate
// pool.db, no MQTT subscription.
type PoolRuntimeTracker struct {
	log       logr.Logger
	storage   *events.Storage
	deviceID  string
	component string
}

func NewPoolRuntimeTracker(log logr.Logger, storage *events.Storage, deviceID string) *PoolRuntimeTracker {
	return &PoolRuntimeTracker{
		log:       log.WithName("PoolRuntimeTracker"),
		storage:   storage,
		deviceID:  deviceID,
		component: "switch:0",
	}
}

// DailyRuntimeSec returns the total seconds the pump has run today,
// including any currently-running interval.
func (t *PoolRuntimeTracker) DailyRuntimeSec(ctx context.Context) (int64, error) {
	return t.storage.OnDurationSec(
		ctx,
		t.deviceID, t.component,
		"switch.on", "switch.off",
		time.Now().Format("2006-01-02"),
	)
}

// RemainingRuntimeSec returns max(0, targetSec - DailyRuntimeSec()).
func (t *PoolRuntimeTracker) RemainingRuntimeSec(ctx context.Context, targetSec int64) (int64, error) {
	daily, err := t.DailyRuntimeSec(ctx)
	if err != nil {
		return 0, err
	}
	if daily >= targetSec {
		return 0, nil
	}
	return targetSec - daily, nil
}
