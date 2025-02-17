package myhome

import (
	"context"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"schedule"

	"github.com/go-logr/logr"
)

type DeviceIdentifier struct {
	// The manufacturer of the device
	Manufacturer string `db:"manufacturer" json:"manufacturer"`
	// The unique identifier of the device, defined by the manufacturer
	Id string `db:"id" json:"id"`
}

type DeviceSummary struct {
	DeviceIdentifier
	MAC  string `db:"mac" json:"mac,omitempty"` // The Ethernet hardware address of the device, globally unique & assigned by the manufacturer
	Host string `db:"host" json:"host"`         // The host address of the device (Host address or resolvable hostname), assigned on this network
	Name string `db:"name" json:"name"`         // The local unique name of the device, defined by the user
}

type Device struct {
	DeviceSummary
	ConfigRevision int                 `db:"config_revision" json:"config_revision"`
	Components     *[]shelly.Component `db:"-" json:"components"`
	Info           *shelly.DeviceInfo  `db:"-" json:"info"`
	Config         *shelly.Config      `db:"-" json:"config"`
	Status         *shelly.Status      `db:"-" json:"status"`
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

func UpdateDeviceFromShelly(ctx context.Context, log logr.Logger, d *Device, sd *shelly.Device, via types.Channel) {
	if d.Info == nil {
		d.Info = sd.Call(ctx, via, "Shelly", "GetDeviceInfo", &shelly.DeviceInfo{}).(*shelly.DeviceInfo)
	}

	if d.Components == nil {
		d.Components = sd.Call(ctx, via, "Shelly", "GetComponents", nil).(*shelly.ComponentsResponse).Components
	}

	if d.Config == nil {
		d.Config = sd.Call(ctx, via, "Shelly", "GetConfig", &shelly.Config{}).(*shelly.Config)
	}
	if d.Config.System == nil {
		d.Config.System = sd.Call(ctx, via, "System", "GetConfig", &system.Config{}).(*system.Config)
	}
	if d.Config.Schedule == nil {
		d.Config.Schedule = sd.Call(ctx, via, "Schedule", "List", &schedule.Scheduled{}).(*schedule.Scheduled)
	}

	if d.Status == nil {
		d.Status = sd.Call(ctx, via, "Shelly", "GetStatus", &shelly.Status{}).(*shelly.Status)
	}
	if d.Status.System == nil {
		d.Status.System = sd.Call(ctx, via, "System", "GetStatus", &system.Status{}).(*system.Status)
	}

	d.MAC = sd.Info.MacAddress.String()
	d.Host = sd.Ipv4().String()
	d.Name = d.Config.System.Device.Name
	d.Manufacturer = "Shelly"
}
