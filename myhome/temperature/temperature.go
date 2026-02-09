package temperature

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome"
	"myhome/mqtt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// Service provides temperature management via RPC
type Service struct {
	mu                 sync.RWMutex
	log                logr.Logger
	mqttClient         mqtt.Client                                                                      // MyHome MQTT client for publishing updates
	storage            *Storage                                                                         // Persistent storage
	rooms              map[string]*RoomConfig                                                           // room-id -> config
	weekdayDefaults    map[string]map[int]myhome.DayType                                                // room-id -> weekday -> day-type
	kindSchedules      map[myhome.RoomKind]map[myhome.DayType][]TimeRange                               // kind -> day-type -> ranges
	externalDayTypeAPI func(ctx context.Context, roomID string, date time.Time) (myhome.DayType, error) // PLACEHOLDER: external API for day-type
}

// RoomConfig defines temperature settings for a room
type RoomConfig struct {
	ID     string
	Name   string
	Kinds  []myhome.RoomKind
	Levels map[string]float64 // Temperature levels: "eco" (default), "comfort", "away", etc.
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
	Start int `json:"start"` // Minutes since midnight (0-1439)
	End   int `json:"end"`   // Minutes since midnight (0-1439)
}

// NewService creates a new temperature service (RPC-only)
func NewService(ctx context.Context, log logr.Logger, mqttClient mqtt.Client, storage *Storage) *Service {
	s := &Service{
		log:             log.WithName("temperature.Service"),
		mqttClient:      mqttClient,
		storage:         storage,
		rooms:           make(map[string]*RoomConfig),
		weekdayDefaults: make(map[string]map[int]myhome.DayType),
		kindSchedules:   make(map[myhome.RoomKind]map[myhome.DayType][]TimeRange),
		// PLACEHOLDER: Set external day-type API function here
		externalDayTypeAPI: nil,
	}

	// Load initial data from storage
	if err := s.loadFromStorage(ctx); err != nil {
		s.log.Error(err, "Failed to load initial data from storage")
	}

	s.log.Info("Temperature service initialized", "rooms", len(s.rooms))
	return s
}

// RegisterHandlers registers all temperature RPC method handlers
func (s *Service) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.TemperatureGet, func(ctx context.Context, params any) (any, error) {
		return s.HandleGet(ctx, params.(*myhome.TemperatureGetParams))
	})
	myhome.RegisterMethodHandler(myhome.TemperatureSet, func(ctx context.Context, params any) (any, error) {
		return s.HandleSet(ctx, params.(*myhome.TemperatureSetParams))
	})
	myhome.RegisterMethodHandler(myhome.TemperatureList, func(ctx context.Context, params any) (any, error) {
		return s.HandleList(ctx)
	})
	myhome.RegisterMethodHandler(myhome.TemperatureDelete, func(ctx context.Context, params any) (any, error) {
		return s.HandleDelete(ctx, params.(*myhome.TemperatureDeleteParams))
	})
	myhome.RegisterMethodHandler(myhome.TemperatureGetSchedule, func(ctx context.Context, params any) (any, error) {
		return s.HandleGetSchedule(ctx, params.(*myhome.TemperatureGetScheduleParams))
	})
	myhome.RegisterMethodHandler(myhome.TemperatureGetWeekdayDefaults, func(ctx context.Context, params any) (any, error) {
		return s.HandleGetWeekdayDefaults(ctx, params.(*myhome.TemperatureGetWeekdayDefaultsParams))
	})
	myhome.RegisterMethodHandler(myhome.TemperatureSetWeekdayDefault, func(ctx context.Context, params any) (any, error) {
		return s.HandleSetWeekdayDefault(ctx, params.(*myhome.TemperatureSetWeekdayDefaultParams))
	})
	myhome.RegisterMethodHandler(myhome.TemperatureGetKindSchedules, func(ctx context.Context, params any) (any, error) {
		return s.HandleGetKindSchedules(ctx, params.(*myhome.TemperatureGetKindSchedulesParams))
	})
	myhome.RegisterMethodHandler(myhome.TemperatureSetKindSchedule, func(ctx context.Context, params any) (any, error) {
		return s.HandleSetKindSchedule(ctx, params.(*myhome.TemperatureSetKindScheduleParams))
	})
	myhome.RegisterMethodHandler(myhome.RoomList, func(ctx context.Context, params any) (any, error) {
		return s.HandleRoomList(ctx)
	})
	myhome.RegisterMethodHandler(myhome.RoomCreate, func(ctx context.Context, params any) (any, error) {
		return s.HandleRoomCreate(ctx, params.(*myhome.RoomCreateParams))
	})
	myhome.RegisterMethodHandler(myhome.RoomEdit, func(ctx context.Context, params any) (any, error) {
		return s.HandleRoomEdit(ctx, params.(*myhome.RoomEditParams))
	})
	myhome.RegisterMethodHandler(myhome.RoomDelete, func(ctx context.Context, params any) (any, error) {
		return s.HandleRoomDelete(ctx, params.(*myhome.RoomDeleteParams))
	})
}

// loadFromStorage loads all data from persistent storage into memory
func (s *Service) loadFromStorage(ctx context.Context) error {
	// Load rooms
	rooms, err := s.storage.ListRooms()
	if err != nil {
		return fmt.Errorf("failed to load rooms: %w", err)
	}
	s.rooms = rooms

	// Load kind schedules
	schedules, err := s.storage.GetKindSchedules(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to load kind schedules: %w", err)
	}

	// Convert to internal format
	s.kindSchedules = make(map[myhome.RoomKind]map[myhome.DayType][]TimeRange)
	for _, sched := range schedules {
		if _, exists := s.kindSchedules[sched.Kind]; !exists {
			s.kindSchedules[sched.Kind] = make(map[myhome.DayType][]TimeRange)
		}

		// Convert TemperatureTimeRange to TimeRange
		ranges := make([]TimeRange, len(sched.Ranges))
		for i, r := range sched.Ranges {
			ranges[i] = TimeRange{Start: r.Start, End: r.End}
		}
		s.kindSchedules[sched.Kind][sched.DayType] = ranges
	}

	// Load global weekday defaults (apply to all rooms)
	globalDefaults, err := s.storage.GetWeekdayDefaults()
	if err != nil {
		s.log.Error(err, "Failed to load global weekday defaults")
	} else {
		// Apply global defaults to all rooms
		s.weekdayDefaults = make(map[string]map[int]myhome.DayType)
		for roomID := range s.rooms {
			s.weekdayDefaults[roomID] = globalDefaults
		}
	}

	// Publish ranges for all rooms at startup
	s.log.Info("Publishing temperature ranges for all rooms at startup")
	for roomID := range s.rooms {
		if err := s.PublishRangesUpdate(ctx, roomID); err != nil {
			s.log.Error(err, "Failed to publish ranges at startup", "room_id", roomID)
		} else {
			s.log.V(1).Info("Published ranges at startup", "room_id", roomID)
		}
	}

	return nil
}

// PublishRangesUpdate publishes temperature ranges for a room to MQTT
// Topic: myhome/rooms/<room-id>/temperature/ranges
func (s *Service) PublishRangesUpdate(ctx context.Context, roomID string) error {
	// Get today's ranges
	ranges, dayType, err := s.GetComfortRanges(ctx, roomID, time.Now())
	if err != nil {
		return err
	}

	room, exists := s.rooms[roomID]
	if !exists {
		return fmt.Errorf("room not found: %s", roomID)
	}

	// Prepare MQTT payload
	payload := struct {
		RoomID  string             `json:"room_id"`
		Date    string             `json:"date"`
		DayType string             `json:"day_type"`
		Levels  map[string]float64 `json:"levels"`
		Ranges  []TimeRange        `json:"ranges"`
	}{
		RoomID:  roomID,
		Date:    time.Now().Format("2006-01-02"),
		DayType: string(dayType),
		Levels:  room.Levels,
		Ranges:  ranges,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal ranges: %w", err)
	}

	topic := fmt.Sprintf("myhome/rooms/%s/temperature/ranges", roomID)
	err = s.mqttClient.Publish(ctx, topic, payloadBytes, mqtt.AtLeastOnce, true /*retain*/, "temperature.service")
	if err != nil {
		s.log.Error(err, "Failed to publish temperature ranges", "room_id", roomID, "topic", topic)
		return err
	}

	s.log.Info("Published temperature ranges", "room_id", roomID, "topic", topic, "day_type", dayType, "range_count", len(ranges))
	return nil
}

// GetComfortRanges returns the union of comfort time ranges for a room on a given date
// This is the main method used by RPC handlers to provide schedule data to heaters
func (s *Service) GetComfortRanges(ctx context.Context, roomID string, date time.Time) ([]TimeRange, myhome.DayType, error) {
	room, exists := s.rooms[roomID]
	if !exists {
		return nil, "", fmt.Errorf("room not found: %s", roomID)
	}

	// Get day type for this date
	dayType := s.getDayType(ctx, roomID, date)

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
			key := fmt.Sprintf("%d-%d", tr.Start, tr.End)
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
func (s *Service) getDayType(ctx context.Context, roomID string, date time.Time) myhome.DayType {
	// PLACEHOLDER: Try external API first if configured
	if s.externalDayTypeAPI != nil {
		dayType, err := s.externalDayTypeAPI(ctx, roomID, date)
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
	// Handle ranges that cross midnight
	if tr.End < tr.Start {
		// Range crosses midnight (e.g., 1380-360 = 23:00-06:00)
		return currentMinutes >= tr.Start || currentMinutes < tr.End
	}

	// Normal range (e.g., 360-1380 = 06:00-23:00)
	return currentMinutes >= tr.Start && currentMinutes < tr.End
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
