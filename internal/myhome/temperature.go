package myhome

// Temperature RPC types

// TemperatureGetParams represents parameters for temperature.get
type TemperatureGetParams struct {
	RoomID string `json:"room_id"`
}

// TemperatureSetParams represents parameters for temperature.set
type TemperatureSetParams struct {
	RoomID      string   `json:"room_id"`
	Name        string   `json:"name"`
	ComfortTemp float64  `json:"comfort_temp"`
	EcoTemp     float64  `json:"eco_temp"`
	Weekday     []string `json:"weekday"`
	Weekend     []string `json:"weekend"`
}

// TemperatureDeleteParams represents parameters for temperature.delete
type TemperatureDeleteParams struct {
	RoomID string `json:"room_id"`
}

// TemperatureGetSetpointParams represents parameters for temperature.getsetpoint
type TemperatureGetSetpointParams struct {
	RoomID string `json:"room_id"`
}

// TemperatureRoomConfig represents a room's temperature configuration
type TemperatureRoomConfig struct {
	RoomID      string              `json:"room_id"`
	Name        string              `json:"name"`
	ComfortTemp float64             `json:"comfort_temp"`
	EcoTemp     float64             `json:"eco_temp"`
	Schedule    TemperatureSchedule `json:"schedule"`
}

// TemperatureSchedule defines time-based rules for comfort mode
type TemperatureSchedule struct {
	Weekday []TemperatureTimeRange `json:"weekday"`
	Weekend []TemperatureTimeRange `json:"weekend"`
}

// TemperatureTimeRange represents a time period
type TemperatureTimeRange struct {
	Start string `json:"start"` // "HH:MM" format
	End   string `json:"end"`   // "HH:MM" format
}

// TemperatureSetpointResult represents the result of temperature.getsetpoint
type TemperatureSetpointResult struct {
	SetpointComfort float64 `json:"setpoint_comfort"`
	SetpointEco     float64 `json:"setpoint_eco"`
	ActiveSetpoint  float64 `json:"active_setpoint"`
	Reason          string  `json:"reason"`
}

// TemperatureRoomList represents a list of room configurations
type TemperatureRoomList map[string]*TemperatureRoomConfig

// TemperatureSetResult represents the result of temperature.set
type TemperatureSetResult struct {
	Status string `json:"status"`
	RoomID string `json:"room_id"`
}

// TemperatureDeleteResult represents the result of temperature.delete
type TemperatureDeleteResult struct {
	Status string `json:"status"`
	RoomID string `json:"room_id"`
}
