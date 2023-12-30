package devices

import "net"

type Output uint

const (
	Plug Output = iota
	Heater
	Light
)

type Switch interface {
	Set() error
	Unset() error
	Status() (bool, error)
}

type Button interface {
	Action() error
	Status() (bool, error)
}

type Host struct {
	Name   string           `json:"name"`
	Ip     net.IP           `json:"ip"`
	Mac    net.HardwareAddr `json:"mac"`
	Online bool             `json:"online"`
}
