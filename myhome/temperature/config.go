package temperature

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config represents the temperature service configuration
type Config struct {
	Port  int                 `yaml:"port"`
	Rooms map[string]RoomYAML `yaml:"rooms"`
}

// RoomYAML represents a room configuration in YAML format
type RoomYAML struct {
	Name        string       `yaml:"name"`
	ComfortTemp float64      `yaml:"comfort_temp"`
	EcoTemp     float64      `yaml:"eco_temp"`
	Schedule    ScheduleYAML `yaml:"schedule"`
}

// ScheduleYAML represents schedule configuration in YAML format
// Only comfort hours are defined - eco is the default
type ScheduleYAML struct {
	Weekday []string `yaml:"weekday"` // Comfort hours, e.g., ["06:00-23:00"]
	Weekend []string `yaml:"weekend"` // Comfort hours, e.g., ["08:00-23:00"]
}

// LoadConfigFromViper loads temperature service configuration from Viper
// Expects configuration under the "temperatures" key
func LoadConfigFromViper(v *viper.Viper) (*Config, error) {
	var cfg Config

	// Get port from temperatures.port or default to 8890
	cfg.Port = v.GetInt("temperatures.port")
	if cfg.Port == 0 {
		cfg.Port = 8890
	}

	// Get rooms map from temperatures.rooms
	roomsMap := v.GetStringMap("temperatures.rooms")
	if len(roomsMap) == 0 {
		return nil, fmt.Errorf("no rooms configured in temperatures.rooms")
	}

	cfg.Rooms = make(map[string]RoomYAML)

	// Parse each room
	for roomID := range roomsMap {
		var room RoomYAML

		// Use sub-viper for this room
		roomKey := fmt.Sprintf("temperatures.rooms.%s", roomID)

		room.Name = v.GetString(roomKey + ".name")
		room.ComfortTemp = v.GetFloat64(roomKey + ".comfort_temp")
		room.EcoTemp = v.GetFloat64(roomKey + ".eco_temp")

		// Get schedule
		room.Schedule.Weekday = v.GetStringSlice(roomKey + ".schedule.weekday")
		room.Schedule.Weekend = v.GetStringSlice(roomKey + ".schedule.weekend")

		cfg.Rooms[roomID] = room
	}

	return &cfg, nil
}

// ToRoomConfigs converts YAML config to internal RoomConfig map
func (c *Config) ToRoomConfigs() (map[string]*RoomConfig, error) {
	rooms := make(map[string]*RoomConfig)

	for id, roomYAML := range c.Rooms {
		// Parse weekday comfort hours
		weekdayComfort, err := parseTimeRangeStrings(roomYAML.Schedule.Weekday)
		if err != nil {
			return nil, fmt.Errorf("invalid weekday schedule for room %s: %w", id, err)
		}

		// Parse weekend comfort hours
		weekendComfort, err := parseTimeRangeStrings(roomYAML.Schedule.Weekend)
		if err != nil {
			return nil, fmt.Errorf("invalid weekend schedule for room %s: %w", id, err)
		}

		rooms[id] = &RoomConfig{
			ID:          id,
			Name:        roomYAML.Name,
			ComfortTemp: roomYAML.ComfortTemp,
			EcoTemp:     roomYAML.EcoTemp,
			Schedule: &Schedule{
				Weekday: weekdayComfort,
				Weekend: weekendComfort,
			},
		}
	}

	return rooms, nil
}

// parseTimeRangeStrings converts strings like "06:00-23:00" to TimeRange structs
func parseTimeRangeStrings(ranges []string) ([]TimeRange, error) {
	var result []TimeRange

	for _, rangeStr := range ranges {
		parts := splitTimeRange(rangeStr)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid time range format: %s (expected HH:MM-HH:MM)", rangeStr)
		}

		result = append(result, TimeRange{
			Start: parts[0],
			End:   parts[1],
		})
	}

	return result, nil
}

// splitTimeRange splits "06:00-23:00" into ["06:00", "23:00"]
func splitTimeRange(s string) []string {
	// Find the dash separator
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			// Make sure it's not at the start (negative time)
			if i > 0 {
				return []string{s[:i], s[i+1:]}
			}
		}
	}
	return []string{s}
}
