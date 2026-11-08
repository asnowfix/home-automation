package myhome

import "errors"

// ErrPoolNotEnabled is returned by the pool.getstatus handler when pool tracking is
// disabled, or its device was unreachable at startup, so callers (the UI) can show
// "not enabled" rather than an error.
var ErrPoolNotEnabled = errors.New("pool tracking not enabled")

// PoolGetStatusResult reports the configured pool device's water-supply
// status and today's filtration turnover (achieved vs. configured target),
// for display in both the web UI and `ctl pool status`. There are no request
// params — the daemon always reports on the single device configured via
// --pool-device-id / pool.device_id.
type PoolGetStatusResult struct {
	DeviceID          string  `json:"device_id" yaml:"device_id"`
	WaterSupplyActive bool    `json:"water_supply_active" yaml:"water_supply_active"` // true = protection engaged, pump forced off
	TurnoverAchieved  float64 `json:"turnover_achieved" yaml:"turnover_achieved"`       // pool volumes filtered today so far
	TurnoverTarget    float64 `json:"turnover_target" yaml:"turnover_target"`           // configured daily target (times/day)
	RuntimeSec        int64   `json:"runtime_sec" yaml:"runtime_sec"`                   // pump runtime today, in seconds
}

// PoolSnapshotResult is what the daemon last knew about the pool device,
// answered from memory only (pool.snapshot): it never reaches the device, so
// the UI can call it on every render (#731, AGENTS.md Rule 3). A nil field is
// unknown and must render as "--", never as a default.
type PoolSnapshotResult struct {
	DeviceID          string   `json:"device_id" yaml:"device_id"`
	WaterSupplyActive *bool    `json:"water_supply_active,omitempty" yaml:"water_supply_active,omitempty"` // input:0 state from the device status the daemon already holds
	TurnoverAchieved  *float64 `json:"turnover_achieved,omitempty" yaml:"turnover_achieved,omitempty"`     // as of TurnoverTs
	TurnoverTarget    *float64 `json:"turnover_target,omitempty" yaml:"turnover_target,omitempty"`
	Turnover24h       *float64 `json:"turnover_24h,omitempty" yaml:"turnover_24h,omitempty"` // pool volumes filtered in the 24 h before now, from persisted pool.turnover_today events; completed pump runs only
	RuntimeSec        *int64   `json:"runtime_sec,omitempty" yaml:"runtime_sec,omitempty"`
	TurnoverTs        int64    `json:"turnover_ts,omitempty" yaml:"turnover_ts,omitempty"` // unix seconds the turnover figures were read; 0 = never
}
