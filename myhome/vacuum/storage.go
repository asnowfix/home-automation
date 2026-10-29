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
	MapVersion string `db:"pmap_version"` // learned: the robot's version then; cloud: the cloud's active version
	RegionID   string `db:"region_id"`
	RegionType string `db:"region_type"` // rid or zid, as sent in a clean command
	Name       string `db:"name"`
	Source     string `db:"source"`   // myhome.VacuumSourceRobot or myhome.VacuumSourceCloud
	MapName    string `db:"map_name"` // cloud only
	Kind       string `db:"kind"`     // cloud room type (kitchen, ...)
	Removed    bool   `db:"removed"`  // cloud only: the last sync no longer listed it
}

// regionColumns are the columns added to vacuum_regions by #628 (cloud sync).
var regionColumns = []struct{ name, def string }{
	{"source", `TEXT NOT NULL DEFAULT 'robot'`},
	{"map_name", `TEXT NOT NULL DEFAULT ''`},
	{"kind", `TEXT NOT NULL DEFAULT ''`},
	{"removed", `INTEGER NOT NULL DEFAULT 0`},
	{"synced_at", `TIMESTAMP`},
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
	if err := s.addRegionColumns(); err != nil {
		s.log.Error(err, "Failed to migrate vacuum_regions")
		return nil, err
	}
	return s, nil
}

// addRegionColumns adds the cloud-sync columns to a vacuum_regions table
// created by an earlier build (AGENTS.md "Database Patterns"). Existing rows
// were learned from the robot, which the 'robot' default records.
func (s *Storage) addRegionColumns() error {
	for _, c := range regionColumns {
		var count int
		if err := s.db.Get(&count, `SELECT COUNT(*) FROM pragma_table_info('vacuum_regions') WHERE name=?`, c.name); err != nil {
			return fmt.Errorf("failed to check for vacuum_regions.%s: %w", c.name, err)
		}
		if count > 0 {
			continue
		}
		s.log.Info("Adding column to vacuum_regions", "column", c.name)
		if _, err := s.db.Exec(`ALTER TABLE vacuum_regions ADD COLUMN ` + c.name + ` ` + c.def); err != nil {
			return fmt.Errorf("failed to add vacuum_regions.%s: %w", c.name, err)
		}
	}
	return nil
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
		SELECT blid, pmap_id, pmap_version, region_id, region_type, name, source, map_name, kind, removed
		FROM vacuum_regions WHERE blid=? ORDER BY name, region_id`, blid)
	return rs, err
}

// learnRegion records a region seen in a room-clean command. An existing name
// is kept, and a region the cloud provided stays a cloud region: the cloud
// owns its name and its map version.
func (s *Storage) learnRegion(r regionRecord) error {
	_, err := s.db.NamedExec(`
		INSERT INTO vacuum_regions (blid, pmap_id, pmap_version, region_id, region_type)
		VALUES (:blid, :pmap_id, :pmap_version, :region_id, :region_type)
		ON CONFLICT(blid, pmap_id, region_id) DO UPDATE SET
			pmap_version=CASE WHEN source='cloud' THEN pmap_version ELSE excluded.pmap_version END,
			region_type=excluded.region_type`, r)
	return err
}

// syncCounts is what syncCloud changed.
type syncCounts struct{ added, updated, stale int }

// syncCloud stores the regions fetched from the cloud, in one transaction: each
// is upserted as a cloud region, overwriting its local name (the cloud is the
// source of truth for names), and every cloud region of this robot the cloud no
// longer lists is marked removed rather than deleted. A region learned from the
// robot with the same map and id becomes a cloud region.
func (s *Storage) syncCloud(blid string, regions []regionRecord) (syncCounts, error) {
	var c syncCounts
	tx, err := s.db.Beginx()
	if err != nil {
		return c, err
	}
	defer func() { _ = tx.Rollback() }()

	type key struct{ m, r string }
	seen := map[key]bool{}
	for _, r := range regions {
		r.BLID, r.Source = blid, "cloud"
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM vacuum_regions WHERE blid=? AND pmap_id=? AND region_id=?`,
			blid, r.MapID, r.RegionID); err != nil {
			return c, err
		}
		if n == 0 {
			c.added++
		} else {
			c.updated++
		}
		if _, err := tx.NamedExec(`
			INSERT INTO vacuum_regions (blid, pmap_id, pmap_version, region_id, region_type, name, source, map_name, kind, removed, synced_at)
			VALUES (:blid, :pmap_id, :pmap_version, :region_id, :region_type, :name, 'cloud', :map_name, :kind, 0, CURRENT_TIMESTAMP)
			ON CONFLICT(blid, pmap_id, region_id) DO UPDATE SET
				pmap_version=excluded.pmap_version, region_type=excluded.region_type, name=excluded.name,
				source='cloud', map_name=excluded.map_name, kind=excluded.kind, removed=0, synced_at=CURRENT_TIMESTAMP`, r); err != nil {
			return c, err
		}
		seen[key{r.MapID, r.RegionID}] = true
	}

	var current []regionRecord
	if err := tx.Select(&current, `
		SELECT blid, pmap_id, pmap_version, region_id, region_type, name, source, map_name, kind, removed
		FROM vacuum_regions WHERE blid=? AND source='cloud' AND removed=0`, blid); err != nil {
		return c, err
	}
	for _, r := range current {
		if seen[key{r.MapID, r.RegionID}] {
			continue
		}
		if _, err := tx.Exec(`UPDATE vacuum_regions SET removed=1, synced_at=CURRENT_TIMESTAMP
			WHERE blid=? AND pmap_id=? AND region_id=?`, blid, r.MapID, r.RegionID); err != nil {
			return c, err
		}
		c.stale++
	}
	return c, tx.Commit()
}

// nameRegion names every region with this id (normally exactly one). With
// localOnly, regions the cloud provided are left alone (the cloud owns their names).
func (s *Storage) nameRegion(blid, regionID, name string, localOnly bool) (int64, error) {
	q := `UPDATE vacuum_regions SET name=? WHERE blid=? AND region_id=?`
	if localOnly {
		q += ` AND source<>'cloud'`
	}
	res, err := s.db.Exec(q, name, blid, regionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
