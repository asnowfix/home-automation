package gen1

import (
	"encoding/json"
	"net"
)

// https://shelly-api-docs.shelly.cloud/gen1/#shelly-h-amp-t
type HTSensor struct {
	Id_         string  `json:"id"`
	Humidity    uint    `schema:"hum,required"  json:"humidity"`
	Temperature float32 `schema:"temp,required" json:"temperature"`
}

type Flood struct {
	Id_            string  `json:"id"`
	Temperature    float32 `schema:"temp,required" json:"temperature"`
	Flood          uint32  `schema:"flood,required"  json:"flood"`
	BatteryVoltage float32 `schema:"batV,required"  json:"battery_voltage"`
}

type Device struct {
	Id           string `json:"-"`
	Ip           net.IP `json:"ip"`
	FirmwareDate string `json:"fw_date,omitempty"`
	FirmwareId   string `json:"fw_id,omitempty"`
	Model        string `json:"model,omitempty"`
	*HTSensor
	*Flood
}

func (d *Device) UnmarshalJSON(data []byte) error {
	type Alias Device
	if err := json.Unmarshal(data, (*Alias)(d)); err != nil {
		return err
	}
	if d.HTSensor != nil {
		d.Id = d.HTSensor.Id_
	}
	if d.Flood != nil {
		d.Id = d.Flood.Id_
	}
	return nil
}
