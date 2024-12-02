package devices

import (
	"net"
)

type DeviceIdentifier struct {
	// The manufacturer of the device
	Manufacturer string `db:"manufacturer" json:"manufacturer"`
	// The unique identifier of the device, defined by the manufacturer
	ID string `db:"id" json:"id"`
}

type Device struct {
	DeviceIdentifier
	// The Ethernet hardware address of the device, globally unique & assigned by the manufacturer
	MAC net.HardwareAddr `db:"mac" json:"mac"`
	// The host address of the device (Host address or resolvable hostname), assigned on this network
	Host string `db:"host" json:"host"`
	// The local unique name of the device, defined by the user
	Name   string `db:"name" json:"name"`
	Info   string `db:"info"`
	Config string `db:"config"`
	Status string `db:"status"`
}

func NewDevice(manufacturer, id, name, host string, mac net.HardwareAddr) *Device {
	return &Device{
		DeviceIdentifier: DeviceIdentifier{
			Manufacturer: manufacturer,
			ID:           id,
		},
		MAC:    mac,
		Host:   host,
		Name:   name,
		Info:   "",
		Config: "",
		Status: "",
	}
}
