package vacuum

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// Storage persists the robot's non-secret facts (name, last known address) and
// its learned regions in the shared myhome.db, following
// myhome/fetchproxy/storage.go: idempotent CREATE TABLE.
//
// The robot's local password is NOT stored here: like every other integration
// secret it comes from MYHOME_VACUUM_PASSWORD in the daemon's .env (#626).
type Storage struct {
	db  *sqlx.DB
	log logr.Logger
}

type robotRecord struct {
	BLID string `db:"blid"`
	Name string `db:"name"`
	Host string `db:"host"` // last address the robot was reached at; a hint, not identity
}

type regionRecord struct {
	BLID       string `db:"blid"`
	MapID      string `db:"pmap_id"`
	MapVersion string `db:"pmap_version"`
	RegionID   string `db:"region_id"`
	RegionType string `db:"region_type"`
	Name       string `db:"name"`
}

// NewStorage creates the vacuum tables if needed and migrates older ones.
func NewStorage(log logr.Logger, db *sqlx.DB) (*Storage, error) {
	s := &Storage{db: db, log: log.WithName("VacuumStorage")}
	schema := `
	CREATE TABLE IF NOT EXISTS vacuum_robots (
		blid       TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		host       TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS vacuum_regions (
		blid         TEXT NOT NULL,
		pmap_id      TEXT NOT NULL,
		pmap_version TEXT NOT NULL,
		region_id    TEXT NOT NULL,
		region_type  TEXT NOT NULL,
		name         TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (blid, pmap_id, region_id)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		s.log.Error(err, "Failed to create vacuum tables")
		return nil, err
	}
	if err := s.dropPasswordColumn(); err != nil {
		s.log.Error(err, "Failed to migrate vacuum_robots")
		return nil, err
	}
	return s, nil
}

// dropPasswordColumn removes vacuum_robots.password, where an earlier build of
// #606 kept the robot's local password in plaintext. secure_delete makes SQLite
// zero the freed content instead of leaving the old value in free pages.
func (s *Storage) dropPasswordColumn() error {
	var count int
	if err := s.db.Get(&count, `SELECT COUNT(*) FROM pragma_table_info('vacuum_robots') WHERE name='password'`); err != nil {
		return fmt.Errorf("failed to check for vacuum_robots.password: %w", err)
	}
	if count == 0 {
		return nil
	}
	s.log.Info("Removing the stored robot password from vacuum_robots (now read from MYHOME_VACUUM_PASSWORD)")
	ctx := context.Background()
	conn, err := s.db.Connx(ctx) // PRAGMA secure_delete is per connection
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA secure_delete = ON`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(ctx, `PRAGMA secure_delete = OFF`) }()
	if _, err := conn.ExecContext(ctx, `UPDATE vacuum_robots SET password = ''`); err != nil {
		return fmt.Errorf("failed to clear vacuum_robots.password: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `ALTER TABLE vacuum_robots DROP COLUMN password`); err != nil {
		return fmt.Errorf("failed to drop vacuum_robots.password: %w", err)
	}
	return nil
}

// robot returns the stored facts for blid, creating an empty row the first time
// so the later name and host updates have something to update.
func (s *Storage) robot(blid string) (robotRecord, error) {
	if _, err := s.db.Exec(`INSERT INTO vacuum_robots (blid) VALUES (?) ON CONFLICT(blid) DO NOTHING`, blid); err != nil {
		return robotRecord{}, err
	}
	var r robotRecord
	err := s.db.Get(&r, `SELECT blid, name, host FROM vacuum_robots WHERE blid=?`, blid)
	return r, err
}

func (s *Storage) saveHost(blid, host string) error {
	_, err := s.db.Exec(`UPDATE vacuum_robots SET host=?, updated_at=CURRENT_TIMESTAMP WHERE blid=?`, host, blid)
	return err
}

func (s *Storage) saveName(blid, name string) error {
	_, err := s.db.Exec(`UPDATE vacuum_robots SET name=?, updated_at=CURRENT_TIMESTAMP WHERE blid=?`, name, blid)
	return err
}

func (s *Storage) regions(blid string) ([]regionRecord, error) {
	var rs []regionRecord
	err := s.db.Select(&rs, `
		SELECT blid, pmap_id, pmap_version, region_id, region_type, name
		FROM vacuum_regions WHERE blid=? ORDER BY name, region_id`, blid)
	return rs, err
}

// learnRegion records a region seen in a room-clean command. An existing name is kept.
func (s *Storage) learnRegion(r regionRecord) error {
	_, err := s.db.NamedExec(`
		INSERT INTO vacuum_regions (blid, pmap_id, pmap_version, region_id, region_type)
		VALUES (:blid, :pmap_id, :pmap_version, :region_id, :region_type)
		ON CONFLICT(blid, pmap_id, region_id) DO UPDATE SET
			pmap_version=excluded.pmap_version, region_type=excluded.region_type`, r)
	return err
}

// nameRegion names every learned region with this id (normally exactly one).
func (s *Storage) nameRegion(blid, regionID, name string) (int64, error) {
	res, err := s.db.Exec(`UPDATE vacuum_regions SET name=? WHERE blid=? AND region_id=?`, name, blid, regionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
