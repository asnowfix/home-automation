package gen1

import "net"

type HTSensor struct {
	Humidity    uint    `schema:"hum,required"  json:"humidity"`
	Temperature float32 `schema:"temp,required" json:"temperature"`
	Id          string  `schema:"id,required"   json:"id"`
}

type Device struct {
	Ip net.IP `json:"-"`
	*HTSensor
}
