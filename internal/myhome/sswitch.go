package myhome

// Switch RPC types

import (
	"pkg/shelly/shelly"
)

// SwitchParams represents parameters for switch.toggle, switch.on, switch.off and switch.status
type SwitchParams struct {
	Identifier string `json:"identifier"` // Device identifier (id/name/host/etc)
	SwitchId   int    `json:"switch_id"`  // Switch component ID (default 0)
}

// SwitchResult represents the result of switch.on, switch.off, switch.toggle and switch.status
type SwitchResult struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	SwitchId   int    `json:"switch_id"` // Switch component ID (default 0)
	On         bool   `json:"on"`        // true if the output channel is currently on, false otherwise
}

// SwitchAllParams represents parameters for switch.all
type SwitchAllParams struct {
	Identifier string `json:"identifier"` // Device identifier (id/name/host/etc)
}

// SwitchAllResult represents the result of switch.all
type SwitchAllResult struct {
	DeviceID   string                       `json:"device_id"`
	DeviceName string                       `json:"device_name"`
	Switches   map[int]shelly.SwitchSummary `json:"switches"`
}
