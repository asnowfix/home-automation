package events

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type Event struct {
	ID         int64   `db:"id"          json:"id"`
	Ts         float64 `db:"ts"          json:"ts"`
	ReceivedAt float64 `db:"received_at" json:"received_at"`
	DeviceID   string  `db:"device_id"   json:"device_id"`
	Component  string  `db:"component"   json:"component"`
	Event      string  `db:"event"       json:"event"`
	Severity   string  `db:"severity"    json:"severity"`
	Data       *string `db:"data"        json:"data,omitempty"`
}

type DailyStat struct {
	Date      string  `db:"date"`
	DeviceID  string  `db:"device_id"`
	Component string  `db:"component"`
	Metric    string  `db:"metric"`
	MinVal    float64 `db:"min_val"`
	MaxVal    float64 `db:"max_val"`
	SumVal    float64 `db:"sum_val"`
	Samples   int64   `db:"samples"`
	UpdatedAt float64 `db:"updated_at"`
}

type Query struct {
	DeviceIDs []string // IN match; takes precedence over DeviceID when set
	DeviceID  string   // exact match (used when DeviceIDs is empty)
	EventType string
	Severity  string
	Since     time.Duration
	Limit     int
	Offset    int
}

type Storage struct {
	db  *sqlx.DB
	log logr.Logger
}

func NewStorage(log logr.Logger, dbPath string) (*Storage, error) {
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Error(err, "Failed to create events database directory", "dir", dir)
			return nil, err
		}
	}
	db, err := sqlx.Connect("sqlite", dbPath)
	if err != nil {
		log.Error(err, "Failed to connect to events database", "dbPath", dbPath)
		return nil, err
	}
	// A pooled second connection to ":memory:" opens its own empty database
	// (no shared-cache URI is used here), so reads on that connection would
	// silently see none of the rows written on the first. Pin the pool to a
	// single connection to keep all reads/writes on the same in-memory (or
	// file) database; SQLite serializes writers anyway.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA synchronous=NORMAL`); err != nil {
		db.Close()
		return nil, err
	}

	s := &Storage{
		db:  db,
		log: log.WithName("EventStorage"),
	}

	if err := s.createTables(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Storage) createTables() error {
	schema := `
CREATE TABLE IF NOT EXISTS events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          REAL    NOT NULL,
    received_at REAL    NOT NULL,
    device_id   TEXT    NOT NULL,
    component   TEXT    NOT NULL,
    event       TEXT    NOT NULL,
    severity    TEXT    NOT NULL DEFAULT 'info',
    data        TEXT,
    UNIQUE (device_id, component, event, ts)
);
CREATE INDEX IF NOT EXISTS events_ts       ON events (ts DESC);
CREATE INDEX IF NOT EXISTS events_device   ON events (device_id, ts DESC);
CREATE INDEX IF NOT EXISTS events_event    ON events (event, ts DESC);
CREATE INDEX IF NOT EXISTS events_severity ON events (severity, ts DESC);

CREATE TABLE IF NOT EXISTS sensor_daily_stats (
    date        TEXT    NOT NULL,
    device_id   TEXT    NOT NULL,
    component   TEXT    NOT NULL,
    metric      TEXT    NOT NULL,
    min_val     REAL,
    max_val     REAL,
    sum_val     REAL    DEFAULT 0,
    samples     INTEGER DEFAULT 0,
    updated_at  REAL    NOT NULL,
    PRIMARY KEY (date, device_id, component, metric)
);`

	_, err := s.db.Exec(schema)
	if err != nil {
		s.log.Error(err, "Failed to create events schema")
		return err
	}

	// Migration: check events table exists (use COUNT(*) pattern from myhome/storage/db.go)
	var count int
	err = s.db.Get(&count, `SELECT COUNT(*) FROM pragma_table_info('events') WHERE name='severity'`)
	if err != nil {
		s.log.Error(err, "Failed to check events table columns")
		return err
	}

	return nil
}

func (s *Storage) Record(ctx context.Context, e Event) error {
	if e.ReceivedAt == 0 {
		e.ReceivedAt = float64(time.Now().Unix())
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO events (ts, received_at, device_id, component, event, severity, data)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Ts, e.ReceivedAt, e.DeviceID, e.Component, e.Event, e.Severity, e.Data,
	)
	if err != nil {
		s.log.Error(err, "Failed to record event", "device_id", e.DeviceID, "event", e.Event)
	}
	return err
}

func (s *Storage) Query(ctx context.Context, q Query) ([]Event, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 500
	}

	parts := []string{"1=1"}
	args := []interface{}{}

	if len(q.DeviceIDs) > 0 {
		placeholders := make([]string, len(q.DeviceIDs))
		for i, id := range q.DeviceIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		parts = append(parts, "device_id IN ("+strings.Join(placeholders, ",")+")")
	} else if q.DeviceID != "" {
		parts = append(parts, "device_id = ?")
		args = append(args, q.DeviceID)
	}
	if q.EventType != "" {
		parts = append(parts, "event LIKE ?||'%'")
		args = append(args, q.EventType)
	}
	if q.Severity != "" {
		parts = append(parts, "severity = ?")
		args = append(args, q.Severity)
	}
	if q.Since > 0 {
		parts = append(parts, "ts >= ?")
		args = append(args, float64(time.Now().Add(-q.Since).Unix()))
	}

	where := strings.Join(parts, " AND ")
	query := fmt.Sprintf(`SELECT id, ts, received_at, device_id, component, event, severity, data
        FROM events WHERE %s ORDER BY ts DESC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, q.Offset)

	var events []Event
	err := s.db.SelectContext(ctx, &events, query, args...)
	if err != nil {
		s.log.Error(err, "Failed to query events")
		return nil, err
	}
	return events, nil
}

func (s *Storage) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, float64(before.Unix()))
	if err != nil {
		s.log.Error(err, "Failed to purge events", "before", before)
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Storage) UpsertDailyStat(ctx context.Context, stat DailyStat) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO sensor_daily_stats
             (date, device_id, component, metric, min_val, max_val, sum_val, samples, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stat.Date, stat.DeviceID, stat.Component, stat.Metric,
		stat.MinVal, stat.MaxVal, stat.SumVal, stat.Samples, stat.UpdatedAt,
	)
	if err != nil {
		s.log.Error(err, "Failed to upsert daily stat",
			"date", stat.Date, "device_id", stat.DeviceID, "metric", stat.Metric)
	}
	return err
}

func (s *Storage) LoadTodayStats(ctx context.Context, date string) ([]DailyStat, error) {
	var stats []DailyStat
	err := s.db.SelectContext(ctx, &stats,
		`SELECT date, device_id, component, metric, min_val, max_val, sum_val, samples, updated_at
         FROM sensor_daily_stats WHERE date = ?`, date)
	if err != nil {
		s.log.Error(err, "Failed to load today stats", "date", date)
		return nil, err
	}
	return stats, nil
}

// OnDurationSec returns the total seconds a component spent in the "on" state
// on the given date (YYYY-MM-DD format).  Each off→on transition contributes
// (next_off.ts - on.ts) to the sum; if no off event follows the most recent on
// (component is currently active), the open interval is measured to now.
// Consecutive on events without an intervening off are deduplicated — only the
// first in each run is counted.
func (s *Storage) OnDurationSec(ctx context.Context, deviceID, component, onEvent, offEvent, date string) (int64, error) {
	const q = `
SELECT COALESCE(SUM(
    COALESCE(
        (SELECT MIN(e2.ts) FROM events e2
         WHERE e2.device_id = e1.device_id
           AND e2.component  = e1.component
           AND e2.event      = ?
           AND e2.ts         > e1.ts),
        unixepoch('now')
    ) - e1.ts
), 0)
FROM events e1
WHERE e1.device_id = ?
  AND e1.component = ?
  AND e1.event     = ?
  AND date(e1.ts, 'unixepoch') = ?
  AND COALESCE(
      (SELECT e0.event FROM events e0
       WHERE e0.device_id = e1.device_id
         AND e0.component  = e1.component
         AND (e0.event = ? OR e0.event = ?)
         AND e0.ts         < e1.ts
       ORDER BY e0.ts DESC LIMIT 1),
      ?
  ) = ?`
	var sec float64
	err := s.db.QueryRowContext(ctx, q,
		offEvent,                           // correlated subquery: min off.ts after this on
		deviceID, component, onEvent, date, // outer WHERE
		onEvent, offEvent, // previous-event lookup: on OR off
		offEvent, // COALESCE default: no prior event → treat as off
		offEvent, // final equality: previous must be off
	).Scan(&sec)
	if err != nil {
		return 0, err
	}
	return int64(sec), nil
}

func (s *Storage) DB() *sqlx.DB {
	return s.db
}

func (s *Storage) Close() {
	s.db.Close()
}
