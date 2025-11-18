package temperature

import (
	"fmt"
	"myhome"
	"time"

	"github.com/go-logr/logr"
)

// MethodHandlers handles temperature RPC methods
type MethodHandlers struct {
	storage *Storage
	log     logr.Logger
}

// NewMethodHandlers creates temperature method handlers
func NewMethodHandlers(log logr.Logger, storage *Storage) *MethodHandlers {
	return &MethodHandlers{
		storage: storage,
		log:     log.WithName("temperature.methods"),
	}
}

// RegisterHandlers registers all temperature method handlers with the RPC system
func (h *MethodHandlers) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.TemperatureGet, h.handleGet)
	myhome.RegisterMethodHandler(myhome.TemperatureSet, h.handleSet)
	myhome.RegisterMethodHandler(myhome.TemperatureList, h.handleList)
	myhome.RegisterMethodHandler(myhome.TemperatureDelete, h.handleDelete)
	myhome.RegisterMethodHandler(myhome.TemperatureSetpoint, h.handleGetSetpoint)

	h.log.Info("Temperature RPC handlers registered")
}

// handleGet retrieves a room configuration
func (h *MethodHandlers) handleGet(params any) (any, error) {
	p, ok := params.(*myhome.TemperatureGetParams)
	if !ok {
		return nil, fmt.Errorf("invalid params type")
	}

	config, err := h.storage.GetRoom(p.RoomID)
	if err != nil {
		return nil, err
	}

	// Convert to RPC type
	result := &myhome.TemperatureRoomConfig{
		RoomID:      p.RoomID,
		Name:        config.Name,
		ComfortTemp: config.ComfortTemp,
		EcoTemp:     config.EcoTemp,
		Schedule: myhome.TemperatureSchedule{
			Weekday: convertTimeRanges(config.Schedule.Weekday),
			Weekend: convertTimeRanges(config.Schedule.Weekend),
		},
	}

	return result, nil
}

// handleSet creates or updates a room configuration
func (h *MethodHandlers) handleSet(params any) (any, error) {
	p, ok := params.(*myhome.TemperatureSetParams)
	if !ok {
		return nil, fmt.Errorf("invalid params type")
	}

	// Parse time ranges
	weekday, err := parseTimeRangeStrings(p.Weekday)
	if err != nil {
		return nil, fmt.Errorf("invalid weekday schedule: %w", err)
	}

	weekend, err := parseTimeRangeStrings(p.Weekend)
	if err != nil {
		return nil, fmt.Errorf("invalid weekend schedule: %w", err)
	}

	config := &RoomConfig{
		Name:        p.Name,
		ComfortTemp: p.ComfortTemp,
		EcoTemp:     p.EcoTemp,
		Schedule: &Schedule{
			Weekday: weekday,
			Weekend: weekend,
		},
	}

	if err := h.storage.SetRoom(p.RoomID, config); err != nil {
		return nil, err
	}

	return &myhome.TemperatureSetResult{
		Status: "ok",
		RoomID: p.RoomID,
	}, nil
}

// handleList returns all room configurations
func (h *MethodHandlers) handleList(params any) (any, error) {
	rooms, err := h.storage.ListRooms()
	if err != nil {
		return nil, err
	}

	// Convert to RPC types
	result := make(myhome.TemperatureRoomList)
	for roomID, config := range rooms {
		result[roomID] = &myhome.TemperatureRoomConfig{
			RoomID:      roomID,
			Name:        config.Name,
			ComfortTemp: config.ComfortTemp,
			EcoTemp:     config.EcoTemp,
			Schedule: myhome.TemperatureSchedule{
				Weekday: convertTimeRanges(config.Schedule.Weekday),
				Weekend: convertTimeRanges(config.Schedule.Weekend),
			},
		}
	}

	return &result, nil
}

// handleDelete removes a room configuration
func (h *MethodHandlers) handleDelete(params any) (any, error) {
	p, ok := params.(*myhome.TemperatureDeleteParams)
	if !ok {
		return nil, fmt.Errorf("invalid params type")
	}

	if err := h.storage.DeleteRoom(p.RoomID); err != nil {
		return nil, err
	}

	return &myhome.TemperatureDeleteResult{
		Status: "ok",
		RoomID: p.RoomID,
	}, nil
}

// handleGetSetpoint returns the current setpoint for a room
func (h *MethodHandlers) handleGetSetpoint(params any) (any, error) {
	p, ok := params.(*myhome.TemperatureGetSetpointParams)
	if !ok {
		return nil, fmt.Errorf("invalid params type")
	}

	config, err := h.storage.GetRoom(p.RoomID)
	if err != nil {
		return nil, err
	}

	// Determine current setpoint based on time
	now := time.Now()
	isComfort := config.Schedule.IsComfortTime(now)

	var activeSetpoint float64
	var reason string
	if isComfort {
		activeSetpoint = config.ComfortTemp
		reason = "comfort_hours"
	} else {
		activeSetpoint = config.EcoTemp
		reason = "eco_hours"
	}

	return &myhome.TemperatureSetpointResult{
		SetpointComfort: config.ComfortTemp,
		SetpointEco:     config.EcoTemp,
		ActiveSetpoint:  activeSetpoint,
		Reason:          reason,
	}, nil
}

// Helper function to convert internal TimeRange to RPC TimeRange
func convertTimeRanges(ranges []TimeRange) []myhome.TemperatureTimeRange {
	result := make([]myhome.TemperatureTimeRange, len(ranges))
	for i, r := range ranges {
		result[i] = myhome.TemperatureTimeRange{
			Start: r.Start,
			End:   r.End,
		}
	}
	return result
}
