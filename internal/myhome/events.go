package myhome

import "time"

// EventListRequest is the parameter type for the event.list RPC verb.
type EventListRequest struct {
	DeviceID  string        `json:"device_id,omitempty"`
	EventType string        `json:"event,omitempty"`
	Severity  string        `json:"severity,omitempty"`
	Since     time.Duration `json:"since,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	Offset    int           `json:"offset,omitempty"`
}

// EventListResponse is the result type for the event.list RPC verb.
type EventListResponse struct {
	Events []EventView `json:"events"`
	Total  int         `json:"total"`
}

// EventView is a JSON-serialisable view of a stored event row.
type EventView struct {
	ID         int64   `json:"id"`
	Ts         float64 `json:"ts"`
	ReceivedAt float64 `json:"received_at"`
	DeviceID   string  `json:"device_id"`
	Component  string  `json:"component"`
	Event      string  `json:"event"`
	Severity   string  `json:"severity"`
	Data       *string `json:"data,omitempty"`
}
