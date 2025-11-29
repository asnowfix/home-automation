package temperature

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"myhome"
	"time"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Type aliases for convenience
type DayType = myhome.DayType
type RoomKind = myhome.RoomKind

// Storage handles persistent storage of temperature configurations
type Storage struct {
	db  *sqlx.DB
	log logr.Logger
}

// RoomConfigDB represents a room's temperature configuration in the database
type RoomConfigDB struct {
	RoomID      string    `db:"room_id"`
	Name        string    `db:"name"`
	KindsJSON   string    `db:"kinds"` // JSON array of room kinds
	ComfortTemp float64   `db:"comfort_temp"`
	EcoTemp     float64   `db:"eco_temp"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// KindScheduleDB represents a kind schedule in the database
type KindScheduleDB struct {
	Kind       string    `db:"kind"`     // bedroom, office, living-room, kitchen, other
	DayType    string    `db:"day_type"` // work-day, day-off
	RangesJSON string    `db:"ranges"`   // JSON array of time ranges
	UpdatedAt  time.Time `db:"updated_at"`
}

// WeekdayDefaultDB represents a weekday default in the database
type WeekdayDefaultDB struct {
	RoomID    string    `db:"room_id"`
	Weekday   int       `db:"weekday"`  // 0=Sunday, 1=Monday, ..., 6=Saturday
	DayType   string    `db:"day_type"` // work-day, day-off
	UpdatedAt time.Time `db:"updated_at"`
}

// NewStorage creates a new temperature storage instance
func NewStorage(log logr.Logger, db *sqlx.DB) (*Storage, error) {
	storage := &Storage{
		db:  db,
		log: log.WithName("TemperatureStorage"),
	}

	if err := storage.createTables(); err != nil {
		return nil, err
	}

	return storage, nil
}

// createTables creates the temperature configuration tables
func (s *Storage) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS temperature_rooms (
		room_id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		kinds TEXT NOT NULL,  -- JSON array of room kinds
		comfort_temp REAL NOT NULL,
		eco_temp REAL NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE TABLE IF NOT EXISTS temperature_kind_schedules (
		kind TEXT NOT NULL,      -- bedroom, office, living-room, kitchen, other
		day_type TEXT NOT NULL,  -- work-day, day-off
		ranges TEXT NOT NULL,    -- JSON array of time ranges
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (kind, day_type)
	);
	
	CREATE TABLE IF NOT EXISTS temperature_weekday_defaults (
		room_id TEXT NOT NULL,
		weekday INTEGER NOT NULL,  -- 0=Sunday, 1=Monday, ..., 6=Saturday
		day_type TEXT NOT NULL,    -- work-day, day-off
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (room_id, weekday),
		FOREIGN KEY (room_id) REFERENCES temperature_rooms(room_id) ON DELETE CASCADE
	);
	
	CREATE INDEX IF NOT EXISTS idx_temperature_rooms_updated 
		ON temperature_rooms(updated_at);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		s.log.Error(err, "Failed to create temperature tables")
		return err
	}

	return nil
}

// SetRoom creates or updates a room configuration
func (s *Storage) SetRoom(roomID string, config *RoomConfig) error {
	// Marshal kinds to JSON
	kindsJSON, err := json.Marshal(config.Kinds)
	if err != nil {
		return fmt.Errorf("failed to marshal kinds: %w", err)
	}

	query := `
	INSERT INTO temperature_rooms (room_id, name, kinds, comfort_temp, eco_temp, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(room_id) DO UPDATE SET
		name = excluded.name,
		kinds = excluded.kinds,
		comfort_temp = excluded.comfort_temp,
		eco_temp = excluded.eco_temp,
		updated_at = excluded.updated_at
	`

	_, err = s.db.Exec(query, roomID, config.Name, string(kindsJSON), config.ComfortTemp, config.EcoTemp, time.Now())
	if err != nil {
		s.log.Error(err, "Failed to set room config", "room_id", roomID)
		return err
	}

	s.log.Info("Room config saved", "room_id", roomID, "name", config.Name, "kinds", config.Kinds)
	return nil
}

// GetRoom retrieves a room configuration
func (s *Storage) GetRoom(roomID string) (*RoomConfig, error) {
	var dbConfig RoomConfigDB

	query := `SELECT room_id, name, kinds, comfort_temp, eco_temp, updated_at
	          FROM temperature_rooms WHERE room_id = ?`

	err := s.db.Get(&dbConfig, query, roomID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("room not found: %s", roomID)
	}
	if err != nil {
		s.log.Error(err, "Failed to get room config", "room_id", roomID)
		return nil, err
	}

	// Unmarshal kinds from JSON
	var kinds []myhome.RoomKind
	if err := json.Unmarshal([]byte(dbConfig.KindsJSON), &kinds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal kinds: %w", err)
	}

	config := &RoomConfig{
		ID:          dbConfig.RoomID,
		Name:        dbConfig.Name,
		Kinds:       kinds,
		ComfortTemp: dbConfig.ComfortTemp,
		EcoTemp:     dbConfig.EcoTemp,
	}

	return config, nil
}

// ListRooms returns all room configurations
func (s *Storage) ListRooms() (map[string]*RoomConfig, error) {
	var dbConfigs []RoomConfigDB

	query := `SELECT room_id, name, kinds, comfort_temp, eco_temp, updated_at
	          FROM temperature_rooms ORDER BY room_id`

	err := s.db.Select(&dbConfigs, query)
	if err != nil {
		s.log.Error(err, "Failed to list room configs")
		return nil, err
	}

	rooms := make(map[string]*RoomConfig)
	for _, dbConfig := range dbConfigs {
		var kinds []myhome.RoomKind
		if err := json.Unmarshal([]byte(dbConfig.KindsJSON), &kinds); err != nil {
			s.log.Error(err, "Failed to unmarshal kinds", "room_id", dbConfig.RoomID)
			continue
		}

		rooms[dbConfig.RoomID] = &RoomConfig{
			ID:          dbConfig.RoomID,
			Name:        dbConfig.Name,
			Kinds:       kinds,
			ComfortTemp: dbConfig.ComfortTemp,
			EcoTemp:     dbConfig.EcoTemp,
		}
	}

	return rooms, nil
}

// DeleteRoom removes a room configuration
func (s *Storage) DeleteRoom(roomID string) error {
	query := `DELETE FROM temperature_rooms WHERE room_id = ?`

	result, err := s.db.Exec(query, roomID)
	if err != nil {
		s.log.Error(err, "Failed to delete room config", "room_id", roomID)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return fmt.Errorf("room not found: %s", roomID)
	}

	s.log.Info("Room config deleted", "room_id", roomID)
	return nil
}

// SetKindSchedule creates or updates a kind schedule
func (s *Storage) SetKindSchedule(sched *KindSchedule) error {
	// Marshal ranges to JSON
	rangesJSON, err := json.Marshal(sched.Ranges)
	if err != nil {
		return fmt.Errorf("failed to marshal ranges: %w", err)
	}

	query := `
	INSERT INTO temperature_kind_schedules (kind, day_type, ranges, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(kind, day_type) DO UPDATE SET
		ranges = excluded.ranges,
		updated_at = excluded.updated_at
	`

	_, err = s.db.Exec(query, string(sched.Kind), string(sched.DayType), string(rangesJSON), time.Now())
	if err != nil {
		s.log.Error(err, "Failed to set kind schedule", "kind", sched.Kind, "day_type", sched.DayType)
		return err
	}

	s.log.Info("Kind schedule saved", "kind", sched.Kind, "day_type", sched.DayType)
	return nil
}

// GetKindSchedules retrieves all kind schedules
func (s *Storage) GetKindSchedules() ([]KindSchedule, error) {
	var dbScheds []KindScheduleDB

	query := `SELECT kind, day_type, ranges, updated_at FROM temperature_kind_schedules ORDER BY kind, day_type`

	err := s.db.Select(&dbScheds, query)
	if err != nil {
		s.log.Error(err, "Failed to list kind schedules")
		return nil, err
	}

	var schedules []KindSchedule
	for _, dbSched := range dbScheds {
		var ranges []TimeRange
		if err := json.Unmarshal([]byte(dbSched.RangesJSON), &ranges); err != nil {
			s.log.Error(err, "Failed to unmarshal ranges", "kind", dbSched.Kind, "day_type", dbSched.DayType)
			continue
		}

		schedules = append(schedules, KindSchedule{
			Kind:    myhome.RoomKind(dbSched.Kind),
			DayType: myhome.DayType(dbSched.DayType),
			Ranges:  ranges,
		})
	}

	return schedules, nil
}

// SetWeekdayDefault sets the day type for a specific weekday
func (s *Storage) SetWeekdayDefault(roomID string, weekday int, dayType myhome.DayType) error {
	query := `
	INSERT INTO temperature_weekday_defaults (room_id, weekday, day_type, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(room_id, weekday) DO UPDATE SET
		day_type = excluded.day_type,
		updated_at = excluded.updated_at
	`

	_, err := s.db.Exec(query, roomID, weekday, string(dayType), time.Now())
	if err != nil {
		s.log.Error(err, "Failed to set weekday default", "room_id", roomID, "weekday", weekday)
		return err
	}

	s.log.Info("Weekday default saved", "room_id", roomID, "weekday", weekday, "day_type", dayType)
	return nil
}

// GetWeekdayDefaults retrieves all weekday defaults for a specific room
// Returns map of weekday (0-6) -> day-type
func (s *Storage) GetWeekdayDefaults(roomID string) (map[int]myhome.DayType, error) {
	var dbEntries []WeekdayDefaultDB

	query := `SELECT room_id, weekday, day_type, updated_at FROM temperature_weekday_defaults WHERE room_id = ? ORDER BY weekday`

	err := s.db.Select(&dbEntries, query, roomID)
	if err != nil {
		s.log.Error(err, "Failed to get weekday defaults", "room_id", roomID)
		return nil, err
	}

	defaults := make(map[int]myhome.DayType)
	for _, entry := range dbEntries {
		defaults[entry.Weekday] = myhome.DayType(entry.DayType)
	}

	return defaults, nil
}
