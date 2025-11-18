package temperature

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

// Service implements an HTTP server that computes temperature set-points for rooms
// based on time-based scheduling rules (day/night, weekday/weekend).
//
// Future: Will integrate with Google Calendar for room occupancy detection.
//
// HTTP:
// - GET /temperature/<room-id> -> {"setpoint_comfort":21.0,"setpoint_eco":17.0,"active_setpoint":21.0,"reason":"comfort_hours"}

type Service struct {
	ctx     context.Context
	log     logr.Logger
	httpSrv *http.Server
	rooms   map[string]*RoomConfig
}

// RoomConfig defines temperature settings and schedule for a room
type RoomConfig struct {
	ID          string
	Name        string
	ComfortTemp float64
	EcoTemp     float64
	Schedule    *Schedule
}

// Schedule defines time-based rules for comfort mode
// Eco mode is the default - comfort hours are explicitly defined
type Schedule struct {
	Weekday []TimeRange // Comfort hours on weekdays
	Weekend []TimeRange // Comfort hours on weekends
}

// TimeRange represents a time period with start and end times
type TimeRange struct {
	Start string // "HH:MM" format (24-hour)
	End   string // "HH:MM" format (24-hour)
}

// Response is the JSON response for temperature queries
type Response struct {
	SetpointComfort float64 `json:"setpoint_comfort"`
	SetpointEco     float64 `json:"setpoint_eco"`
	ActiveSetpoint  float64 `json:"active_setpoint"`
	Reason          string  `json:"reason"`
}

// NewService creates a new temperature service
func NewService(ctx context.Context, log logr.Logger, rooms map[string]*RoomConfig) *Service {
	s := &Service{
		ctx:   ctx,
		log:   log.WithName("temperature.Service"),
		rooms: rooms,
	}
	return s
}

// Start runs the HTTP server on the given port
func Start(ctx context.Context, port int, rooms map[string]*RoomConfig) error {
	log := logr.FromContextOrDiscard(ctx)
	svc := NewService(ctx, log, rooms)
	return svc.Start(port)
}

// Start runs the HTTP server
func (s *Service) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/temperature/", s.handleTemperature)

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		<-s.ctx.Done()
		_ = s.httpSrv.Close()
	}()

	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error(err, "temperature HTTP server crashed")
		}
	}()

	s.log.Info("Temperature HTTP service started", "port", port, "rooms", len(s.rooms))
	return nil
}

// handleTemperature handles GET /temperature/<room-id>
func (s *Service) handleTemperature(w http.ResponseWriter, r *http.Request) {
	// Extract room ID from path: /temperature/<room-id>
	path := strings.TrimPrefix(r.URL.Path, "/temperature/")
	roomID := strings.TrimSuffix(path, "/")

	if roomID == "" {
		http.Error(w, "room ID required", http.StatusBadRequest)
		return
	}

	room, ok := s.rooms[roomID]
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	// Determine active set-point based on time-based schedule
	now := time.Now()
	inComfortHours := room.Schedule.IsComfortTime(now)

	var active float64
	var reason string

	if inComfortHours {
		active = room.ComfortTemp
		reason = "comfort_hours"
	} else {
		active = room.EcoTemp
		reason = "eco_hours"
	}

	resp := Response{
		SetpointComfort: room.ComfortTemp,
		SetpointEco:     room.EcoTemp,
		ActiveSetpoint:  active,
		Reason:          reason,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Error(err, "Failed to encode response", "room", roomID)
	}
}

// IsComfortTime checks if the given time falls within comfort hours
// Returns true if within comfort hours, false otherwise (eco mode)
func (s *Schedule) IsComfortTime(t time.Time) bool {
	hour := t.Hour()
	minute := t.Minute()
	currentMinutes := hour*60 + minute

	// Determine if it's a weekend (Saturday=6, Sunday=0)
	weekday := t.Weekday()
	isWeekend := weekday == time.Saturday || weekday == time.Sunday

	// Select appropriate day's comfort hours
	var comfortRanges []TimeRange
	if isWeekend {
		comfortRanges = s.Weekend
	} else {
		comfortRanges = s.Weekday
	}

	// Check if current time is within any comfort range
	for _, tr := range comfortRanges {
		if isInTimeRange(currentMinutes, tr) {
			return true
		}
	}

	// Default to eco mode if not in any comfort range
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
