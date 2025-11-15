package gen1

import (
	"net"
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

// Note: IsGen1Device function has been moved to pkg/shelly package
// to avoid import cycles. Use shelly.IsGen1Device(deviceId) instead.
