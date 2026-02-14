package myhome

// Switch RPC types

import (
	"pkg/shelly/shelly"
	"pkg/shelly/sswitch"
)

// SwitchParams represents parameters for switch.toggle, switch.on, switch.off, switch.status
type SwitchParams struct {
	Identifier string `json:"identifier"` // Device identifier (id/name/host/etc)
	SwitchId   int    `json:"switch_id"`  // Switch component ID (default 0)
}

// SwitchAllParams represents parameters for switch.all
type SwitchAllParams struct {
	Identifier string `json:"identifier"` // Device identifier (id/name/host/etc)
}

// SwitchStatusResult represents the result of switch.status
type SwitchStatusResult struct {
	DeviceID   string          `json:"device_id"`
	DeviceName string          `json:"device_name"`
	Status     *sswitch.Status `json:"status"`
}

// SwitchToggleResult represents the result of switch.toggle
type SwitchToggleResult struct {
	DeviceID   string                     `json:"device_id"`
	DeviceName string                     `json:"device_name"`
	Result     *sswitch.ToogleSetResponse `json:"result"`
}

// SwitchOnOffResult represents the result of switch.on and switch.off
type SwitchOnOffResult struct {
	DeviceID   string                     `json:"device_id"`
	DeviceName string                     `json:"device_name"`
	Result     *sswitch.ToogleSetResponse `json:"result"`
}

// SwitchAllResult represents the result of switch.all
type SwitchAllResult struct {
	DeviceID   string                       `json:"device_id"`
	DeviceName string                       `json:"device_name"`
	Switches   map[int]shelly.SwitchSummary `json:"switches"`
}
