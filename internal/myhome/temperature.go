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
	RoomID      string     `json:"room_id"`
	Name        string     `json:"name"`
	Kinds       []RoomKind `json:"kinds"` // Room kinds (can be multiple)
	ComfortTemp float64    `json:"comfort_temp"`
	EcoTemp     float64    `json:"eco_temp"`
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
type TemperatureGetWeekdayDefaultsParams struct {
	RoomID string `json:"room_id"`
}

// TemperatureSetWeekdayDefaultParams represents parameters for temperature.setweekdaydefault
type TemperatureSetWeekdayDefaultParams struct {
	RoomID  string  `json:"room_id"`
	Weekday int     `json:"weekday"`  // 0=Sunday, 1=Monday, ..., 6=Saturday
	DayType DayType `json:"day_type"` // work-day or day-off
}

// TemperatureRoomConfig represents a room's temperature configuration
type TemperatureRoomConfig struct {
	RoomID      string     `json:"room_id"`
	Name        string     `json:"name"`
	Kinds       []RoomKind `json:"kinds"` // Room kinds (can be multiple)
	ComfortTemp float64    `json:"comfort_temp"`
	EcoTemp     float64    `json:"eco_temp"`
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
	Start string `json:"start"` // "HH:MM" format
	End   string `json:"end"`   // "HH:MM" format
}

// TemperatureScheduleResult represents the result of temperature.getschedule
// Returns the full day schedule - heater decides active setpoint based on time and occupancy
type TemperatureScheduleResult struct {
	RoomID        string                 `json:"room_id"`
	Date          string                 `json:"date"`           // YYYY-MM-DD format
	Weekday       int                    `json:"weekday"`        // 0=Sunday, 1=Monday, ..., 6=Saturday
	DayType       DayType                `json:"day_type"`       // Day type for this date (from weekday default or external API)
	ComfortTemp   float64                `json:"comfort_temp"`   // Comfort temperature
	EcoTemp       float64                `json:"eco_temp"`       // Eco temperature
	ComfortRanges []TemperatureTimeRange `json:"comfort_ranges"` // Union of all comfort ranges for room's kinds
}

// TemperatureWeekdayDefaults represents weekday defaults for a room
type TemperatureWeekdayDefaults struct {
	RoomID   string          `json:"room_id"`
	Defaults map[int]DayType `json:"defaults"` // weekday (0-6) -> day-type
}

// TemperatureSetWeekdayDefaultResult represents the result of temperature.setweekdaydefault
type TemperatureSetWeekdayDefaultResult struct {
	Status  string  `json:"status"`
	RoomID  string  `json:"room_id"`
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
