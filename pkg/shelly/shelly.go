package shelly

import (
	"net"
	"pkg/devices"
)

type ShellyDevice struct {
	shelly *Device
}

func (d ShellyDevice) Provider() string {
	return "shelly"
}

func (d ShellyDevice) Name() string {
	return d.shelly.Id()
}

func (d ShellyDevice) Mac() net.HardwareAddr {
	return d.shelly.MacAddress
}

func (d ShellyDevice) Online() bool {
	return true // TODO because found by mDNS
}

func (d ShellyDevice) MarshalJSON() ([]byte, error) {
	return devices.MarshalJSON(d)
}

func (d ShellyDevice) Id() string {
	return d.shelly.Id()
}

func (d ShellyDevice) Ip() net.IP {
	if ip := net.ParseIP(d.shelly.Host()); ip != nil {
		return ip
	}
	return nil
}
