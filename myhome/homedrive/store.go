package homedrive

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// Store persists known instances in myhome.db so a dead agent is still shown
// (offline) after a daemon restart: retained MQTT messages do not survive a
// broker restart, and the daemon restarts every two days.
//
// Only identity, last ts and the last payload are kept. The Cache throttles
// writes to one per instance per PersistEvery.
type Store struct{ db *sqlx.DB }

// Row is one persisted instance.
type Row struct {
	Host       string `db:"host"`
	User       string `db:"user"`
	LastTs     int64  `db:"last_ts"`     // Unix seconds
	LastStatus string `db:"last_status"` // the raw payload
}

// NewStore creates the table if needed (idempotent, like the other myhome.db
// tables).
func NewStore(db *sqlx.DB) (*Store, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS homedrive_instances (
		host        TEXT NOT NULL,
		user        TEXT NOT NULL,
		last_ts     INTEGER NOT NULL,
		last_status TEXT NOT NULL,
		PRIMARY KEY (host, user)
	)`)
	if err != nil {
		return nil, fmt.Errorf("create homedrive_instances: %w", err)
	}
	return &Store{db: db}, nil
}

// Save inserts or replaces one row.
func (s *Store) Save(r Row) error {
	_, err := s.db.Exec(`INSERT INTO homedrive_instances (host, user, last_ts, last_status)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(host, user) DO UPDATE SET last_ts = excluded.last_ts, last_status = excluded.last_status`,
		r.Host, r.User, r.LastTs, r.LastStatus)
	if err != nil {
		return fmt.Errorf("save homedrive instance: %w", err)
	}
	return nil
}

// Load returns all rows, newest first.
func (s *Store) Load() ([]Row, error) {
	var rows []Row
	if err := s.db.Select(&rows, `SELECT host, user, last_ts, last_status FROM homedrive_instances ORDER BY last_ts DESC`); err != nil {
		return nil, fmt.Errorf("load homedrive instances: %w", err)
	}
	return rows, nil
}

// Delete removes one row.
func (s *Store) Delete(host, user string) error {
	if _, err := s.db.Exec(`DELETE FROM homedrive_instances WHERE host = ? AND user = ?`, host, user); err != nil {
		return fmt.Errorf("delete homedrive instance: %w", err)
	}
	return nil
}
