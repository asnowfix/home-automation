package rooms

import (
	"context"
	"fmt"
	"github.com/asnowfix/home-automation/internal/myhome"
	"time"
)

// HandleGet handles temperature.get RPC method
func (s *Service) HandleGet(ctx context.Context, params *myhome.TemperatureGetParams) (*myhome.TemperatureRoomConfig, error) {
	config, err := s.storage.GetRoom(params.RoomID)
	if err != nil {
		return nil, err
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
	if _, hasEco := p.Levels["eco"]; !hasEco {
		return nil, fmt.Errorf("'eco' temperature level is required (it's the default)")
	}

	config := &RoomConfig{
		ID:     p.RoomID,
		Name:   p.Name,
		Kinds:  p.Kinds,
		Levels: p.Levels,
	}

	modified, err := s.storage.SaveRoom(config)
	if err != nil {
		return nil, fmt.Errorf("failed to save room: %w", err)
	}

	if modified {
		s.log.Info("Room configuration updated", "room_id", p.RoomID, "name", p.Name)
	} else {
		s.log.V(1).Info("Room configuration unchanged", "room_id", p.RoomID)
	}

	return &myhome.TemperatureSetResult{
		Status: "ok",
		RoomID: p.RoomID,
	}, nil
}

// HandleList handles temperature.list RPC method
func (s *Service) HandleList(ctx context.Context) (*myhome.TemperatureRoomList, error) {
	rooms, err := s.storage.ListRooms()
	if err != nil {
		return nil, fmt.Errorf("failed to list rooms: %w", err)
	}

	result := make(myhome.TemperatureRoomList, len(rooms))
	for roomID, config := range rooms {
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
	if err := s.storage.DeleteRoom(params.RoomID); err != nil {
		return nil, err
	}

	s.log.Info("Room configuration deleted", "room_id", params.RoomID)

	return &myhome.TemperatureDeleteResult{
		Status: "ok",
		RoomID: params.RoomID,
	}, nil
}

// HandleGetSchedule handles temperature.getschedule RPC method
func (s *Service) HandleGetSchedule(ctx context.Context, params *myhome.TemperatureGetScheduleParams) (*myhome.TemperatureScheduleResult, error) {
	config, err := s.storage.GetRoom(params.RoomID)
	if err != nil {
		return nil, err
	}

	var date time.Time
	if params.Date != nil && *params.Date != "" {
		date, err = time.Parse("2006-01-02", *params.Date)
		if err != nil {
			return nil, fmt.Errorf("invalid date format: %s (expected YYYY-MM-DD)", *params.Date)
		}
	} else {
		date = time.Now()
	}

	weekday := int(date.Weekday())

	dayType, err := s.storage.GetWeekdayDefault(weekday)
	if err != nil {
		if weekday == 0 || weekday == 6 {
			dayType = myhome.DayTypeDayOff
		} else {
			dayType = myhome.DayTypeWorkDay
		}
	}

	ranges, _, err := s.GetComfortRanges(ctx, params.RoomID, date)
	if err != nil {
		return nil, fmt.Errorf("failed to get comfort ranges: %w", err)
	}

	comfortRanges := make([]myhome.TemperatureTimeRange, len(ranges))
	for i, r := range ranges {
		comfortRanges[i] = myhome.TemperatureTimeRange{Start: r.Start, End: r.End}
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
func (s *Service) HandleGetWeekdayDefaults(ctx context.Context, params *myhome.TemperatureGetWeekdayDefaultsParams) (*myhome.TemperatureWeekdayDefaults, error) {
	defaults, err := s.storage.GetWeekdayDefaults()
	if err != nil {
		return nil, fmt.Errorf("failed to get weekday defaults: %w", err)
	}

	return &myhome.TemperatureWeekdayDefaults{Defaults: defaults}, nil
}

// HandleSetWeekdayDefault handles temperature.setweekdaydefault RPC method
func (s *Service) HandleSetWeekdayDefault(ctx context.Context, params *myhome.TemperatureSetWeekdayDefaultParams) (*myhome.TemperatureSetWeekdayDefaultResult, error) {
	if params.Weekday < 0 || params.Weekday > 6 {
		return nil, fmt.Errorf("invalid weekday: %d (must be 0-6)", params.Weekday)
	}
	if params.DayType != myhome.DayTypeWorkDay && params.DayType != myhome.DayTypeDayOff {
		return nil, fmt.Errorf("invalid day_type: %s (must be 'work-day' or 'day-off')", params.DayType)
	}

	modified, err := s.storage.SetWeekdayDefault(params.Weekday, params.DayType)
	if err != nil {
		return nil, fmt.Errorf("failed to set weekday default: %w", err)
	}

	if modified {
		s.log.Info("Global weekday default set", "weekday", params.Weekday, "day_type", params.DayType)
	} else {
		s.log.V(1).Info("Global weekday default unchanged", "weekday", params.Weekday)
	}

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
	if params.DayType != myhome.DayTypeWorkDay && params.DayType != myhome.DayTypeDayOff {
		return nil, fmt.Errorf("invalid day_type: %s", params.DayType)
	}

	ranges, err := parseTimeRanges(params.Ranges)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ranges: %w", err)
	}

	modified, err := s.storage.SetKindSchedule(params.Kind, params.DayType, ranges)
	if err != nil {
		return nil, fmt.Errorf("failed to set kind schedule: %w", err)
	}

	if modified {
		s.log.Info("Kind schedule updated", "kind", params.Kind, "day_type", params.DayType, "ranges", len(ranges))
	} else {
		s.log.V(1).Info("Kind schedule unchanged", "kind", params.Kind, "day_type", params.DayType)
	}

	if err := s.publishKindScheduleUpdate(ctx, params.Kind, params.DayType); err != nil {
		s.log.Error(err, "Failed to publish kind schedule update", "kind", params.Kind)
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
		dashIdx := -1
		for i := 1; i < len(rangeStr); i++ {
			if rangeStr[i] == '-' {
				dashIdx = i
				break
			}
		}
		if dashIdx == -1 {
			return nil, fmt.Errorf("invalid range format: %s (expected HH:MM-HH:MM)", rangeStr)
		}

		start, err := parseTimeString(rangeStr[:dashIdx])
		if err != nil {
			return nil, fmt.Errorf("invalid start time in range %s: %w", rangeStr, err)
		}

		end, err := parseTimeString(rangeStr[dashIdx+1:])
		if err != nil {
			return nil, fmt.Errorf("invalid end time in range %s: %w", rangeStr, err)
		}

		ranges = append(ranges, myhome.TemperatureTimeRange{Start: start, End: end})
	}

	return ranges, nil
}

// HandleRoomList handles room.list RPC method
func (s *Service) HandleRoomList(ctx context.Context) (*myhome.RoomListResult, error) {
	rooms, err := s.storage.ListRooms()
	if err != nil {
		return nil, fmt.Errorf("failed to list rooms: %w", err)
	}

	list := make([]myhome.RoomInfo, 0, len(rooms))
	for _, config := range rooms {
		list = append(list, myhome.RoomInfo{
			ID:   config.ID,
			Name: config.Name,
		})
	}

	return &myhome.RoomListResult{Rooms: list}, nil
}

// HandleRoomCreate handles room.create RPC method
func (s *Service) HandleRoomCreate(ctx context.Context, params *myhome.RoomCreateParams) (*myhome.RoomCreateResult, error) {
	if params.ID == "" {
		return nil, fmt.Errorf("room id is required")
	}

	if _, err := s.storage.GetRoom(params.ID); err == nil {
		return &myhome.RoomCreateResult{Success: false, Message: "room already exists"}, nil
	}

	name := params.Name
	if name == "" {
		name = params.ID
	}

	config := &RoomConfig{
		ID:    params.ID,
		Name:  name,
		Kinds: []myhome.RoomKind{myhome.RoomKindOther},
		Levels: map[string]float64{
			"eco":     18.0,
			"comfort": 20.0,
			"away":    15.0,
		},
	}

	if _, err := s.storage.SaveRoom(config); err != nil {
		return nil, fmt.Errorf("failed to save room: %w", err)
	}

	s.log.Info("Room created", "room_id", params.ID, "name", name)

	return &myhome.RoomCreateResult{Success: true, Message: "room created"}, nil
}

// HandleRoomEdit handles room.edit RPC method
func (s *Service) HandleRoomEdit(ctx context.Context, params *myhome.RoomEditParams) (*myhome.RoomEditResult, error) {
	if params.ID == "" {
		return nil, fmt.Errorf("room id is required")
	}

	config, err := s.storage.GetRoom(params.ID)
	if err != nil {
		return &myhome.RoomEditResult{Success: false, Message: "room not found"}, nil
	}

	if params.Name != nil {
		config.Name = *params.Name
	}
	if len(params.Kinds) > 0 {
		config.Kinds = params.Kinds
	}
	if len(params.Levels) > 0 {
		if _, hasEco := params.Levels["eco"]; !hasEco {
			return &myhome.RoomEditResult{Success: false, Message: "'eco' temperature level is required"}, nil
		}
		config.Levels = params.Levels
	}

	if _, err := s.storage.SaveRoom(config); err != nil {
		return nil, fmt.Errorf("failed to save room: %w", err)
	}

	s.log.Info("Room updated", "room_id", params.ID)

	if err := s.PublishRangesUpdate(ctx, params.ID); err != nil {
		s.log.Error(err, "Failed to publish ranges update after room edit", "room_id", params.ID)
	}

	return &myhome.RoomEditResult{Success: true, Message: "room updated"}, nil
}

// HandleRoomDelete handles room.delete RPC method
func (s *Service) HandleRoomDelete(ctx context.Context, params *myhome.RoomDeleteParams) (*myhome.RoomDeleteResult, error) {
	if params.ID == "" {
		return nil, fmt.Errorf("room id is required")
	}

	if _, err := s.storage.GetRoom(params.ID); err != nil {
		return &myhome.RoomDeleteResult{Success: false, Message: "room not found"}, nil
	}

	if err := s.storage.DeleteRoom(params.ID); err != nil {
		return nil, fmt.Errorf("failed to delete room: %w", err)
	}

	s.log.Info("Room deleted", "room_id", params.ID)

	return &myhome.RoomDeleteResult{Success: true, Message: "room deleted"}, nil
}

// publishKindScheduleUpdate publishes MQTT updates for all rooms that have the given kind
func (s *Service) publishKindScheduleUpdate(ctx context.Context, kind myhome.RoomKind, dayType myhome.DayType) error {
	rooms, err := s.storage.ListRooms()
	if err != nil {
		return fmt.Errorf("failed to list rooms: %w", err)
	}

	for roomID, config := range rooms {
		for _, k := range config.Kinds {
			if k == kind {
				if err := s.PublishRangesUpdate(ctx, roomID); err != nil {
					s.log.Error(err, "Failed to publish ranges update", "room_id", roomID, "day_type", dayType)
				}
				break
			}
		}
	}

	return nil
}
