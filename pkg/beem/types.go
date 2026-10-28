package beem

import "time"

// PowerSample holds a single instantaneous reading from the Beem Energy cloud API.
type PowerSample struct {
	SolarW    float64 `json:"solar_w"`
	DailyWh   float64 `json:"daily_wh"`
	MonthlyWh float64 `json:"monthly_wh"`
	// GridW float64  // reserved for Beem Battery MQTT channel
	Source string    `json:"source"` // "rest" or "mqtt"
	TS     time.Time `json:"ts"`
}

// ClientConfig holds the credentials and polling configuration for the Beem Energy REST API.
type ClientConfig struct {
	Email        string
	Password     string
	PollInterval time.Duration

	// LoginURL, SummaryURL and DevicesURL address the Beem API. Leave them
	// empty for the public endpoints; NewClient fills in the defaults.
	//
	// These are per-client configuration rather than package-level variables
	// so a test can point one client at an httptest.Server without disturbing
	// any other client in the same binary. They used to be package globals
	// that tests reassigned and restored, which raced the Watcher's polling
	// goroutine and blocked running the suite under -race (#453).
	LoginURL   string
	SummaryURL string
	DevicesURL string
}
