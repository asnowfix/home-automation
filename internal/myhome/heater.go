package myhome

// HeaterGetConfigParams represents parameters for heater.getconfig RPC
type HeaterGetConfigParams struct {
	Identifier string `json:"identifier"` // Device identifier (id/name/host/MAC/IP)
}

// HeaterConfig represents the heater script configuration stored in KVS
type HeaterConfig struct {
	EnableLogging            bool   `json:"enable_logging"`
	RoomID                   string `json:"room_id"`
	CheapStartHour           int    `json:"cheap_start_hour"`
	CheapEndHour             int    `json:"cheap_end_hour"`
	PollIntervalMs           int    `json:"poll_interval_ms"`
	PreheatHours             int    `json:"preheat_hours"`
	NormallyClosed           bool   `json:"normally_closed"`
	InternalTemperatureTopic string `json:"internal_temperature_topic"`
	ExternalTemperatureTopic string `json:"external_temperature_topic"`
}

// HeaterGetConfigResult represents the result of heater.getconfig RPC
type HeaterGetConfigResult struct {
	DeviceID   string        `json:"device_id"`
	DeviceName string        `json:"device_name"`
	HasScript  bool          `json:"has_script"`
	Config     *HeaterConfig `json:"config,omitempty"`
}

// HeaterSetConfigParams represents parameters for heater.setconfig RPC
type HeaterSetConfigParams struct {
	Identifier               string  `json:"identifier"`
	EnableLogging            *bool   `json:"enable_logging,omitempty"`
	RoomID                   *string `json:"room_id,omitempty"`
	CheapStartHour           *int    `json:"cheap_start_hour,omitempty"`
	CheapEndHour             *int    `json:"cheap_end_hour,omitempty"`
	PollIntervalMs           *int    `json:"poll_interval_ms,omitempty"`
	PreheatHours             *int    `json:"preheat_hours,omitempty"`
	NormallyClosed           *bool   `json:"normally_closed,omitempty"`
	InternalTemperatureTopic *string `json:"internal_temperature_topic,omitempty"`
	ExternalTemperatureTopic *string `json:"external_temperature_topic,omitempty"`
}

// HeaterSetConfigResult represents the result of heater.setconfig RPC
type HeaterSetConfigResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// ThermometerInfo represents a temperature sensor device
type ThermometerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	MqttTopic string `json:"mqtt_topic"`
}

// ThermometerListResult represents the result of thermometer.list RPC
type ThermometerListResult struct {
	Thermometers []ThermometerInfo `json:"thermometers"`
}

// DoorInfo represents a door/window sensor device
type DoorInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	MqttTopic string `json:"mqtt_topic"`
}

// DoorListResult represents the result of door.list RPC
type DoorListResult struct {
	Doors []DoorInfo `json:"doors"`
}

// RoomInfo represents a room for temperature management
type RoomInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RoomListResult represents the result of room.list RPC
type RoomListResult struct {
	Rooms []RoomInfo `json:"rooms"`
}

// RoomCreateParams represents parameters for room.create RPC
type RoomCreateParams struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RoomCreateResult represents the result of room.create RPC
type RoomCreateResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// RoomEditParams represents parameters for room.edit RPC
type RoomEditParams struct {
	ID     string             `json:"id"`               // Room ID (required, cannot be changed)
	Name   *string            `json:"name,omitempty"`   // New room name
	Kinds  []RoomKind         `json:"kinds,omitempty"`  // Room kinds (bedroom, office, etc.)
	Levels map[string]float64 `json:"levels,omitempty"` // Temperature levels (eco, comfort, away)
}

// RoomEditResult represents the result of room.edit RPC
type RoomEditResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// RoomDeleteParams represents parameters for room.delete RPC
type RoomDeleteParams struct {
	ID string `json:"id"` // Room ID to delete
}

// RoomDeleteResult represents the result of room.delete RPC
type RoomDeleteResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
