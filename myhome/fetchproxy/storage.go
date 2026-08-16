package fetchproxy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// Storage persists fetch subscriptions in the shared myhome.db (see #465
// "Reuse myhome.db. Do not introduce a new database file"). It follows the
// precedent in myhome/temperature/storage.go: NewStorage takes the shared
// *sqlx.DB handle, creates its table idempotently, and there is no versioned
// migration framework to fight.
//
// last_seen is deliberately NOT a column here: persisting it would reintroduce
// a write on every re-publication and undo the wear saving the change-hash
// buys. It lives in Service's in-memory map instead (see service.go).
type Storage struct {
	db  *sqlx.DB
	log logr.Logger
}

// subscriptionRow is the on-disk shape of a Subscription. Headers is stored
// as a JSON blob, matching the pattern already used for kinds/levels in
// myhome/temperature/storage.go and for info/config in myhome/storage/db.go.
type subscriptionRow struct {
	DeviceID        string    `db:"device_id"`
	Name            string    `db:"name"`
	URL             string    `db:"url"`
	HeadersJSON     string    `db:"headers"`
	Transform       string    `db:"transform"`
	IntervalSeconds int       `db:"interval_seconds"`
	Topic           string    `db:"topic"`
	ChangeHash      string    `db:"change_hash"`
	UpdatedAt       time.Time `db:"updated_at"`
}

// NewStorage creates the fetch_subscriptions table if it does not already
// exist, using the shared database handle (storage.DB() in the daemon).
func NewStorage(log logr.Logger, db *sqlx.DB) (*Storage, error) {
	s := &Storage{
		db:  db,
		log: log.WithName("FetchStorage"),
	}
	if err := s.createTable(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) createTable() error {
	schema := `
	CREATE TABLE IF NOT EXISTS fetch_subscriptions (
		device_id        TEXT NOT NULL,
		name             TEXT NOT NULL,
		url              TEXT NOT NULL,
		headers          TEXT NOT NULL DEFAULT '{}',
		transform        TEXT NOT NULL,
		interval_seconds INTEGER NOT NULL,
		topic            TEXT NOT NULL,
		change_hash      TEXT NOT NULL,
		updated_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (device_id, name)
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		s.log.Error(err, "Failed to create fetch_subscriptions table")
		return err
	}
	return nil
}

// Upsert writes sub if, and only if, its change-hash differs from the
// currently-stored row (or no row exists yet). Returns whether a write
// happened, so the caller can decide whether to log a registration.
//
// The WHERE clause on the ON CONFLICT branch is the mechanism from
// myhome/temperature/storage.go that turns "no actual change" into "no SQL
// write at all" — the whole point of the change-hash (#465, SD-card wear).
func (s *Storage) Upsert(sub Subscription) (bool, error) {
	headersJSON, err := json.Marshal(normalizedHeaders(sub.Headers))
	if err != nil {
		return false, fmt.Errorf("marshal headers: %w", err)
	}
	hash, err := ChangeHash(sub)
	if err != nil {
		return false, fmt.Errorf("compute change hash: %w", err)
	}

	query := `
	INSERT INTO fetch_subscriptions
		(device_id, name, url, headers, transform, interval_seconds, topic, change_hash, updated_at)
	VALUES
		(?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(device_id, name) DO UPDATE SET
		url              = excluded.url,
		headers          = excluded.headers,
		transform        = excluded.transform,
		interval_seconds = excluded.interval_seconds,
		topic            = excluded.topic,
		change_hash      = excluded.change_hash,
		updated_at       = excluded.updated_at
	WHERE fetch_subscriptions.change_hash IS DISTINCT FROM excluded.change_hash
	`
	result, err := s.db.Exec(query,
		sub.DeviceID, sub.Name, sub.URL, string(headersJSON), sub.Transform,
		sub.IntervalSeconds, sub.Topic, hash, time.Now())
	if err != nil {
		s.log.Error(err, "Failed to upsert fetch subscription", "device_id", sub.DeviceID, "name", sub.Name)
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// List returns every persisted subscription, used at boot to resume polling.
func (s *Storage) List() ([]Subscription, error) {
	var rows []subscriptionRow
	if err := s.db.Select(&rows, `SELECT * FROM fetch_subscriptions ORDER BY device_id, name`); err != nil {
		s.log.Error(err, "Failed to list fetch subscriptions")
		return nil, err
	}
	subs := make([]Subscription, 0, len(rows))
	for _, r := range rows {
		sub, err := r.toSubscription()
		if err != nil {
			s.log.Error(err, "Failed to decode stored subscription", "device_id", r.DeviceID, "name", r.Name)
			continue
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

// Get returns a single subscription by (device_id, name).
func (s *Storage) Get(deviceID, name string) (*Subscription, error) {
	var r subscriptionRow
	err := s.db.Get(&r, `SELECT * FROM fetch_subscriptions WHERE device_id = ? AND name = ?`, deviceID, name)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("fetch subscription not found: %s/%s", deviceID, name)
	}
	if err != nil {
		s.log.Error(err, "Failed to get fetch subscription", "device_id", deviceID, "name", name)
		return nil, err
	}
	sub, err := r.toSubscription()
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// Delete removes a subscription by (device_id, name). Cleanup is explicit —
// no TTL, no automatic expiry (#465).
func (s *Storage) Delete(deviceID, name string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM fetch_subscriptions WHERE device_id = ? AND name = ?`, deviceID, name)
	if err != nil {
		s.log.Error(err, "Failed to delete fetch subscription", "device_id", deviceID, "name", name)
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r subscriptionRow) toSubscription() (Subscription, error) {
	var headers map[string]string
	if err := json.Unmarshal([]byte(r.HeadersJSON), &headers); err != nil {
		return Subscription{}, fmt.Errorf("unmarshal headers: %w", err)
	}
	return Subscription{
		DeviceID:        r.DeviceID,
		Name:            r.Name,
		URL:             r.URL,
		Headers:         headers,
		Transform:       r.Transform,
		IntervalSeconds: r.IntervalSeconds,
		Topic:           r.Topic,
	}, nil
}
