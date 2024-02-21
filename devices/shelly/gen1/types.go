package gen1

import (
	"net"
)

type HTSensor struct {
	Humidity    uint    `schema:"hum,required"  json:"humidity"`
	Temperature float32 `schema:"temp,required" json:"temperature"`
	Id          string  `schema:"id,required"   json:"id"`
}

type Device struct {
	Ip           net.IP `json:"ip"`
	FirmwareDate string `json:"fw_date,omitempty"`
	FirmwareId   string `json:"fw_id,omitempty"`
	Model        string `json:"model,omitempty"`
	*HTSensor
}
