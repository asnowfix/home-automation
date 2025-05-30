package sswitch

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Switch

type InMode uint32

const (
	Momentary InMode = iota
	Follow
	Flip
	Detached
	Cycle
)

func (im InMode) String() string {
	return [...]string{"momentary", "follow", "flip", "detached", "cycle"}[im]
}

type InitialState uint32

const (
	Off InitialState = iota
	On
	RestoreLast
	MatchInput
)

func (is InitialState) String() string {
	return [...]string{"off", "on", "restore_last", "match_input"}[is]
}

type Error uint32

const (
	OverTemp Error = iota
	OverPower
	OverVoltage
	UnderVoltage
)

func (e Error) String() string {
	return [...]string{"overtemp", "overpower", "overvoltage", "undervoltage"}[e]
}

type Config struct {
	Id                       int     `json:"id"`                                   // Id of the Switch component instance
	Name                     string  `json:"name,omitempty"`                       // Name of the switch instance
	InMode                   string  `json:"in_mode"`                              // Mode of the associated input. Range of values: momentary, follow, flip, detached, cycle (if applicable)
	InitialState             string  `json:"initial_state"`                        // Output state to set on power_on. Range of values: off, on, restore_last, match_input
	AutoOn                   bool    `json:"auto_on"`                              // True if the "Automatic ON" function is enabled, false otherwise
	AutoOnDelay              float32 `json:"auto_on_delay"`                        // Seconds to pass until the component is switched back on
	AutoOff                  bool    `json:"auto_off"`                             // True if the "Automatic OFF" function is enabled, false otherwise
	AutoOffDelay             float32 `json:"auto_off_delay"`                       // Seconds to pass until the component is switched back off
	AutorecoverVoltageErrors bool    `json:"autorecover_voltage_errors,omitempty"` // True if switch output state should be restored after over/undervoltage error is cleared, false otherwise (shown if applicable)
	InputId                  int     `json:"input_id,omitempty"`                   //Id of the Input component which controls the Switch. Applicable only to Pro1 and Pro1PM devices. Valid values: 0, 1
	PowerLimit               float32 `json:"power_limit,omitempty"`                // Limit (in Watts) over which overpower condition occurs (shown if applicable)
	VoltageLimit             float32 `json:"voltage_limit,omitempty"`              // Limit (in Volts) over which overvoltage condition occurs (shown if applicable)
	UnderVoltageLimit        float32 `json:"undervoltage_limit,omitempty"`         // Limit (in Volts) under which undervoltage condition occurs (shown if applicable)
	CurrentLimit             float32 `json:"current_limit,omitempty"`              // Number, limit (in Amperes) over which overcurrent condition occurs (shown if applicable)
}

type ConfigurationRequest struct {
	Id            int    `json:"id"`     // Id of the Switch component instance
	Configuration Config `json:"config"` // Configuration that the method takes
}

type InputConfig struct {
	Id                       int     `json:"id"`                                   // Id of the Switch component instance
	Name                     *string `json:"name"`                                 // Name of the switch instance
	InMode                   string  `json:"in_mode"`                              // Mode of the associated input
	InitialState             string  `json:"initial_state"`                        // Output state to set on power_on
	AutoOn                   bool    `json:"auto_on"`                              // True if "Automatic ON" function is enabled
	AutoOnDelay              int     `json:"auto_on_delay"`                        // Seconds to pass until switched back on
	AutoOff                  bool    `json:"auto_off"`                             // True if "Automatic OFF" function is enabled
	AutoOffDelay             int     `json:"auto_off_delay"`                       // Seconds to pass until switched back off
	AutorecoverVoltageErrors bool    `json:"autorecover_voltage_errors,omitempty"` // Restore state after voltage error (shown if applicable)
	InputId                  int     `json:"input_id,omitempty"`                   // Id of the Input component which controls the Switch. Applicable only to Pro1 and Pro1PM devices. Valid values: 0, 1. (shown if applicable)
	PowerLimit               int     `json:"power_limit,omitempty"`                // Limit (in Watts) for overpower condition (shown if applicable)
	VoltageLimit             int     `json:"voltage_limit,omitempty"`              // Limit (in Volts) for overvoltage condition (shown if applicable)
	UndervoltageLimit        int     `json:"undervoltage_limit,omitempty"`         // Limit (in Volts) for undervoltage condition (shown if applicable)
	CurrentLimit             int     `json:"current_limit,omitempty"`              // Limit (in Amperes) for overcurrent condition (shown if applicable)
	Reverse                  bool    `json:"reverse,omitempty"`                    // Reverse measurement direction of active power
}

type InputStatus struct {
	Id    int  `json:"id"`    // Id of the Switch component instance
	State bool `json:"state"` // Current state of this input
}

type Status struct {
	Input          InputStatus `json:"input"`
	Id             int         `json:"id"`                         //Id of the Switch component instance
	Source         string      `json:"source"`                     // Source of the last command, for example: init, WS_in, http, ...
	Output         bool        `json:"output"`                     // true if the output channel is currently on, false otherwise
	TimerStartedAt float32     `json:"timer_started_at,omitempty"` // Unix timestamp, start time of the timer (in UTC) (shown if the timer is triggered)
	TimerDuration  float32     `json:"timer_duration,omitempty"`   // Duration of the timer in seconds (shown if the timer is triggered)
	Apower         float32     `json:"apower,omitempty"`           // Last measured instantaneous active power (in Watts) delivered to the attached load (shown if applicable)
	Voltage        float32     `json:"voltage,omitempty"`          // Last measured voltage in Volts (shown if applicable)
	Current        float32     `json:"current,omitempty"`          // Last measured current in Amperes (shown if applicable)
	PowerFactor    float32     `json:"pf"`                         // Last measured power factor (shown if applicable)
	Freq           float32     `json:"freq"`                       // Last measured network frequency in Hz (shown if applicable)
	Aenergy        struct {
		Total    float32   `json:"total"`     // Total energy consumed in Watt-hours
		ByMinute []float32 `json:"by_minute"` // Energy consumption by minute (in Milliwatt-hours) for the last three minutes (the lower the index of the element in the array, the closer to the current moment the minute)
		MinuteTs int       `json:"minute_ts"` // Unix timestamp of the first second of the last minute (in UTC)
	} `json:"aenergy,omitempty"`
	Temperature struct {
		Celsius    float32 `json:"tC,omitempty"` // Temperature in Celsius (null if temperature is out of the measurement range)
		Fahrenheit float32 `json:"tF,omitempty"` // Temperature in Fahrenheit (null if temperature is out of the measurement range)
	} `json:"temperature"`
	Errors []string `json:"errors"` // Error conditions occurred. May contain overtemp, overpower, overvoltage, undervoltage, (shown if at least one error is present)
}

type ToogleSetResponse struct {
	WasOn bool `json:"was_on"`
}

type ToggleRequest struct {
	Id int `json:"id"`
}

type SetRequest struct {
	Id          int  `json:"id"`                     // Id of the Switch component instance. Required
	On          bool `json:"on"`                     // true for switch on, false otherwise. Required
	ToggleAfter int  `json:"toggle_after,omitempty"` // Optional flip-back timer in seconds. Optional
}

var SwitchedOffKey map[string]any = map[string]any{"key": "switched-off"}
