package alert

import (
	"context"
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// row is one open fingerprint in alert_state (myhome.db). Times are unix seconds.
type row struct {
	Fingerprint  string `db:"fingerprint"`
	Rule         string `db:"rule"`
	Key          string `db:"alert_key"`
	State        string `db:"state"` // pending, firing or resolving
	Severity     string `db:"severity"`
	Source       string `db:"source"`
	PendingSince int64  `db:"pending_since"`
	Since        int64  `db:"since"`
	HoldStart    int64  `db:"hold_start"`
	Summary      string `db:"summary"` // last texts, so a resolve can be built for a key no longer observed
	Detail       string `db:"detail"`
	Labels       string `db:"labels"` // JSON
}

const schema = `CREATE TABLE IF NOT EXISTS alert_state (
	fingerprint   TEXT PRIMARY KEY,
	rule          TEXT NOT NULL,
	alert_key     TEXT NOT NULL,
	state         TEXT NOT NULL,
	severity      TEXT NOT NULL,
	source        TEXT NOT NULL,
	pending_since INTEGER NOT NULL,
	since         INTEGER NOT NULL,
	hold_start    INTEGER NOT NULL DEFAULT 0,
	summary       TEXT NOT NULL DEFAULT '',
	detail        TEXT NOT NULL DEFAULT '',
	labels        TEXT NOT NULL DEFAULT '{}'
)`

// Store persists alert_state. It is the only thing that survives the daemon's 2-day restart.
type Store struct{ db *sqlx.DB }

// NewStore uses the daemon's myhome.db handle and creates the table (new table: no column migration yet).
func NewStore(db *sqlx.DB) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) load(ctx context.Context) ([]row, error) {
	var rows []row
	err := s.db.SelectContext(ctx, &rows, `SELECT * FROM alert_state`)
	return rows, err
}

func (s *Store) save(ctx context.Context, r *row) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT OR REPLACE INTO alert_state
		(fingerprint, rule, alert_key, state, severity, source, pending_since, since, hold_start, summary, detail, labels)
		VALUES (:fingerprint, :rule, :alert_key, :state, :severity, :source, :pending_since, :since, :hold_start, :summary, :detail, :labels)`, r)
	return err
}

func (s *Store) delete(ctx context.Context, fingerprint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM alert_state WHERE fingerprint = ?`, fingerprint)
	return err
}

func encodeLabels(l map[string]string) string {
	b, err := json.Marshal(l)
	if err != nil || l == nil {
		return "{}"
	}
	return string(b)
}

func decodeLabels(s string) map[string]string {
	var l map[string]string
	_ = json.Unmarshal([]byte(s), &l)
	return l
}
