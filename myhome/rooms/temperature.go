package rooms

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

// Service provides room management via RPC
type Service struct {
	log                logr.Logger
	mqttClient         mqtt.Client
	storage            *Storage
	externalDayTypeAPI func(ctx context.Context, roomID string, date time.Time) (myhome.DayType, error) // PLACEHOLDER
}

// RoomConfig defines temperature settings for a room
type RoomConfig struct {
	ID      string
	Name    string
	Kinds   []myhome.RoomKind
	Levels  map[string]float64 // "eco", "comfort", "away"
	ICalURL string             // public iCal URL for room agenda
}

// KindSchedule stores comfort time ranges for a room kind and day type
type KindSchedule struct {
	Kind    myhome.RoomKind
	DayType myhome.DayType
	Ranges  []TimeRange
}

// DayTypeCalendar stores the day-type for each date for a room
type DayTypeCalendar map[string]myhome.DayType

// WeekdayDefaults stores day types for weekdays (0=Sunday, 6=Saturday)
type WeekdayDefaults map[int]myhome.DayType

// TimeRange represents a time period with start and end times
type TimeRange struct {
	Start int `json:"start"` // Minutes since midnight (0-1439)
	End   int `json:"end"`   // Minutes since midnight (0-1439)
}

// NewService creates a new rooms service
func NewService(ctx context.Context, log logr.Logger, mqttClient mqtt.Client, storage *Storage) *Service {
	s := &Service{
		log:        log.WithName("rooms.Service"),
		mqttClient: mqttClient,
		storage:    storage,
	}

	s.publishAllRanges(ctx)
	s.log.Info("Rooms service initialized")
	return s
}

// RegisterHandlers registers all RPC method handlers
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

// publishAllRanges publishes temperature ranges for all rooms at startup
func (s *Service) publishAllRanges(ctx context.Context) {
	rooms, err := s.storage.ListRooms()
	if err != nil {
		s.log.Error(err, "Failed to list rooms for startup publish")
		return
	}
	for roomID := range rooms {
		if err := s.PublishRangesUpdate(ctx, roomID); err != nil {
			s.log.Error(err, "Failed to publish ranges at startup", "room_id", roomID)
		}
	}
}

// PublishRangesUpdate publishes temperature ranges for a room to MQTT
// Topic: myhome/rooms/<room-id>/temperature/ranges
func (s *Service) PublishRangesUpdate(ctx context.Context, roomID string) error {
	ranges, dayType, err := s.GetComfortRanges(ctx, roomID, time.Now())
	if err != nil {
		return err
	}

	room, err := s.storage.GetRoom(roomID)
	if err != nil {
		return err
	}

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
	if err := s.mqttClient.Publish(ctx, topic, payloadBytes, mqtt.AtLeastOnce, true, "rooms.service"); err != nil {
		s.log.Error(err, "Failed to publish temperature ranges", "room_id", roomID, "topic", topic)
		return err
	}

	s.log.Info("Published temperature ranges", "room_id", roomID, "topic", topic, "day_type", dayType, "ranges", len(ranges))
	return nil
}

// GetComfortRanges returns the union of comfort time ranges for a room on a given date
func (s *Service) GetComfortRanges(ctx context.Context, roomID string, date time.Time) ([]TimeRange, myhome.DayType, error) {
	room, err := s.storage.GetRoom(roomID)
	if err != nil {
		return nil, "", fmt.Errorf("room not found: %s", roomID)
	}

	dayType := s.getDayType(ctx, roomID, date)

	schedules, err := s.storage.GetKindSchedules(nil, &dayType)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get kind schedules: %w", err)
	}

	// Build map of kind -> ranges for this dayType
	kindRanges := make(map[myhome.RoomKind][]TimeRange, len(schedules))
	for _, sched := range schedules {
		ranges := make([]TimeRange, len(sched.Ranges))
		for i, r := range sched.Ranges {
			ranges[i] = TimeRange{Start: r.Start, End: r.End}
		}
		kindRanges[sched.Kind] = ranges
	}

	// Union of ranges from all room kinds (deduplicated by start-end key)
	rangeMap := make(map[string]TimeRange)
	for _, kind := range room.Kinds {
		for _, tr := range kindRanges[kind] {
			key := fmt.Sprintf("%d-%d", tr.Start, tr.End)
			rangeMap[key] = tr
		}
	}

	comfortRanges := make([]TimeRange, 0, len(rangeMap))
	for _, tr := range rangeMap {
		comfortRanges = append(comfortRanges, tr)
	}

	return comfortRanges, dayType, nil
}

// getDayType returns the day type for a given room and date
func (s *Service) getDayType(ctx context.Context, roomID string, date time.Time) myhome.DayType {
	if s.externalDayTypeAPI != nil {
		dayType, err := s.externalDayTypeAPI(ctx, roomID, date)
		if err == nil {
			return dayType
		}
		s.log.Error(err, "External day-type API failed, using defaults", "room_id", roomID)
	}

	weekday := int(date.Weekday())
	dayType, err := s.storage.GetWeekdayDefault(weekday)
	if err == nil {
		return dayType
	}

	if weekday == 0 || weekday == 6 {
		return myhome.DayTypeDayOff
	}
	return myhome.DayTypeWorkDay
}

// isInTimeRange checks if currentMinutes falls within the given time range
func isInTimeRange(currentMinutes int, tr TimeRange) bool {
	if tr.End < tr.Start {
		// Range crosses midnight (e.g., 22:00–06:00)
		return currentMinutes >= tr.Start || currentMinutes < tr.End
	}
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
