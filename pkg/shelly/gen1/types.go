package gen1

import (
	"net"
)

// The gen1.Device struct is used to represent a Gen1 device
// (with both info & status data) as received from the HTTP API.
// The specificiation for this payloas is here:
// <https://shelly-api-docs.shelly.cloud/gen1/#shelly-h-amp-t>
type Device struct {
	// Common fields
	Id           string  `schema:"id" json:"id"`
	Ip           net.IP  `json:"ip"`
	FirmwareDate string  `json:"fw_date,omitempty"`
	FirmwareId   string  `json:"fw_id,omitempty"`
	Model        string  `json:"model,omitempty"`
	Temperature  float32 `schema:"temp" json:"temperature"`

	// H&T specific (optional)
	Humidity *uint `schema:"hum" json:"humidity,omitempty"`

	// Flood specific (optional)
	Flood          *uint32  `schema:"flood" json:"flood,omitempty"`
	BatteryVoltage *float32 `schema:"batV" json:"battery_voltage,omitempty"`
}

// IsHTSensor returns true if this is a Humidity & Temperature sensor
func (d *Device) IsHTSensor() bool {
	return d.Humidity != nil
}

// IsFloodSensor returns true if this is a Flood sensor
func (d *Device) IsFloodSensor() bool {
	return d.Flood != nil
}
