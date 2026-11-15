// Package metricsdb is a small SQLite time-series store for device-health
// values (RSSI, uptime, free heap, ...). It is separate from events.db:
// events.db records what happened, metrics.db records values over time.
//
// Layout (see docs/observability/observability-study.md, "Storage"): samples
// are WITHOUT ROWID, keyed (ts, series_id) with time first and no secondary
// index, so that inserts append at the right edge of the b-tree. A
// series-first key or a secondary index measured ~3.7-4 GiB of writes a day
// on a workstation against ~56-72 MiB for this layout: do not add one.
//
// The store is advisory. Every method is safe on a nil *Store (no-op), so a
// daemon whose store failed to open simply runs without it.
package metricsdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

const (
	// DefaultRetention is how long samples are kept.
	DefaultRetention = 7 * 24 * time.Hour
	// FlushInterval is how often buffered samples are written, in one transaction.
	FlushInterval = time.Minute
	// purgeChunk is the time span deleted per statement during retention.
	purgeChunk = time.Hour
	// maxBuffered bounds memory if flushes keep failing; the oldest samples are dropped.
	maxBuffered = 100000
)

// Sample is one value of one metric of one device at one instant.
type Sample struct {
	DeviceID string
	Metric   string
	Ts       time.Time
	Value    float64
}

// Point is one (possibly downsampled) value returned by Query.
type Point struct {
	Ts    int64   `db:"ts"    json:"ts"` // unix seconds
	Value float64 `db:"value" json:"value"`
}

type seriesKey struct{ device, metric string }

// Store is the metrics database plus its write buffer.
type Store struct {
	db  *sqlx.DB
	log logr.Logger

	mu      sync.Mutex // guards buf and dropped
	buf     []Sample
	dropped int

	flushMu sync.Mutex          // serializes Flush; guards series
	series  map[seriesKey]int64 // series_id cache
}

// Open opens (creating if needed) the database at path. A relative filename
// is used as is; for a path with a directory part the directory is created.
func Open(log logr.Logger, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create metrics database directory: %w", err)
		}
	}
	db, err := sqlx.Connect("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open metrics database: %w", err)
	}
	// One connection: ":memory:" would otherwise give each pooled connection
	// its own empty database, and SQLite serializes writers anyway.
	db.SetMaxOpenConns(1)
	for _, p := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=NORMAL`} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, err
		}
	}
	const schema = `
CREATE TABLE IF NOT EXISTS series (
    series_id INTEGER PRIMARY KEY,
    device_id TEXT NOT NULL,
    metric    TEXT NOT NULL,
    UNIQUE (device_id, metric)
);
CREATE TABLE IF NOT EXISTS samples (
    ts        INTEGER NOT NULL,
    series_id INTEGER NOT NULL,
    value     REAL    NOT NULL,
    PRIMARY KEY (ts, series_id)
) WITHOUT ROWID;`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create metrics schema: %w", err)
	}
	return &Store{
		db:     db,
		log:    log.WithName("MetricsStore"),
		series: make(map[seriesKey]int64),
	}, nil
}

// Close flushes buffered samples (best effort) and closes the database.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		s.log.Error(err, "Final flush failed")
	}
	return s.db.Close()
}

// Add buffers a sample; it is written by the next Flush. It never touches the
// disk, so a collector may call it freely.
func (s *Store) Add(smp Sample) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) >= maxBuffered {
		s.buf = s.buf[1:]
		s.dropped++
	}
	s.buf = append(s.buf, smp)
}

// Flush writes all buffered samples in a single transaction. On failure the
// samples are put back (ahead of newer ones) for the next attempt.
func (s *Store) Flush(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	s.mu.Lock()
	batch := s.buf
	s.buf = nil
	dropped := s.dropped
	s.dropped = 0
	s.mu.Unlock()
	if dropped > 0 {
		s.log.Info("Dropped samples: buffer full", "count", dropped)
	}
	if len(batch) == 0 {
		return nil
	}
	if err := s.write(ctx, batch); err != nil {
		s.mu.Lock()
		s.buf = append(batch, s.buf...)
		if over := len(s.buf) - maxBuffered; over > 0 {
			s.buf = s.buf[over:]
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) write(ctx context.Context, batch []Sample) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Series ids resolved in this transaction are cached only after commit,
	// so a rollback cannot leave the cache pointing at rows that do not exist.
	fresh := make(map[seriesKey]int64)
	for _, smp := range batch {
		k := seriesKey{smp.DeviceID, smp.Metric}
		id, ok := s.series[k]
		if !ok {
			if id, ok = fresh[k]; !ok {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO series (device_id, metric) VALUES (?, ?)`, k.device, k.metric); err != nil {
					return err
				}
				if err := tx.GetContext(ctx, &id, `SELECT series_id FROM series WHERE device_id = ? AND metric = ?`, k.device, k.metric); err != nil {
					return err
				}
				fresh[k] = id
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO samples (ts, series_id, value) VALUES (?, ?, ?)`, smp.Ts.Unix(), id, smp.Value); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for k, id := range fresh {
		s.series[k] = id
	}
	return nil
}

// Purge deletes samples older than cutoff, one purgeChunk of time per
// statement so that the write lock is never held for long. It returns the
// number of rows deleted.
func (s *Store) Purge(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil {
		return 0, nil
	}
	var total int64
	limit := cutoff.Unix()
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		var oldest sql.NullInt64
		if err := s.db.GetContext(ctx, &oldest, `SELECT MIN(ts) FROM samples`); err != nil {
			return total, err
		}
		if !oldest.Valid || oldest.Int64 >= limit {
			return total, nil
		}
		upper := min(oldest.Int64+int64(purgeChunk/time.Second), limit)
		res, err := s.db.ExecContext(ctx, `DELETE FROM samples WHERE ts < ?`, upper)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
}

// Query returns one series over [from, to), oldest first. With step >= 1s the
// points are averaged into buckets of step (bucket start timestamps);
// otherwise raw samples are returned. An unknown series yields no points.
func (s *Store) Query(ctx context.Context, deviceID, metric string, from, to time.Time, step time.Duration) ([]Point, error) {
	if s == nil {
		return nil, nil
	}
	var id int64
	err := s.db.GetContext(ctx, &id, `SELECT series_id FROM series WHERE device_id = ? AND metric = ?`, deviceID, metric)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	pts := []Point{}
	if secs := int64(step / time.Second); secs > 0 {
		err = s.db.SelectContext(ctx, &pts, `
SELECT (ts / ?) * ? AS ts, AVG(value) AS value FROM samples
WHERE series_id = ? AND ts >= ? AND ts < ?
GROUP BY 1 ORDER BY 1`, secs, secs, id, from.Unix(), to.Unix())
	} else {
		err = s.db.SelectContext(ctx, &pts, `
SELECT ts, value FROM samples
WHERE series_id = ? AND ts >= ? AND ts < ? ORDER BY ts`, id, from.Unix(), to.Unix())
	}
	return pts, err
}

// Start flushes every FlushInterval and purges hourly (and once at startup)
// until ctx is done. Errors are logged, never fatal. Close does the final flush.
func (s *Store) Start(ctx context.Context, retention time.Duration) {
	if s == nil {
		return
	}
	flush := time.NewTicker(FlushInterval)
	defer flush.Stop()
	purge := time.NewTicker(time.Hour)
	defer purge.Stop()

	s.purgeOnce(ctx, retention)
	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			if err := s.Flush(ctx); err != nil {
				s.log.Error(err, "Failed to flush metric samples")
			}
		case <-purge.C:
			s.purgeOnce(ctx, retention)
		}
	}
}

func (s *Store) purgeOnce(ctx context.Context, retention time.Duration) {
	if retention <= 0 {
		return
	}
	cutoff := time.Now().Add(-retention)
	n, err := s.Purge(ctx, cutoff)
	if err != nil {
		s.log.Error(err, "Failed to purge old metric samples")
	} else if n > 0 {
		s.log.Info("Purged old metric samples", "count", n, "cutoff", cutoff)
	}
}
