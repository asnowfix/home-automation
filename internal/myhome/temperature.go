package myhome

// Temperature RPC types

// DayType represents the type of day for temperature scheduling
type DayType string

const (
	DayTypeWorkDay DayType = "work-day" // Working day
	DayTypeDayOff  DayType = "day-off"  // Day off (weekend, holiday, etc.)
)

// RoomKind represents the type/purpose of a room
type RoomKind string

const (
	RoomKindBedroom    RoomKind = "bedroom"
	RoomKindOffice     RoomKind = "office"
	RoomKindLivingRoom RoomKind = "living-room"
	RoomKindKitchen    RoomKind = "kitchen"
	RoomKindOther      RoomKind = "other"
)

// TemperatureGetParams represents parameters for temperature.get
type TemperatureGetParams struct {
	RoomID string `json:"room_id"`
}

// TemperatureSetParams represents parameters for temperature.set
type TemperatureSetParams struct {
	RoomID string             `json:"room_id"`
	Name   string             `json:"name"`
	Kinds  []RoomKind         `json:"kinds"`  // Room kinds (can be multiple)
	Levels map[string]float64 `json:"levels"` // Temperature levels: "eco", "comfort", "away", etc.
}

// TemperatureDeleteParams represents parameters for temperature.delete
type TemperatureDeleteParams struct {
	RoomID string `json:"room_id"`
}

// TemperatureGetScheduleParams represents parameters for temperature.getschedule
type TemperatureGetScheduleParams struct {
	RoomID string  `json:"room_id"`
	Date   *string `json:"date,omitempty"` // Optional: YYYY-MM-DD format, defaults to today
}

// TemperatureGetWeekdayDefaultsParams represents parameters for temperature.getweekdaydefaults
// Weekday defaults are global and apply to all rooms
type TemperatureGetWeekdayDefaultsParams struct {
	// No parameters - weekday defaults are global
}

// TemperatureSetWeekdayDefaultParams represents parameters for temperature.setweekdaydefault
// Weekday defaults are global and apply to all rooms
type TemperatureSetWeekdayDefaultParams struct {
	Weekday int     `json:"weekday"`  // 0=Sunday, 1=Monday, ..., 6=Saturday
	DayType DayType `json:"day_type"` // work-day or day-off
}

// TemperatureRoomConfig represents a room's temperature configuration
type TemperatureRoomConfig struct {
	RoomID string             `json:"room_id"`
	Name   string             `json:"name"`
	Kinds  []RoomKind         `json:"kinds"`  // Room kinds (can be multiple)
	Levels map[string]float64 `json:"levels"` // Temperature levels: "eco" (default), "comfort", "away", etc.
}

// TemperatureKindSchedule represents comfort time ranges for a room kind and day type
type TemperatureKindSchedule struct {
	Kind    RoomKind               `json:"kind"`     // Room kind
	DayType DayType                `json:"day_type"` // Day type (work-day or day-off)
	Ranges  []TemperatureTimeRange `json:"ranges"`   // Comfort time ranges
}

// TemperatureGetKindSchedulesParams represents parameters for temperature.getkindschedules
type TemperatureGetKindSchedulesParams struct {
	Kind    *RoomKind `json:"kind,omitempty"`     // Optional: filter by kind
	DayType *DayType  `json:"day_type,omitempty"` // Optional: filter by day type
}

// TemperatureSetKindScheduleParams represents parameters for temperature.setkindschedule
type TemperatureSetKindScheduleParams struct {
	Kind    RoomKind `json:"kind"`     // Room kind
	DayType DayType  `json:"day_type"` // Day type
	Ranges  []string `json:"ranges"`   // Comfort time ranges in "HH:MM-HH:MM" format
}

// TemperatureTimeRange represents a time period
type TemperatureTimeRange struct {
	Start int `json:"start"` // Minutes since midnight (0-1439)
	End   int `json:"end"`   // Minutes since midnight (0-1439)
}

// TemperatureScheduleResult represents the result of temperature.getschedule
// Returns the full day schedule - heater decides active setpoint based on time and occupancy
type TemperatureScheduleResult struct {
	RoomID        string                 `json:"room_id"`
	Date          string                 `json:"date"`           // YYYY-MM-DD format
	Weekday       int                    `json:"weekday"`        // 0=Sunday, 1=Monday, ..., 6=Saturday
	DayType       DayType                `json:"day_type"`       // Day type for this date (from weekday default or external API)
	Levels        map[string]float64     `json:"levels"`         // Temperature levels: "eco" (default), "comfort", "away", etc.
	ComfortRanges []TemperatureTimeRange `json:"comfort_ranges"` // Union of all comfort ranges for room's kinds
}

// TemperatureWeekdayDefaults represents global weekday defaults (applies to all rooms)
type TemperatureWeekdayDefaults struct {
	Defaults map[int]DayType `json:"defaults"` // weekday (0-6) -> day-type
}

// TemperatureSetWeekdayDefaultResult represents the result of temperature.setweekdaydefault
type TemperatureSetWeekdayDefaultResult struct {
	Weekday int     `json:"weekday"`
	DayType DayType `json:"day_type"`
}

// TemperatureKindScheduleList represents a list of kind schedules
type TemperatureKindScheduleList []TemperatureKindSchedule

// TemperatureSetKindScheduleResult represents the result of temperature.setkindschedule
type TemperatureSetKindScheduleResult struct {
	Status  string   `json:"status"`
	Kind    RoomKind `json:"kind"`
	DayType DayType  `json:"day_type"`
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
