package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"
	mqttmock "github.com/asnowfix/home-automation/myhome/mqtt"
	_ "modernc.org/sqlite"
)

// discardLogger returns a no-op logr.Logger for tests.
func discardLogger() logr.Logger {
	return logr.Discard()
}

// newTestTracker creates a PoolRuntimeTracker backed by an in-memory SQLite DB.
func newTestTracker(t *testing.T, mc *mqttmock.RecordingMockClient) *PoolRuntimeTracker {
	t.Helper()
	tracker, err := NewPoolRuntimeTracker(discardLogger(), "file::memory:?cache=shared&_fk=1", mc, "")
	if err != nil {
		t.Fatalf("NewPoolRuntimeTracker: %v", err)
	}
	t.Cleanup(func() { tracker.db.Close() })
	return tracker
}

// insertEvent inserts a raw row directly into pump_events for test setup.
func insertEvent(t *testing.T, db *sql.DB, ts time.Time, evtType string, durationSec *int64) {
	t.Helper()
	if durationSec != nil {
		_, err := db.Exec(
			`INSERT INTO pump_events (ts, type, duration_sec) VALUES (?, ?, ?)`,
			ts.UTC().Format(time.RFC3339Nano), evtType, *durationSec,
		)
		if err != nil {
			t.Fatalf("insertEvent(%s): %v", evtType, err)
		}
	} else {
		_, err := db.Exec(
			`INSERT INTO pump_events (ts, type) VALUES (?, ?)`,
			ts.UTC().Format(time.RFC3339Nano), evtType,
		)
		if err != nil {
			t.Fatalf("insertEvent(%s): %v", evtType, err)
		}
	}
}

// dur is a helper to return a pointer to an int64 duration value.
func dur(d int64) *int64 { return &d }

// makeNotifyStatus creates a minimal Shelly NotifyStatus JSON payload.
func makeNotifyStatus(deviceID string, output bool) []byte {
	payload := map[string]interface{}{
		"method": "NotifyStatus",
		"src":    deviceID,
		"params": map[string]interface{}{
			"switch:0": map[string]interface{}{
				"output": output,
			},
			"ts": float64(time.Now().Unix()),
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

// TestDailyRuntimeSec_MultipleOnOff inserts several on/off pairs for today
// and verifies that DailyRuntimeSec() returns the expected sum.
func TestDailyRuntimeSec_MultipleOnOff(t *testing.T) {
	mc := mqttmock.NewRecordingMockClient()
	tracker := newTestTracker(t, mc)

	now := time.Now().UTC()

	// Simulate three ON/OFF cycles totalling 300 seconds.
	// Cycle 1: 100 s
	insertEvent(t, tracker.db, now.Add(-10*time.Minute), "on", nil)
	insertEvent(t, tracker.db, now.Add(-10*time.Minute+100*time.Second), "off", dur(100))
	// Cycle 2: 150 s
	insertEvent(t, tracker.db, now.Add(-5*time.Minute), "on", nil)
	insertEvent(t, tracker.db, now.Add(-5*time.Minute+150*time.Second), "off", dur(150))
	// Cycle 3: 50 s
	insertEvent(t, tracker.db, now.Add(-1*time.Minute), "on", nil)
	insertEvent(t, tracker.db, now.Add(-1*time.Minute+50*time.Second), "off", dur(50))

	// Re-open tracker from same DB to trigger recoverState.
	tracker2, err := NewPoolRuntimeTracker(discardLogger(), "file::memory:?cache=shared&_fk=1", mc, "")
	if err != nil {
		t.Fatalf("NewPoolRuntimeTracker (reopen): %v", err)
	}
	t.Cleanup(func() { tracker2.db.Close() })

	got := tracker2.DailyRuntimeSec()
	if got != 300 {
		t.Errorf("DailyRuntimeSec() = %d, want 300", got)
	}
}

// TestRestartRecovery pre-populates the DB with completed events, creates a
// new tracker from the same DB, and verifies DailyRuntimeSec() matches.
func TestRestartRecovery(t *testing.T) {
	mc := mqttmock.NewRecordingMockClient()

	// Use a unique in-memory cache name so this test has its own schema.
	dbPath := "file:testrestart?mode=memory&cache=shared&_fk=1"

	setup, err := NewPoolRuntimeTracker(discardLogger(), dbPath, mc, "")
	if err != nil {
		t.Fatalf("setup NewPoolRuntimeTracker: %v", err)
	}

	now := time.Now().UTC()
	insertEvent(t, setup.db, now.Add(-20*time.Minute), "on", nil)
	insertEvent(t, setup.db, now.Add(-20*time.Minute+200*time.Second), "off", dur(200))
	insertEvent(t, setup.db, now.Add(-5*time.Minute), "on", nil)
	insertEvent(t, setup.db, now.Add(-5*time.Minute+60*time.Second), "off", dur(60))

	// Simulate daemon restart: create a new tracker against the same DB.
	tracker, err := NewPoolRuntimeTracker(discardLogger(), dbPath, mc, "")
	if err != nil {
		t.Fatalf("restart NewPoolRuntimeTracker: %v", err)
	}
	t.Cleanup(func() {
		tracker.db.Close()
		setup.db.Close()
	})

	got := tracker.DailyRuntimeSec()
	if got != 260 {
		t.Errorf("DailyRuntimeSec() after restart = %d, want 260", got)
	}
}

// TestPumpOnAtRestart pre-populates with a type='on' row and no subsequent
// OFF, then verifies the tracker counts elapsed time as ongoing.
func TestPumpOnAtRestart(t *testing.T) {
	mc := mqttmock.NewRecordingMockClient()
	dbPath := "file:testpumpon?mode=memory&cache=shared&_fk=1"

	setup, err := NewPoolRuntimeTracker(discardLogger(), dbPath, mc, "")
	if err != nil {
		t.Fatalf("setup NewPoolRuntimeTracker: %v", err)
	}

	// Insert a completed cycle (100 s) then an open ON from 30 s ago.
	now := time.Now().UTC()
	insertEvent(t, setup.db, now.Add(-10*time.Minute), "on", nil)
	insertEvent(t, setup.db, now.Add(-10*time.Minute+100*time.Second), "off", dur(100))
	insertEvent(t, setup.db, now.Add(-30*time.Second), "on", nil)

	tracker, err := NewPoolRuntimeTracker(discardLogger(), dbPath, mc, "")
	if err != nil {
		t.Fatalf("restart NewPoolRuntimeTracker: %v", err)
	}
	t.Cleanup(func() {
		tracker.db.Close()
		setup.db.Close()
	})

	got := tracker.DailyRuntimeSec()
	// 100 s completed + at least 30 s ongoing.
	if got < 130 {
		t.Errorf("DailyRuntimeSec() with ongoing run = %d, want >= 130", got)
	}
	// Sanity upper bound: pump wasn't on for more than 5 min.
	if got > 100+5*60 {
		t.Errorf("DailyRuntimeSec() with ongoing run = %d, unexpectedly large", got)
	}
}

// TestRemainingRuntimeSec verifies RemainingRuntimeSec returns max(0, target-daily).
func TestRemainingRuntimeSec(t *testing.T) {
	mc := mqttmock.NewRecordingMockClient()
	dbPath := "file:testremaining?mode=memory&cache=shared&_fk=1"

	tracker, err := NewPoolRuntimeTracker(discardLogger(), dbPath, mc, "")
	if err != nil {
		t.Fatalf("NewPoolRuntimeTracker: %v", err)
	}
	t.Cleanup(func() { tracker.db.Close() })

	now := time.Now().UTC()
	insertEvent(t, tracker.db, now.Add(-10*time.Minute), "on", nil)
	insertEvent(t, tracker.db, now.Add(-10*time.Minute+200*time.Second), "off", dur(200))

	// Reload to pick up the 200 s.
	tracker2, err := NewPoolRuntimeTracker(discardLogger(), dbPath, mc, "")
	if err != nil {
		t.Fatalf("NewPoolRuntimeTracker reload: %v", err)
	}
	t.Cleanup(func() { tracker2.db.Close() })

	// Target not yet reached.
	got := tracker2.RemainingRuntimeSec(500)
	if got != 300 {
		t.Errorf("RemainingRuntimeSec(500) = %d, want 300", got)
	}

	// Target already exceeded.
	got = tracker2.RemainingRuntimeSec(100)
	if got != 0 {
		t.Errorf("RemainingRuntimeSec(100) = %d, want 0 (already exceeded)", got)
	}
}

// TestMQTTOnOffTransitions verifies that live ON/OFF MQTT messages update
// the runtime accumulator correctly.
func TestMQTTOnOffTransitions(t *testing.T) {
	mc := mqttmock.NewRecordingMockClient()
	dbPath := "file:testmqtt?mode=memory&cache=shared&_fk=1"
	deviceID := "shellyplus1pm-aabbccddee"

	tracker, err := NewPoolRuntimeTracker(discardLogger(), dbPath, mc, deviceID)
	if err != nil {
		t.Fatalf("NewPoolRuntimeTracker: %v", err)
	}
	t.Cleanup(func() { tracker.db.Close() })

	ctx := context.Background()
	if err := tracker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The tracker subscribes to "+/events/rpc"; the mock matches on exact topic.
	// Feed using the subscription topic so the handler is invoked.
	subTopic := "+/events/rpc"

	// Turn pump ON.
	mc.Feed(subTopic, makeNotifyStatus(deviceID, true))

	// Let handleMessage run (it is sync via Feed).
	tracker.mu.Lock()
	onTs := tracker.lastOnTs
	tracker.mu.Unlock()

	if onTs.IsZero() {
		t.Fatal("Expected pump to be ON after ON event")
	}

	// Wait a moment so elapsed time is non-zero.
	time.Sleep(10 * time.Millisecond)

	// Turn pump OFF.
	mc.Feed(subTopic, makeNotifyStatus(deviceID, false))

	tracker.mu.Lock()
	offTs := tracker.lastOnTs
	acc := tracker.accumulated
	tracker.mu.Unlock()

	if !offTs.IsZero() {
		t.Fatal("Expected pump to be OFF after OFF event")
	}
	// accumulated may be 0 if the ON→OFF happened within the same second (integer
	// truncation). Accept 0 as long as the pump is recorded as OFF.
	if acc < 0 {
		t.Errorf("accumulated = %d, want >= 0", acc)
	}
	// DailyRuntimeSec should equal accumulated when pump is OFF.
	if tracker.DailyRuntimeSec() != acc {
		t.Errorf("DailyRuntimeSec() = %d, want %d", tracker.DailyRuntimeSec(), acc)
	}
}
