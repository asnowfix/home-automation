package shelly

import (
	"net"
	"pkg/devices"
)

// func Devices(ctx context.Context, log logr.Logger, devices []devices.Device) []*Device {
// 	var result []*Device
// 	for _, d := range devices {
// 		sd, ok := d.(*Device)
// 		if ok {
// 			result = append(result, sd)
// 		} else {
// 			sd = NewDeviceFromSummary(ctx, log, d)
// 			result = append(result, sd)
// 		}
// 	}
// 	return result
// }

type ShellyDevice struct {
	shelly *Device
}

func (d ShellyDevice) Provider() string {
	return d.shelly.Manufacturer()
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

func (d ShellyDevice) Manufacturer() string {
	return d.shelly.Manufacturer()
}

func (d ShellyDevice) Id() string {
	return d.shelly.Id()
}

func (d ShellyDevice) Host() string {
	return d.shelly.Host()
}

func (d ShellyDevice) Ip() net.IP {
	if ip := net.ParseIP(d.shelly.Host()); ip != nil {
		return ip
	}
	return nil
}
