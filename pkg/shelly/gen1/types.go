package gen1

import (
	"net"
	"strings"
)

// Sensor contains sensor data from Gen1 devices
type Sensor struct {
	Temperature *float32 `schema:"temp" json:"temperature,omitempty"`
	Humidity    *uint    `schema:"hum" json:"humidity,omitempty"`
	Flood       *uint32  `schema:"flood" json:"flood,omitempty"`
	BatV        *float32 `schema:"batV" json:"battery_voltage,omitempty"`
}

// The gen1.Device struct is used to represent a Gen1 device
// (with both info & status data) as received from the HTTP API.
// The specificiation for this payloas is here:
// <https://shelly-api-docs.shelly.cloud/gen1/#shelly-h-amp-t>
type Device struct {
	// Common fields
	Id           string `schema:"id" json:"id"`
	Ip           net.IP `json:"ip"`
	FirmwareDate string `json:"fw_date,omitempty"`
	FirmwareId   string `json:"fw_id,omitempty"`
	Model        string `json:"model,omitempty"`
	Sensor       `json:"sensor,omitempty"`
}

// Gen1 device ID prefixes that identify Gen1 devices
var gen1Prefixes = []string{
	"shellyht-",      // Shelly H&T (Humidity & Temperature)
	"shellyflood-",   // Shelly Flood
	"shelly1-",       // Shelly 1
	"shelly1pm-",     // Shelly 1PM
	"shelly25-",      // Shelly 2.5
	"shellyplug-",    // Shelly Plug
	"shellydimmer-",  // Shelly Dimmer
	"shellyrgbw2-",   // Shelly RGBW2
	"shellybulb-",    // Shelly Bulb
	"shellydw-",      // Shelly Door/Window
	"shellyem-",      // Shelly EM
	"shelly3em-",     // Shelly 3EM
	"shellyuni-",     // Shelly UNI
}

// IsGen1Device returns true if the device ID indicates a Gen1 device
// Gen1 devices are identified by their ID prefix (e.g., "shellyht-", "shellyflood-")
func IsGen1Device(deviceId string) bool {
	for _, prefix := range gen1Prefixes {
		if strings.HasPrefix(deviceId, prefix) {
			return true
		}
	}
	return false
}
