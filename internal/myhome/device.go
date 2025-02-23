package myhome

import (
	"context"
	"fmt"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"

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

type GroupInfo struct {
	ID          int    `db:"id" json:"-"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description"`
}

type Groups struct {
	Groups []GroupInfo `json:"groups"`
}

type Group struct {
	GroupInfo
	Devices []DeviceSummary `json:"devices"`
}

type GroupDevice struct {
	Manufacturer string `db:"manufacturer" json:"manufacturer"`
	ID           string `db:"id" json:"id"`
	Group        string `db:"group" json:"group"`
}

func UpdateDeviceFromShelly(ctx context.Context, log logr.Logger, d *Device, sd *shelly.Device, via types.Channel) {
	log.Info("Updating device", "device", d)
	if d.Info == nil {
		out, err := sd.CallE(ctx, via, shelly.GetDeviceInfo.String(), nil)
		if err != nil {
			log.Error(err, "Unable to get device info (giving-up)")
			return
		}
		d.Info = out.(*shelly.DeviceInfo)
	}
	d.Manufacturer = "Shelly"
	d.Id = d.Info.Id
	d.MAC = sd.Info.MacAddress.String()

	if d.Components == nil {
		out, err := sd.CallE(ctx, via, shelly.GetComponents.String(), nil)
		if err != nil {
			log.Error(err, "Unable to get device's components (continuing)")
		} else {
			d.Components = out.(*shelly.ComponentsResponse).Components
		}
	}

	if d.Config == nil {
		out, err := sd.CallE(ctx, via, shelly.GetConfig.String(), nil)
		if err != nil {
			log.Error(err, "Unable to get device config (continuing)")
		} else {
			d.Config = out.(*shelly.Config)
		}
	}

	if d.Config != nil && d.Config.System == nil {
		out, err := sd.CallE(ctx, via, system.GetConfig.String(), &system.Config{})
		if err != nil {
			log.Error(err, "Unable to get device system config (continuing)")
		} else {
			d.Config.System = out.(*system.Config)
		}
	}

	if d.Config != nil && d.Config.System != nil && d.Config.System.Device.Name != "" {
		d.Name = d.Config.System.Device.Name
	} else {
		d.Name = d.Id
	}

	if d.Status == nil {
		out, err := sd.CallE(ctx, via, shelly.GetStatus.String(), nil)
		if err != nil {
			log.Error(err, "Unable to get device status (continuing)")
		} else {
			d.Status = out.(*shelly.Status)
		}
	}

	if d.Status != nil && d.Status.System == nil {
		out, err := sd.CallE(ctx, via, system.GetStatus.String(), &system.Status{})
		if err != nil {
			log.Error(err, "Unable to get device system status (continuing)")
		} else {
			d.Status.System = out.(*system.Status)
		}
	}

	if d.Status != nil && d.Status.Wifi != nil && d.Status.Wifi.IP != "" {
		d.Host = d.Status.Wifi.IP
	} else {
		d.Host = fmt.Sprintf("%s.local.", d.Id)
	}

	log.Info("Updated device", "device", d)
}
