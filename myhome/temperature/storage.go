package temperature

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Storage handles persistent storage of temperature configurations
type Storage struct {
	db  *sqlx.DB
	log logr.Logger
}

// RoomConfig represents a room's temperature configuration in the database
type RoomConfigDB struct {
	RoomID      string    `db:"room_id"`
	Name        string    `db:"name"`
	ComfortTemp float64   `db:"comfort_temp"`
	EcoTemp     float64   `db:"eco_temp"`
	WeekdayJSON string    `db:"weekday_schedule"`
	WeekendJSON string    `db:"weekend_schedule"`
	UpdatedAt   time.Time `db:"updated_at"`
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
		comfort_temp REAL NOT NULL,
		eco_temp REAL NOT NULL,
		weekday_schedule TEXT NOT NULL,  -- JSON array of time ranges
		weekend_schedule TEXT NOT NULL,  -- JSON array of time ranges
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
	// Marshal schedule arrays to JSON
	weekdayJSON, err := json.Marshal(config.Schedule.Weekday)
	if err != nil {
		return fmt.Errorf("failed to marshal weekday schedule: %w", err)
	}

	weekendJSON, err := json.Marshal(config.Schedule.Weekend)
	if err != nil {
		return fmt.Errorf("failed to marshal weekend schedule: %w", err)
	}

	query := `
	INSERT INTO temperature_rooms (room_id, name, comfort_temp, eco_temp, weekday_schedule, weekend_schedule, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(room_id) DO UPDATE SET
		name = excluded.name,
		comfort_temp = excluded.comfort_temp,
		eco_temp = excluded.eco_temp,
		weekday_schedule = excluded.weekday_schedule,
		weekend_schedule = excluded.weekend_schedule,
		updated_at = excluded.updated_at
	`

	_, err = s.db.Exec(query, roomID, config.Name, config.ComfortTemp, config.EcoTemp,
		string(weekdayJSON), string(weekendJSON), time.Now())
	if err != nil {
		s.log.Error(err, "Failed to set room config", "room_id", roomID)
		return err
	}

	s.log.Info("Room config saved", "room_id", roomID, "name", config.Name)
	return nil
}

// GetRoom retrieves a room configuration
func (s *Storage) GetRoom(roomID string) (*RoomConfig, error) {
	var dbConfig RoomConfigDB

	query := `SELECT room_id, name, comfort_temp, eco_temp, weekday_schedule, weekend_schedule, updated_at
	          FROM temperature_rooms WHERE room_id = ?`

	err := s.db.Get(&dbConfig, query, roomID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("room not found: %s", roomID)
	}
	if err != nil {
		s.log.Error(err, "Failed to get room config", "room_id", roomID)
		return nil, err
	}

	// Unmarshal schedule JSON
	var weekday, weekend []TimeRange
	if err := json.Unmarshal([]byte(dbConfig.WeekdayJSON), &weekday); err != nil {
		return nil, fmt.Errorf("failed to unmarshal weekday schedule: %w", err)
	}
	if err := json.Unmarshal([]byte(dbConfig.WeekendJSON), &weekend); err != nil {
		return nil, fmt.Errorf("failed to unmarshal weekend schedule: %w", err)
	}

	config := &RoomConfig{
		Name:        dbConfig.Name,
		ComfortTemp: dbConfig.ComfortTemp,
		EcoTemp:     dbConfig.EcoTemp,
		Schedule: &Schedule{
			Weekday: weekday,
			Weekend: weekend,
		},
	}

	return config, nil
}

// ListRooms returns all room configurations
func (s *Storage) ListRooms() (map[string]*RoomConfig, error) {
	var dbConfigs []RoomConfigDB

	query := `SELECT room_id, name, comfort_temp, eco_temp, weekday_schedule, weekend_schedule, updated_at
	          FROM temperature_rooms ORDER BY room_id`

	err := s.db.Select(&dbConfigs, query)
	if err != nil {
		s.log.Error(err, "Failed to list room configs")
		return nil, err
	}

	rooms := make(map[string]*RoomConfig)
	for _, dbConfig := range dbConfigs {
		var weekday, weekend []TimeRange
		if err := json.Unmarshal([]byte(dbConfig.WeekdayJSON), &weekday); err != nil {
			s.log.Error(err, "Failed to unmarshal weekday schedule", "room_id", dbConfig.RoomID)
			continue
		}
		if err := json.Unmarshal([]byte(dbConfig.WeekendJSON), &weekend); err != nil {
			s.log.Error(err, "Failed to unmarshal weekend schedule", "room_id", dbConfig.RoomID)
			continue
		}

		rooms[dbConfig.RoomID] = &RoomConfig{
			Name:        dbConfig.Name,
			ComfortTemp: dbConfig.ComfortTemp,
			EcoTemp:     dbConfig.EcoTemp,
			Schedule: &Schedule{
				Weekday: weekday,
				Weekend: weekend,
			},
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
