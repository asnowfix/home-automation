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
}
