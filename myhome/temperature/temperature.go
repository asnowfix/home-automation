package temperature

import (
	"context"
	"fmt"
	"myhome"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

// Service provides temperature management via RPC
type Service struct {
	ctx                context.Context
	log                logr.Logger
	rooms              map[string]*RoomConfig                                                           // room-id -> config
	weekdayDefaults    map[string]map[int]myhome.DayType                                                // room-id -> weekday -> day-type
	kindSchedules      map[myhome.RoomKind]map[myhome.DayType][]TimeRange                               // kind -> day-type -> ranges
	externalDayTypeAPI func(ctx context.Context, roomID string, date time.Time) (myhome.DayType, error) // PLACEHOLDER: external API for day-type
}

// RoomConfig defines temperature settings for a room
type RoomConfig struct {
	ID          string
	Name        string
	Kinds       []myhome.RoomKind
	ComfortTemp float64
	EcoTemp     float64
}

// KindSchedule stores comfort time ranges for a room kind and day type
type KindSchedule struct {
	Kind    myhome.RoomKind
	DayType myhome.DayType
	Ranges  []TimeRange
}

// DayTypeCalendar stores the day-type for each date for a room
type DayTypeCalendar map[string]myhome.DayType // key: YYYY-MM-DD, value: DayType

// WeekdayDefaults stores day types for weekdays (0=Sunday, 6=Saturday)
type WeekdayDefaults map[int]myhome.DayType

// TimeRange represents a time period with start and end times
type TimeRange struct {
	Start string // "HH:MM" format (24-hour)
	End   string // "HH:MM" format (24-hour)
}

// NewService creates a new temperature service (RPC-only)
func NewService(ctx context.Context, log logr.Logger, rooms map[string]*RoomConfig, weekdayDefaults map[string]map[int]myhome.DayType, kindSchedules map[myhome.RoomKind]map[myhome.DayType][]TimeRange) *Service {
	s := &Service{
		ctx:             ctx,
		log:             log.WithName("temperature.Service"),
		rooms:           rooms,
		weekdayDefaults: weekdayDefaults,
		kindSchedules:   kindSchedules,
		// PLACEHOLDER: Set external day-type API function here
		externalDayTypeAPI: nil,
	}
	s.log.Info("Temperature service initialized", "rooms", len(s.rooms))
	return s
}

// GetComfortRanges returns the union of comfort time ranges for a room on a given date
// This is the main method used by RPC handlers to provide schedule data to heaters
func (s *Service) GetComfortRanges(roomID string, date time.Time) ([]TimeRange, myhome.DayType, error) {
	room, exists := s.rooms[roomID]
	if !exists {
		return nil, "", fmt.Errorf("room not found: %s", roomID)
	}

	// Get day type for this date
	dayType := s.getDayType(roomID, date)

	// Collect all comfort ranges from all kinds
	rangeMap := make(map[string]TimeRange) // Use map to deduplicate
	for _, kind := range room.Kinds {
		kindScheds, exists := s.kindSchedules[kind]
		if !exists {
			continue
		}

		ranges, exists := kindScheds[dayType]
		if !exists {
			continue
		}

		// Add ranges to map (using Start-End as key for deduplication)
		for _, tr := range ranges {
			key := tr.Start + "-" + tr.End
			rangeMap[key] = tr
		}
	}

	// Convert map to slice
	var comfortRanges []TimeRange
	for _, tr := range rangeMap {
		comfortRanges = append(comfortRanges, tr)
	}

	return comfortRanges, dayType, nil
}

// getDayType returns the day type for a given room and date
// Priority: 1) External API (if configured), 2) Weekday defaults, 3) Built-in defaults (Sat/Sun = day-off)
func (s *Service) getDayType(roomID string, date time.Time) myhome.DayType {
	// PLACEHOLDER: Try external API first if configured
	if s.externalDayTypeAPI != nil {
		dayType, err := s.externalDayTypeAPI(s.ctx, roomID, date)
		if err == nil {
			return dayType
		}
		// If external API fails, fall through to defaults
		s.log.Error(err, "External day-type API failed, using defaults", "room_id", roomID, "date", date.Format("2006-01-02"))
	}

	// Check weekday defaults for this room
	weekday := int(date.Weekday()) // 0=Sunday, 1=Monday, ..., 6=Saturday
	if defaults, exists := s.weekdayDefaults[roomID]; exists {
		if dayType, exists := defaults[weekday]; exists {
			return dayType
		}
	}

	// Built-in default: Saturday & Sunday = day-off, others = work-day
	if weekday == 0 || weekday == 6 { // Sunday or Saturday
		return myhome.DayTypeDayOff
	}
	return myhome.DayTypeWorkDay
}

// isComfortTime checks if the given time falls within comfort hours for any of the room kinds and day type
// Returns true if the time is comfort for ANY of the room's kinds (union of all comfort ranges)
func (s *Service) isComfortTime(kinds []myhome.RoomKind, dayType myhome.DayType, t time.Time) bool {
	hour := t.Hour()
	minute := t.Minute()
	currentMinutes := hour*60 + minute

	// Check each kind - if comfort in ANY kind, it's comfort for the room
	for _, kind := range kinds {
		// Get schedules for this kind
		kindScheds, exists := s.kindSchedules[kind]
		if !exists {
			continue
		}

		// Get comfort ranges for this day type
		comfortRanges, exists := kindScheds[dayType]
		if !exists {
			continue
		}

		// Check if current time is within any comfort range for this kind
		for _, tr := range comfortRanges {
			if isInTimeRange(currentMinutes, tr) {
				return true // Comfort in at least one kind
			}
		}
	}

	// Default to eco mode if not in any comfort range for any kind
	return false
}

// isInTimeRange checks if currentMinutes falls within the given time range
func isInTimeRange(currentMinutes int, tr TimeRange) bool {
	startMinutes, err := parseTime(tr.Start)
	if err != nil {
		return false
	}

	endMinutes, err := parseTime(tr.End)
	if err != nil {
		return false
	}

	// Handle ranges that cross midnight
	if endMinutes < startMinutes {
		// Range crosses midnight (e.g., 23:00-06:00)
		return currentMinutes >= startMinutes || currentMinutes < endMinutes
	}

	// Normal range (e.g., 06:00-23:00)
	return currentMinutes >= startMinutes && currentMinutes < endMinutes
}

// parseTime converts "HH:MM" to minutes since midnight
func parseTime(timeStr string) (int, error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time format: %s", timeStr)
	}

	var hour, minute int
	if _, err := fmt.Sscanf(timeStr, "%d:%d", &hour, &minute); err != nil {
		return 0, err
	}

	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid time values: %s", timeStr)
	}

	return hour*60 + minute, nil
}
