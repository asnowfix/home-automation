package myhome

// SolarClaimer reports one registered energy claimer (see
// internal/myhome/energy.Registry) enriched, where possible, with a live
// active/speed read. This is a static-identity + best-effort-live-status
// view, not a live-arbitration result — see the follow-up "solar router"
// issue for multi-consumer arbitration.
type SolarClaimer struct {
	Name        string `json:"name" yaml:"name"`
	DeviceID    string `json:"device_id,omitempty" yaml:"device_id,omitempty"`
	Active      bool   `json:"active" yaml:"active"`
	ActiveSpeed string `json:"active_speed,omitempty" yaml:"active_speed,omitempty"`
}

// SolarClaimersListResult is the result of the solar.claimerslist RPC verb.
type SolarClaimersListResult struct {
	Claimers []SolarClaimer `json:"claimers" yaml:"claimers"`
}
