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
