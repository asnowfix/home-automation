package rooms

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/asnowfix/home-automation/internal/myhome"
	"time"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// Type aliases for convenience
type DayType = myhome.DayType
type RoomKind = myhome.RoomKind

// Storage handles persistent storage of room configurations
type Storage struct {
	db  *sqlx.DB
	log logr.Logger
}

// RoomConfigDB represents a room's configuration in the database
type RoomConfigDB struct {
	RoomID     string    `db:"room_id"`
	Name       string    `db:"name"`
	KindsJSON  string    `db:"kinds"`
	LevelsJSON string    `db:"levels"`
	ICalURL    string    `db:"ical_url"`
	UpdatedAt  time.Time `db:"updated_at"`
}

// KindScheduleDB represents a kind schedule in the database
type KindScheduleDB struct {
	Kind       string    `db:"kind"`
	DayType    string    `db:"day_type"`
	RangesJSON string    `db:"ranges"`
	UpdatedAt  time.Time `db:"updated_at"`
}

// WeekdayDefaultDB represents a global weekday default in the database
type WeekdayDefaultDB struct {
	Weekday   int       `db:"weekday"`
	DayType   string    `db:"day_type"`
	UpdatedAt time.Time `db:"updated_at"`
}

// NewStorage creates a new rooms storage instance
func NewStorage(log logr.Logger, db *sqlx.DB) (*Storage, error) {
	s := &Storage{
		db:  db,
		log: log.WithName("rooms.Storage"),
	}

	if err := s.migrate(); err != nil {
		return nil, err
	}

	if err := s.createTables(); err != nil {
		return nil, err
	}

	return s, nil
}

// migrate renames legacy temperature_* tables and adds any missing columns
func (s *Storage) migrate() error {
	renames := []struct{ old, new string }{
		{"temperature_rooms", "rooms"},
		{"temperature_kind_schedules", "room_kind_schedules"},
		{"temperature_weekday_defaults", "room_weekday_defaults"},
	}

	for _, r := range renames {
		var count int
		s.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", r.old,
		).Scan(&count)
		if count > 0 {
			if _, err := s.db.Exec("ALTER TABLE " + r.old + " RENAME TO " + r.new); err != nil {
				return fmt.Errorf("renaming %s to %s: %w", r.old, r.new, err)
			}
			s.log.Info("Migrated table", "from", r.old, "to", r.new)
		}
	}

	// Add ical_url column if the rooms table already exists but lacks the column
	var colCount int
	s.db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('rooms') WHERE name='ical_url'",
	).Scan(&colCount)
	if colCount == 0 {
		// Ignore error: table may not exist yet; createTables will include the column
		s.db.Exec("ALTER TABLE rooms ADD COLUMN ical_url TEXT NOT NULL DEFAULT ''")
	}

	return nil
}

// createTables creates room configuration tables if they don't exist
func (s *Storage) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS rooms (
		room_id    TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		kinds      TEXT NOT NULL DEFAULT '[]',
		levels     TEXT NOT NULL DEFAULT '{}',
		ical_url   TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS room_kind_schedules (
		kind       TEXT NOT NULL,
		day_type   TEXT NOT NULL,
		ranges     TEXT NOT NULL DEFAULT '[]',
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (kind, day_type)
	);

	CREATE TABLE IF NOT EXISTS room_weekday_defaults (
		weekday    INTEGER PRIMARY KEY,
		day_type   TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS weather_cache (
		fetched_at INTEGER NOT NULL,
		forecast   TEXT    NOT NULL DEFAULT '[]',
		stale      INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS room_agenda_cache (
		room_id    TEXT    NOT NULL PRIMARY KEY,
		date       TEXT    NOT NULL,
		slots      TEXT    NOT NULL DEFAULT '[]',
		fetched_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_rooms_updated ON rooms(updated_at);
	`

	if _, err := s.db.Exec(schema); err != nil {
		s.log.Error(err, "Failed to create rooms tables")
		return err
	}

	return nil
}

// SaveRoom creates or updates a room configuration
// Returns true if the room was modified (inserted or updated), false if no changes were made
func (s *Storage) SaveRoom(config *RoomConfig) (bool, error) {
	kindsJSON, err := json.Marshal(config.Kinds)
	if err != nil {
		return false, fmt.Errorf("failed to marshal kinds: %w", err)
	}

	levelsJSON, err := json.Marshal(config.Levels)
	if err != nil {
		return false, fmt.Errorf("failed to marshal levels: %w", err)
	}

	query := `
	INSERT INTO rooms (room_id, name, kinds, levels, ical_url, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(room_id) DO UPDATE SET
		name       = excluded.name,
		kinds      = excluded.kinds,
		levels     = excluded.levels,
		ical_url   = excluded.ical_url,
		updated_at = excluded.updated_at
	WHERE rooms.name     IS DISTINCT FROM excluded.name
	   OR rooms.kinds    IS DISTINCT FROM excluded.kinds
	   OR rooms.levels   IS DISTINCT FROM excluded.levels
	   OR rooms.ical_url IS DISTINCT FROM excluded.ical_url
	`

	result, err := s.db.Exec(query, config.ID, config.Name, string(kindsJSON), string(levelsJSON), config.ICalURL, time.Now())
	if err != nil {
		s.log.Error(err, "Failed to save room", "room_id", config.ID)
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	if rows > 0 {
		s.log.Info("Room saved", "room_id", config.ID, "name", config.Name)
		return true, nil
	}

	s.log.V(1).Info("Room unchanged", "room_id", config.ID)
	return false, nil
}

// GetRoom retrieves a room configuration
func (s *Storage) GetRoom(roomID string) (*RoomConfig, error) {
	var row RoomConfigDB

	err := s.db.Get(&row, `SELECT room_id, name, kinds, levels, ical_url, updated_at FROM rooms WHERE room_id = ?`, roomID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("room not found: %s", roomID)
	}
	if err != nil {
		s.log.Error(err, "Failed to get room", "room_id", roomID)
		return nil, err
	}

	return dbToRoomConfig(&row)
}

// ListRooms returns all room configurations
func (s *Storage) ListRooms() (map[string]*RoomConfig, error) {
	var dbRows []RoomConfigDB

	err := s.db.Select(&dbRows, `SELECT room_id, name, kinds, levels, ical_url, updated_at FROM rooms ORDER BY room_id`)
	if err != nil {
		s.log.Error(err, "Failed to list rooms")
		return nil, err
	}

	result := make(map[string]*RoomConfig, len(dbRows))
	for i := range dbRows {
		cfg, err := dbToRoomConfig(&dbRows[i])
		if err != nil {
			s.log.Error(err, "Failed to decode room", "room_id", dbRows[i].RoomID)
			continue
		}
		result[cfg.ID] = cfg
	}

	return result, nil
}

// DeleteRoom removes a room configuration
func (s *Storage) DeleteRoom(roomID string) error {
	result, err := s.db.Exec(`DELETE FROM rooms WHERE room_id = ?`, roomID)
	if err != nil {
		s.log.Error(err, "Failed to delete room", "room_id", roomID)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return fmt.Errorf("room not found: %s", roomID)
	}

	s.log.Info("Room deleted", "room_id", roomID)
	return nil
}

// SetKindSchedule creates or updates a kind schedule
// Returns true if the schedule was modified, false if unchanged
func (s *Storage) SetKindSchedule(kind myhome.RoomKind, dayType myhome.DayType, ranges []myhome.TemperatureTimeRange) (bool, error) {
	rangesJSON, err := json.Marshal(ranges)
	if err != nil {
		return false, fmt.Errorf("failed to marshal ranges: %w", err)
	}

	query := `
	INSERT INTO room_kind_schedules (kind, day_type, ranges, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(kind, day_type) DO UPDATE SET
		ranges     = excluded.ranges,
		updated_at = excluded.updated_at
	WHERE room_kind_schedules.ranges IS DISTINCT FROM excluded.ranges
	`

	result, err := s.db.Exec(query, string(kind), string(dayType), string(rangesJSON), time.Now())
	if err != nil {
		s.log.Error(err, "Failed to set kind schedule", "kind", kind, "day_type", dayType)
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	if rows > 0 {
		s.log.Info("Kind schedule saved", "kind", kind, "day_type", dayType)
		return true, nil
	}

	s.log.V(1).Info("Kind schedule unchanged", "kind", kind, "day_type", dayType)
	return false, nil
}

// GetKindSchedules retrieves kind schedules with optional filters
func (s *Storage) GetKindSchedules(kind *myhome.RoomKind, dayType *myhome.DayType) ([]myhome.TemperatureKindSchedule, error) {
	var dbRows []KindScheduleDB

	query := `SELECT kind, day_type, ranges, updated_at FROM room_kind_schedules WHERE 1=1`
	args := []interface{}{}

	if kind != nil {
		query += ` AND kind = ?`
		args = append(args, string(*kind))
	}
	if dayType != nil {
		query += ` AND day_type = ?`
		args = append(args, string(*dayType))
	}
	query += ` ORDER BY kind, day_type`

	if err := s.db.Select(&dbRows, query, args...); err != nil {
		s.log.Error(err, "Failed to list kind schedules")
		return nil, err
	}

	schedules := make([]myhome.TemperatureKindSchedule, 0, len(dbRows))
	for _, row := range dbRows {
		var ranges []myhome.TemperatureTimeRange
		if err := json.Unmarshal([]byte(row.RangesJSON), &ranges); err != nil {
			s.log.Error(err, "Failed to unmarshal ranges", "kind", row.Kind, "day_type", row.DayType)
			continue
		}
		schedules = append(schedules, myhome.TemperatureKindSchedule{
			Kind:    myhome.RoomKind(row.Kind),
			DayType: myhome.DayType(row.DayType),
			Ranges:  ranges,
		})
	}

	return schedules, nil
}

// SetWeekdayDefault sets the global day type for a specific weekday
// Returns true if modified, false if unchanged
func (s *Storage) SetWeekdayDefault(weekday int, dayType myhome.DayType) (bool, error) {
	query := `
	INSERT INTO room_weekday_defaults (weekday, day_type, updated_at)
	VALUES (?, ?, ?)
	ON CONFLICT(weekday) DO UPDATE SET
		day_type   = excluded.day_type,
		updated_at = excluded.updated_at
	WHERE room_weekday_defaults.day_type IS DISTINCT FROM excluded.day_type
	`

	result, err := s.db.Exec(query, weekday, string(dayType), time.Now())
	if err != nil {
		s.log.Error(err, "Failed to set weekday default", "weekday", weekday)
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	if rows > 0 {
		s.log.Info("Weekday default saved", "weekday", weekday, "day_type", dayType)
		return true, nil
	}

	s.log.V(1).Info("Weekday default unchanged", "weekday", weekday)
	return false, nil
}

// GetWeekdayDefault retrieves the global day type for a specific weekday
func (s *Storage) GetWeekdayDefault(weekday int) (myhome.DayType, error) {
	var row WeekdayDefaultDB

	err := s.db.Get(&row, `SELECT weekday, day_type, updated_at FROM room_weekday_defaults WHERE weekday = ?`, weekday)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("weekday default not found for weekday %d", weekday)
	}
	if err != nil {
		s.log.Error(err, "Failed to get weekday default", "weekday", weekday)
		return "", err
	}

	return myhome.DayType(row.DayType), nil
}

// GetWeekdayDefaults retrieves all global weekday defaults
// Returns map of weekday (0-6) -> day-type
func (s *Storage) GetWeekdayDefaults() (map[int]myhome.DayType, error) {
	var rows []WeekdayDefaultDB

	if err := s.db.Select(&rows, `SELECT weekday, day_type, updated_at FROM room_weekday_defaults ORDER BY weekday`); err != nil {
		s.log.Error(err, "Failed to get weekday defaults")
		return nil, err
	}

	defaults := make(map[int]myhome.DayType, len(rows))
	for _, row := range rows {
		defaults[row.Weekday] = myhome.DayType(row.DayType)
	}

	return defaults, nil
}

// dbToRoomConfig converts a DB row to a RoomConfig
func dbToRoomConfig(db *RoomConfigDB) (*RoomConfig, error) {
	var kinds []myhome.RoomKind
	if err := json.Unmarshal([]byte(db.KindsJSON), &kinds); err != nil {
		return nil, fmt.Errorf("unmarshal kinds: %w", err)
	}

	var levels map[string]float64
	if err := json.Unmarshal([]byte(db.LevelsJSON), &levels); err != nil {
		return nil, fmt.Errorf("unmarshal levels: %w", err)
	}

	return &RoomConfig{
		ID:      db.RoomID,
		Name:    db.Name,
		Kinds:   kinds,
		Levels:  levels,
		ICalURL: db.ICalURL,
	}, nil
}
