package vacuum

import (
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// Storage persists paired robots and learned regions in the shared myhome.db,
// following myhome/fetchproxy/storage.go: idempotent CREATE TABLE, no migrations.
//
// vacuum_robots.password is the robot's local password. It stays in this
// database on the daemon host: it is never logged, returned over RPC, or printed.
type Storage struct {
	db  *sqlx.DB
	log logr.Logger
}

type robotRecord struct {
	BLID     string `db:"blid"`
	Name     string `db:"name"`
	Password string `db:"password"`
	Host     string `db:"host"` // last address the robot was reached at; a hint, not identity
}

type regionRecord struct {
	BLID       string `db:"blid"`
	MapID      string `db:"pmap_id"`
	MapVersion string `db:"pmap_version"`
	RegionID   string `db:"region_id"`
	RegionType string `db:"region_type"`
	Name       string `db:"name"`
}

// NewStorage creates the vacuum tables if needed.
func NewStorage(log logr.Logger, db *sqlx.DB) (*Storage, error) {
	s := &Storage{db: db, log: log.WithName("VacuumStorage")}
	schema := `
	CREATE TABLE IF NOT EXISTS vacuum_robots (
		blid       TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		password   TEXT NOT NULL,
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
	return s, nil
}

func (s *Storage) robots() ([]robotRecord, error) {
	var rs []robotRecord
	err := s.db.Select(&rs, `SELECT blid, name, password, host FROM vacuum_robots ORDER BY name, blid`)
	return rs, err
}

func (s *Storage) saveRobot(r robotRecord) error {
	_, err := s.db.NamedExec(`
		INSERT INTO vacuum_robots (blid, name, password, host) VALUES (:blid, :name, :password, :host)
		ON CONFLICT(blid) DO UPDATE SET name=excluded.name, password=excluded.password,
			host=excluded.host, updated_at=CURRENT_TIMESTAMP`, r)
	return err
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
