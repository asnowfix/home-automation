package gen1

import (
	"net"
)

// https://shelly-api-docs.shelly.cloud/gen1/#shelly-h-amp-t
type HTSensor struct {
	Humidity    uint    `schema:"hum,required"  json:"humidity"`
	Temperature float32 `schema:"temp,required" json:"temperature"`
	Id          string  `schema:"id,required"   json:"id"`
}

type Flood struct {
	Temperature    float32 `schema:"temp,required" json:"temperature"`
	Flood          uint    `schema:"flood,required"  json:"flood"`
	BatteryVoltage float32 `schema:"batV,required"  json:"battery_voltage"`
	Id             string  `schema:"id,required"   json:"id"`
}

type Device struct {
	Ip           net.IP `json:"ip"`
	FirmwareDate string `json:"fw_date,omitempty"`
	FirmwareId   string `json:"fw_id,omitempty"`
	Model        string `json:"model,omitempty"`
	*HTSensor
	*Flood
}
