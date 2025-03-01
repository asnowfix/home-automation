package myhome

import (
	"context"
	"net"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
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
	ConfigRevision uint32             `db:"config_revision" json:"config_revision"`
	Info           *shelly.DeviceInfo `db:"-" json:"info"`
	Config         *shelly.Config     `db:"-" json:"config"`
	Status         *shelly.Status     `db:"-" json:"status"`
	StatusChanged  bool               `db:"-" json:"-"`
	impl           any                `db:"-" json:"-"` // Reference to the inner implementation
	log            logr.Logger        `db:"-" json:"-"`
	// Components     *[]shelly.Component `db:"-" json:"components"`
}

func (d *Device) WithImpl(i any) *Device {
	d.impl = i
	return d
}

func (d *Device) Impl() any {
	return d.impl
}

func NewDevice(log logr.Logger, manufacturer Manufacturer, id string) *Device {
	d := &Device{}
	d.log = log
	d.Manufacturer = string(manufacturer)
	d.Id = id
	return d
}

func (d *Device) WithMAC(mac net.HardwareAddr) *Device {
	d.MAC = mac.String()
	return d
}

func (d *Device) WithHost(host string) *Device {
	d.Host = host
	return d
}

func (d *Device) WithName(name string) *Device {
	d.Name = name
	return d
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

func NewDeviceFromShellyDevice(ctx context.Context, log logr.Logger, sd *shelly.Device) (*Device, error) {
	err := sd.Init(ctx)
	if err != nil {
		log.Error(err, "Unable to initialize device")
		return nil, err
	}

	d := NewDevice(log, Shelly, sd.Id())
	d = d.WithImpl(sd)
	d = d.WithMAC(sd.Info.MacAddress)
	d = d.WithHost(sd.Host())
	// d = d.WithName(sd.Config.Sys.DeviceName)

	return d, nil
}

func (d *Device) UpdateFromShelly(ctx context.Context, sd *shelly.Device, via types.Channel) bool {
	updated := d.StatusChanged
	d.StatusChanged = false

	d.log.Info("Updating device", "device", d)
	if d.Info == nil {
		out, err := sd.CallE(ctx, via, shelly.GetDeviceInfo.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device info (giving-up)")
			return updated
		}
		d.Info = out.(*shelly.DeviceInfo)
		updated = true
	}
	// d.Manufacturer = devices.Shelly

	if d.Id == "" {
		d.Id = d.Info.Id
		updated = true
	}

	if d.MAC == "" {
		d.MAC = sd.Info.MacAddress.String()
		updated = true
	}

	// if d.Components == nil {
	// 	out, err := sd.CallE(ctx, via, shelly.GetComponents.String(), nil)
	// 	if err != nil {
	// 		d.log.Error(err, "Unable to get device's components (continuing)")
	// 	} else {
	// 		d.Components = out.(*shelly.ComponentsResponse).Components
	// 	}
	// }

	if d.Config == nil || d.Config.System.ConfigRevision != d.ConfigRevision {
		out, err := sd.CallE(ctx, via, shelly.GetConfig.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device config (continuing)")
		} else {
			d.Config = out.(*shelly.Config)
			updated = true
		}
	}

	if d.Config != nil && d.Config.System == nil {
		out, err := sd.CallE(ctx, via, system.GetConfig.String(), &system.Config{})
		if err != nil {
			d.log.Error(err, "Unable to get device system config (continuing)")
		} else {
			d.Config.System = out.(*system.Config)
			d.ConfigRevision = d.Config.System.ConfigRevision
			updated = true
		}
	}

	if d.Config != nil && d.Config.System != nil && d.Config.System.Device.Name != "" {
		d.Name = d.Config.System.Device.Name
		updated = true
	} else {
		d.Name = d.Id
	}

	if d.Status == nil {
		out, err := sd.CallE(ctx, via, shelly.GetStatus.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device status (continuing)")
		} else {
			d.Status = out.(*shelly.Status)
			updated = true
		}
	}

	if d.Status != nil && d.Status.System == nil {
		out, err := sd.CallE(ctx, via, system.GetStatus.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device system status (continuing)")
		} else {
			d.Status.System = out.(*system.Status)
			updated = true
		}
	}

	if d.Status != nil && d.Status.Wifi != nil && d.Status.Wifi.IP != "" {
		d.Host = d.Status.Wifi.IP
		updated = true
	}

	d.log.Info("Updated device", "device", d)
	return updated
}

func (d *Device) WithZeroConfEntry(entry *zeroconf.ServiceEntry) *Device {
	d.log.Info("Updating device", "id", d.Id, "zeroconf entry", entry)
	if len(entry.AddrIPv4) > 0 {
		return d.WithHost(string(entry.AddrIPv4[0]))
	} else {
		return d.WithHost(entry.HostName)
	}
}
