package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
	_ "modernc.org/sqlite"
)

// PoolRuntimeTracker tracks how many seconds the pool pump has run today.
// It subscribes to the pool Shelly's MQTT topic and persists every ON/OFF
// transition to a SQLite event log, surviving daemon restarts.
type PoolRuntimeTracker struct {
	log      logr.Logger
	db       *sql.DB
	mqtt     mqttclient.Client
	deviceID string

	mu          sync.Mutex
	accumulated int64     // seconds from completed ON→OFF pairs today
	lastOnTs    time.Time // set when pump is currently ON; zero if OFF
}

// NewPoolRuntimeTracker creates a PoolRuntimeTracker, opens (or creates) the
// SQLite database at dbPath, and recovers runtime accumulated so far today.
func NewPoolRuntimeTracker(log logr.Logger, dbPath string, mc mqttclient.Client, deviceID string) (*PoolRuntimeTracker, error) {
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS pump_events (
		id           INTEGER PRIMARY KEY,
		ts           DATETIME NOT NULL,
		type         TEXT NOT NULL,
		duration_sec INTEGER
	)`); err != nil {
		db.Close()
		return nil, err
	}

	t := &PoolRuntimeTracker{
		log:      log.WithName("PoolRuntimeTracker"),
		db:       db,
		mqtt:     mc,
		deviceID: deviceID,
	}

	if err := t.recoverState(); err != nil {
		db.Close()
		return nil, err
	}

	return t, nil
}

// recoverState reads today's completed events and checks if the pump is
// currently ON (i.e. the last ON event has no subsequent OFF event today).
func (t *PoolRuntimeTracker) recoverState() error {
	// Sum completed ON→OFF durations for today.
	var sum sql.NullInt64
	err := t.db.QueryRow(
		`SELECT SUM(duration_sec) FROM pump_events WHERE date(ts)=date('now') AND type='off'`,
	).Scan(&sum)
	if err != nil {
		return err
	}
	if sum.Valid {
		t.accumulated = sum.Int64
	}

	// Find the last ON event today.
	var lastOnStr string
	err = t.db.QueryRow(
		`SELECT ts FROM pump_events WHERE date(ts)=date('now') AND type='on' ORDER BY id DESC LIMIT 1`,
	).Scan(&lastOnStr)
	if err == sql.ErrNoRows {
		// No ON event today — pump was not running.
		return nil
	}
	if err != nil {
		return err
	}

	// Parse the stored timestamp.
	lastOn, err := parseTimestamp(lastOnStr)
	if err != nil {
		t.log.Error(err, "Failed to parse last ON timestamp, ignoring", "ts", lastOnStr)
		return nil
	}

	// Check if there is a subsequent OFF event after the last ON event.
	var offCount int
	err = t.db.QueryRow(
		`SELECT COUNT(*) FROM pump_events WHERE date(ts)=date('now') AND type='off' AND ts > ?`,
		lastOnStr,
	).Scan(&offCount)
	if err != nil {
		return err
	}

	if offCount == 0 {
		// Pump was ON when the daemon last stopped; treat it as still running.
		t.lastOnTs = lastOn
		t.log.Info("Recovery: pump was ON at last shutdown, continuing timer", "since", lastOn)
	}

	return nil
}

// parseTimestamp tries several SQLite datetime formats.
func parseTimestamp(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if ts, err := time.Parse(f, s); err == nil {
			return ts, nil
		}
	}
	// fallback: try time.Parse with the value as-is
	return time.Parse("2006-01-02 15:04:05", s)
}

// Start subscribes to the Shelly device's MQTT events topic and begins
// processing ON/OFF state changes. It blocks until ctx is cancelled.
func (t *PoolRuntimeTracker) Start(ctx context.Context) error {
	// Shelly Gen2 devices publish NotifyStatus on <device-id>/events/rpc.
	// We subscribe to the wildcard +/events/rpc and filter by device ID.
	// If a specific deviceID is configured we match only that device;
	// otherwise we fall back to any device (useful for testing with ":memory:").
	topic := "+/events/rpc"

	t.log.Info("Subscribing to pool pump events", "topic", topic, "device_id", t.deviceID)

	return t.mqtt.SubscribeWithHandler(ctx, topic, 16, "pool-runtime-tracker",
		func(mqttTopic string, payload []byte, _ string) error {
			return t.handleMessage(ctx, mqttTopic, payload)
		})
}

// handleMessage processes a single MQTT message from the events/rpc topic.
func (t *PoolRuntimeTracker) handleMessage(_ context.Context, _ string, payload []byte) error {
	var msg struct {
		Method string                     `json:"method"`
		Src    string                     `json:"src"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.log.V(1).Info("Failed to parse MQTT message", "error", err)
		return nil
	}

	// Filter by device ID if configured; use the payload's "src" field.
	if t.deviceID != "" && msg.Src != t.deviceID {
		return nil
	}

	if msg.Method != "NotifyStatus" {
		return nil
	}

	for key, raw := range msg.Params {
		if key == "ts" || !strings.HasPrefix(key, "switch:") {
			continue
		}
		var sw struct {
			Output *bool `json:"output"`
		}
		if err := json.Unmarshal(raw, &sw); err != nil || sw.Output == nil {
			continue
		}
		if *sw.Output {
			t.recordOn()
		} else {
			t.recordOff()
		}
	}
	return nil
}

// recordOn records a pump-on event in the DB and starts the in-memory timer.
func (t *PoolRuntimeTracker) recordOn() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UTC()
	if !t.lastOnTs.IsZero() {
		// Already tracking an ON event — ignore duplicate.
		t.log.V(1).Info("Duplicate ON event ignored")
		return
	}

	t.lastOnTs = now
	if _, err := t.db.Exec(
		`INSERT INTO pump_events (ts, type) VALUES (?, 'on')`,
		now.Format(time.RFC3339Nano),
	); err != nil {
		t.log.Error(err, "Failed to insert ON event")
	}
	t.log.Info("Pump ON", "ts", now)
}

// recordOff records a pump-off event with elapsed duration in the DB.
func (t *PoolRuntimeTracker) recordOff() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UTC()
	if t.lastOnTs.IsZero() {
		// Not currently ON — ignore stray OFF.
		t.log.V(1).Info("OFF event without prior ON ignored")
		return
	}

	elapsed := int64(now.Sub(t.lastOnTs).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}

	if _, err := t.db.Exec(
		`INSERT INTO pump_events (ts, type, duration_sec) VALUES (?, 'off', ?)`,
		now.Format(time.RFC3339Nano), elapsed,
	); err != nil {
		t.log.Error(err, "Failed to insert OFF event")
	}

	t.accumulated += elapsed
	t.lastOnTs = time.Time{}
	t.log.Info("Pump OFF", "ts", now, "elapsed_sec", elapsed, "daily_total_sec", t.accumulated)
}

// DailyRuntimeSec returns the total seconds the pump has run today.
// If the pump is currently ON, the ongoing run time is included.
func (t *PoolRuntimeTracker) DailyRuntimeSec() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	total := t.accumulated
	if !t.lastOnTs.IsZero() {
		ongoing := int64(time.Since(t.lastOnTs).Seconds())
		if ongoing > 0 {
			total += ongoing
		}
	}
	return total
}

// RemainingRuntimeSec returns how many more seconds the pump should run today
// to reach targetSec. Returns 0 if the target has already been reached.
func (t *PoolRuntimeTracker) RemainingRuntimeSec(targetSec int64) int64 {
	remaining := targetSec - t.DailyRuntimeSec()
	if remaining < 0 {
		return 0
	}
	return remaining
}
