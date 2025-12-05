package temperature

import (
	"context"
	"fmt"
	"myhome"
	"time"
)

// RPC method handlers

// HandleGet handles temperature.get RPC method
func (s *Service) HandleGet(ctx context.Context, params *myhome.TemperatureGetParams) (*myhome.TemperatureRoomConfig, error) {
	s.mu.RLock()
	config, exists := s.rooms[params.RoomID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("room not found: %s", params.RoomID)
	}

	return &myhome.TemperatureRoomConfig{
		RoomID: config.ID,
		Name:   config.Name,
		Kinds:  config.Kinds,
		Levels: config.Levels,
	}, nil
}

// HandleSet handles temperature.set RPC method
func (s *Service) HandleSet(ctx context.Context, p *myhome.TemperatureSetParams) (*myhome.TemperatureSetResult, error) {
	// Validate parameters
	if p.RoomID == "" {
		return nil, fmt.Errorf("room_id is required")
	}
	if p.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(p.Kinds) == 0 {
		return nil, fmt.Errorf("at least one kind is required")
	}
	if len(p.Levels) == 0 {
		return nil, fmt.Errorf("at least one temperature level is required")
	}
	// Ensure "eco" level exists (it's the default)
	if _, hasEco := p.Levels["eco"]; !hasEco {
		return nil, fmt.Errorf("'eco' temperature level is required (it's the default)")
	}

	// Create or update room config
	config := &RoomConfig{
		ID:     p.RoomID,
		Name:   p.Name,
		Kinds:  p.Kinds,
		Levels: p.Levels,
	}

	s.mu.Lock()
	s.rooms[p.RoomID] = config
	s.mu.Unlock()

	// Save to storage
	if err := s.storage.SaveRoom(config); err != nil {
		return nil, fmt.Errorf("failed to save room: %w", err)
	}

	s.log.Info("Room configuration saved", "room_id", p.RoomID, "name", p.Name, "kinds", p.Kinds)

	return &myhome.TemperatureSetResult{
		Status: "ok",
		RoomID: p.RoomID,
	}, nil
}

// HandleList handles temperature.list RPC method
func (s *Service) HandleList(ctx context.Context) (*myhome.TemperatureRoomList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(myhome.TemperatureRoomList)

	for roomID, config := range s.rooms {
		result[roomID] = &myhome.TemperatureRoomConfig{
			RoomID: config.ID,
			Name:   config.Name,
			Kinds:  config.Kinds,
			Levels: config.Levels,
		}
	}

	return &result, nil
}

// HandleDelete handles temperature.delete RPC method
func (s *Service) HandleDelete(ctx context.Context, params *myhome.TemperatureDeleteParams) (*myhome.TemperatureDeleteResult, error) {
	s.mu.Lock()
	_, exists := s.rooms[params.RoomID]
	if exists {
		delete(s.rooms, params.RoomID)
	}
	s.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("room not found: %s", params.RoomID)
	}

	// Delete from storage
	if err := s.storage.DeleteRoom(params.RoomID); err != nil {
		return nil, fmt.Errorf("failed to delete room: %w", err)
	}

	s.log.Info("Room configuration deleted", "room_id", params.RoomID)

	return &myhome.TemperatureDeleteResult{
		Status: "ok",
		RoomID: params.RoomID,
	}, nil
}

// HandleGetSchedule handles temperature.getschedule RPC method
func (s *Service) HandleGetSchedule(ctx context.Context, params *myhome.TemperatureGetScheduleParams) (*myhome.TemperatureScheduleResult, error) {
	// Get room config
	s.mu.RLock()
	config, exists := s.rooms[params.RoomID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("room not found: %s", params.RoomID)
	}

	// Parse date or use today
	var date time.Time
	var err error
	if params.Date != nil && *params.Date != "" {
		date, err = time.Parse("2006-01-02", *params.Date)
		if err != nil {
			return nil, fmt.Errorf("invalid date format: %s (expected YYYY-MM-DD)", *params.Date)
		}
	} else {
		date = time.Now()
	}

	// Get weekday (0=Sunday, 1=Monday, ..., 6=Saturday)
	weekday := int(date.Weekday())

	// Get global day type for this weekday
	dayType, err := s.storage.GetWeekdayDefault(weekday)
	if err != nil {
		// Use default if not set
		if weekday == 0 || weekday == 6 {
			dayType = myhome.DayTypeDayOff
		} else {
			dayType = myhome.DayTypeWorkDay
		}
	}

	// Get comfort ranges for this room's kinds and day type
	ranges, _, err := s.GetComfortRanges(ctx, params.RoomID, date)
	if err != nil {
		return nil, fmt.Errorf("failed to get comfort ranges: %w", err)
	}

	// Convert TimeRange to TemperatureTimeRange
	comfortRanges := make([]myhome.TemperatureTimeRange, len(ranges))
	for i, r := range ranges {
		comfortRanges[i] = myhome.TemperatureTimeRange{
			Start: r.Start,
			End:   r.End,
		}
	}

	return &myhome.TemperatureScheduleResult{
		RoomID:        params.RoomID,
		Date:          date.Format("2006-01-02"),
		Weekday:       weekday,
		DayType:       dayType,
		Levels:        config.Levels,
		ComfortRanges: comfortRanges,
	}, nil
}

// HandleGetWeekdayDefaults handles temperature.getweekdaydefaults RPC method
// Returns global weekday defaults that apply to all rooms
func (s *Service) HandleGetWeekdayDefaults(ctx context.Context, params *myhome.TemperatureGetWeekdayDefaultsParams) (*myhome.TemperatureWeekdayDefaults, error) {
	defaults, err := s.storage.GetWeekdayDefaults()
	if err != nil {
		return nil, fmt.Errorf("failed to get weekday defaults: %w", err)
	}

	return &myhome.TemperatureWeekdayDefaults{
		Defaults: defaults,
	}, nil
}

// HandleSetWeekdayDefault handles temperature.setweekdaydefault RPC method
// Sets global weekday default that applies to all rooms
func (s *Service) HandleSetWeekdayDefault(ctx context.Context, params *myhome.TemperatureSetWeekdayDefaultParams) (*myhome.TemperatureSetWeekdayDefaultResult, error) {
	// Validate weekday
	if params.Weekday < 0 || params.Weekday > 6 {
		return nil, fmt.Errorf("invalid weekday: %d (must be 0-6)", params.Weekday)
	}

	// Validate day type
	if params.DayType != myhome.DayTypeWorkDay && params.DayType != myhome.DayTypeDayOff {
		return nil, fmt.Errorf("invalid day_type: %s (must be 'work-day' or 'day-off')", params.DayType)
	}

	// Save to storage (global)
	if err := s.storage.SetWeekdayDefault(params.Weekday, params.DayType); err != nil {
		return nil, fmt.Errorf("failed to set weekday default: %w", err)
	}

	// Update in-memory cache for all rooms
	s.mu.Lock()
	for roomID := range s.rooms {
		if s.weekdayDefaults[roomID] == nil {
			s.weekdayDefaults[roomID] = make(map[int]myhome.DayType)
		}
		s.weekdayDefaults[roomID][params.Weekday] = params.DayType
	}
	s.mu.Unlock()

	s.log.Info("Global weekday default set", "weekday", params.Weekday, "day_type", params.DayType)

	return &myhome.TemperatureSetWeekdayDefaultResult{
		Weekday: params.Weekday,
		DayType: params.DayType,
	}, nil
}

// HandleGetKindSchedules handles temperature.getkindschedules RPC method
func (s *Service) HandleGetKindSchedules(ctx context.Context, params *myhome.TemperatureGetKindSchedulesParams) (*myhome.TemperatureKindScheduleList, error) {
	schedules, err := s.storage.GetKindSchedules(params.Kind, params.DayType)
	if err != nil {
		return nil, fmt.Errorf("failed to get kind schedules: %w", err)
	}

	result := make(myhome.TemperatureKindScheduleList, len(schedules))
	copy(result, schedules)

	return &result, nil
}

// HandleSetKindSchedule handles temperature.setkindschedule RPC method
func (s *Service) HandleSetKindSchedule(ctx context.Context, params *myhome.TemperatureSetKindScheduleParams) (*myhome.TemperatureSetKindScheduleResult, error) {
	// Validate kind
	validKinds := []myhome.RoomKind{
		myhome.RoomKindBedroom,
		myhome.RoomKindOffice,
		myhome.RoomKindLivingRoom,
		myhome.RoomKindKitchen,
		myhome.RoomKindOther,
	}
	valid := false
	for _, k := range validKinds {
		if params.Kind == k {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("invalid kind: %s", params.Kind)
	}

	// Validate day type
	if params.DayType != myhome.DayTypeWorkDay && params.DayType != myhome.DayTypeDayOff {
		return nil, fmt.Errorf("invalid day_type: %s", params.DayType)
	}

	// Parse time ranges
	ranges, err := parseTimeRanges(params.Ranges)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ranges: %w", err)
	}

	// Save to storage
	if err := s.storage.SetKindSchedule(params.Kind, params.DayType, ranges); err != nil {
		return nil, fmt.Errorf("failed to set kind schedule: %w", err)
	}

	// Update in-memory cache
	s.mu.Lock()
	if _, exists := s.kindSchedules[params.Kind]; !exists {
		s.kindSchedules[params.Kind] = make(map[myhome.DayType][]TimeRange)
	}
	// Convert to internal TimeRange format
	internalRanges := make([]TimeRange, len(ranges))
	for i, r := range ranges {
		internalRanges[i] = TimeRange{Start: r.Start, End: r.End}
	}
	s.kindSchedules[params.Kind][params.DayType] = internalRanges
	s.mu.Unlock()

	s.log.Info("Kind schedule set", "kind", params.Kind, "day_type", params.DayType, "ranges", len(ranges))

	// Publish updates for all rooms with this kind
	if err := s.publishKindScheduleUpdate(params.Kind, params.DayType); err != nil {
		s.log.Error(err, "Failed to publish kind schedule update", "kind", params.Kind, "day_type", params.DayType)
	}

	return &myhome.TemperatureSetKindScheduleResult{
		Status:  "ok",
		Kind:    params.Kind,
		DayType: params.DayType,
	}, nil
}

// parseTimeRanges converts "HH:MM-HH:MM" strings to TemperatureTimeRange structs
func parseTimeRanges(rangeStrs []string) ([]myhome.TemperatureTimeRange, error) {
	ranges := make([]myhome.TemperatureTimeRange, 0, len(rangeStrs))

	for _, rangeStr := range rangeStrs {
		// Split by dash
		var startStr, endStr string
		dashIdx := -1
		for i := 0; i < len(rangeStr); i++ {
			if rangeStr[i] == '-' && i > 0 {
				dashIdx = i
				break
			}
		}

		if dashIdx == -1 {
			return nil, fmt.Errorf("invalid range format: %s (expected HH:MM-HH:MM)", rangeStr)
		}

		startStr = rangeStr[:dashIdx]
		endStr = rangeStr[dashIdx+1:]

		// Parse start and end times
		start, err := parseTimeString(startStr)
		if err != nil {
			return nil, fmt.Errorf("invalid start time in range %s: %w", rangeStr, err)
		}

		end, err := parseTimeString(endStr)
		if err != nil {
			return nil, fmt.Errorf("invalid end time in range %s: %w", rangeStr, err)
		}

		ranges = append(ranges, myhome.TemperatureTimeRange{
			Start: start,
			End:   end,
		})
	}

	return ranges, nil
}

// publishKindScheduleUpdate publishes MQTT updates for all rooms with the given kind
func (s *Service) publishKindScheduleUpdate(kind myhome.RoomKind, dayType myhome.DayType) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for roomID, config := range s.rooms {
		// Check if room has this kind
		hasKind := false
		for _, k := range config.Kinds {
			if k == kind {
				hasKind = true
				break
			}
		}

		if hasKind {
			// Publish update for this room and day type
			if err := s.PublishRangesUpdate(context.Background(), roomID); err != nil {
				s.log.Error(err, "Failed to publish ranges update", "room_id", roomID, "day_type", dayType)
			}
		}
	}

	return nil
}
