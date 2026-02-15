package temperature

import (
	"fmt"
	"myhome"

	"github.com/spf13/viper"
)

// Config represents the temperature service configuration
type Config struct {
	Port  int                 `json:"port"`
	Rooms map[string]RoomYAML `json:"rooms"`
}

// RoomYAML represents a room configuration in YAML format
type RoomYAML struct {
	Name   string             `json:"name"`
	Kinds  []string           `json:"kinds"`  // Room kinds, e.g., ["bedroom", "office"]
	Levels map[string]float64 `json:"levels"` // Temperature levels: {"eco": 17.0, "comfort": 21.0, "away": 15.0}
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
		room.Kinds = v.GetStringSlice(roomKey + ".kinds")

		// Load levels as a map
		levelsMap := v.GetStringMap(roomKey + ".levels")
		room.Levels = make(map[string]float64)
		for level, value := range levelsMap {
			if floatVal, ok := value.(float64); ok {
				room.Levels[level] = floatVal
			}
		}

		cfg.Rooms[roomID] = room
	}

	return &cfg, nil
}

// ToRoomConfigs converts YAML config to internal RoomConfig map
func (c *Config) ToRoomConfigs() (map[string]*RoomConfig, error) {
	rooms := make(map[string]*RoomConfig)

	for id, roomYAML := range c.Rooms {
		// Parse kinds
		kinds := make([]myhome.RoomKind, 0, len(roomYAML.Kinds))
		for _, kindStr := range roomYAML.Kinds {
			kinds = append(kinds, myhome.RoomKind(kindStr))
		}

		rooms[id] = &RoomConfig{
			ID:     id,
			Name:   roomYAML.Name,
			Kinds:  kinds,
			Levels: roomYAML.Levels,
		}
	}

	return rooms, nil
}

// parseTimeString converts "HH:MM" to minutes since midnight
func parseTimeString(timeStr string) (int, error) {
	var hours, mins int
	_, err := fmt.Sscanf(timeStr, "%d:%d", &hours, &mins)
	if err != nil {
		return 0, fmt.Errorf("invalid time format: %s (expected HH:MM)", timeStr)
	}
	if hours < 0 || hours > 23 || mins < 0 || mins > 59 {
		return 0, fmt.Errorf("invalid time values: %s", timeStr)
	}
	return hours*60 + mins, nil
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
