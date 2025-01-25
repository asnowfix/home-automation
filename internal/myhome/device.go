package myhome

import (
	"net"
	"pkg/shelly"
)

type DeviceIdentifier struct {
	// The manufacturer of the device
	Manufacturer string `db:"manufacturer" json:"manufacturer"`
	// The unique identifier of the device, defined by the manufacturer
	Id string `db:"id" json:"id"`
}

type DeviceSummary struct {
	DeviceIdentifier
	MAC  net.HardwareAddr `db:"mac" json:"mac,omitempty"` // The Ethernet hardware address of the device, globally unique & assigned by the manufacturer
	Host string           `db:"host" json:"host"`         // The host address of the device (Host address or resolvable hostname), assigned on this network
	Name string           `db:"name" json:"name"`         // The local unique name of the device, defined by the user
}

type Device struct {
	DeviceSummary
	ConfigRevision int                `db:"config_revision" json:"config_revision"`
	Info           *shelly.DeviceInfo `db:"-" json:"info"`
	Config         *shelly.Config     `db:"-" json:"config"`
	Status         *shelly.Status     `db:"-" json:"status"`
}

type Devices struct {
	Devices []DeviceSummary `json:"devices"`
}

type Group struct {
	ID          int    `db:"id" json:"-"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description"`
}

type Groups struct {
	Groups []Group `json:"groups"`
}

type GroupDevice struct {
	Manufacturer string `db:"manufacturer" json:"manufacturer"`
	ID           string `db:"id" json:"id"`
	Group        string `db:"group" json:"group"`
}
