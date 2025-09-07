package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"mynet"
	"net"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/shelly"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

type DeviceIdentifier struct {
	// The manufacturer of the device
	Manufacturer_ string `db:"manufacturer" json:"manufacturer"`
	// The unique identifier of the device, defined by the manufacturer
	Id_ string `db:"id" json:"id"`
}

type DeviceSummary struct {
	DeviceIdentifier
	MAC   string `db:"mac" json:"mac,omitempty"` // The Ethernet hardware address of the device, globally unique & assigned by the manufacturer
	Host_ string `db:"host" json:"host"`         // The host address of the device (Host address or resolvable hostname), assigned on this network
	Name_ string `db:"name" json:"name"`         // The local unique name of the device, defined by the user
}

func (d DeviceSummary) Manufacturer() string {
	return d.Manufacturer_
}

func (d DeviceSummary) Id() string {
	return d.Id_
}

func (d DeviceSummary) Name() string {
	return d.Name_
}

func (d DeviceSummary) Ip() net.IP {
	return net.ParseIP(d.Host_)
}

func (d DeviceSummary) Host() string {
	return d.Host_
}

func (d DeviceSummary) Mac() net.HardwareAddr {
	mac, err := net.ParseMAC(d.MAC)
	if err != nil {
		return nil
	}
	return mac
}

// func (d DeviceSummary) MarshalJSON() ([]byte, error) {
// 	type MarshalledHost struct {
// 		Host string `json:"host"`
// 	}
// 	return json.Marshal(struct {
// 		DeviceIdentifier
// 		Host MarshalledHost `json:"host"`
// 	})
// }

type Device struct {
	DeviceSummary
	ConfigRevision uint32             `db:"config_revision" json:"config_revision"`
	Info           *shelly.DeviceInfo `db:"-" json:"info"`
	Config         *shelly.Config     `db:"-" json:"config"`
	impl           any                `db:"-" json:"-"` // Reference to the inner implementation
	log            logr.Logger        `db:"-" json:"-"`
}

type Component struct {
	Config map[string]any `json:"config"`
	Status map[string]any `json:"status"`
}

type DeviceImplementation interface {
	Info() *shelly.DeviceInfo
	Config() *shelly.Config
	Status() *shelly.Status
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
	d.Manufacturer_ = string(manufacturer)
	d.Id_ = id
	return d
}

func (d *Device) WithId(id string) *Device {
	d.Id_ = id
	return d
}

func (d *Device) WithMAC(mac net.HardwareAddr) *Device {
	d.MAC = mac.String()
	return d
}

func (d *Device) WithHost(host string) *Device {
	d.Host_ = host
	return d
}

func (d *Device) WithName(name string) *Device {
	d.Name_ = name
	return d
}

func (d *Device) Update(status any) {
	// TODO: update status & save
}

type GroupInfo struct {
	ID   int               `db:"id" json:"id"`
	Name string            `db:"name" json:"name"`
	KVS  string            `db:"kvs" json:"kvs"`
	kvs  map[string]string `db:"-" json:"-"`
}

func (g *GroupInfo) WithKeyValue(key, value string) *GroupInfo {
	if len(key) == 0 {
		return g
	}
	if len(g.kvs) == 0 {
		g.kvs = make(map[string]string)
		json.Unmarshal([]byte(g.KVS), &g.kvs)
	}
	if len(value) == 0 {
		delete(g.kvs, key)
	} else {
		g.kvs[key] = value
	}
	buf, err := json.Marshal(g.kvs)
	if err == nil {
		g.KVS = string(buf)
	}
	return g
}

func (g *GroupInfo) KeyValues() map[string]string {
	if len(g.kvs) == 0 {
		g.kvs = make(map[string]string)
		json.Unmarshal([]byte(g.KVS), &g.kvs)
	}
	return g.kvs
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
	Id           string `db:"id" json:"id"`
	Group        string `db:"group" json:"group"`
}

func NewDeviceFromImpl(ctx context.Context, log logr.Logger, device devices.Device) (*Device, error) {
	d := NewDevice(log, SHELLY, device.Id())
	d = d.WithImpl(device)
	d = d.WithMAC(device.Mac())
	d = d.WithHost(device.Host())
	d = d.WithName(device.Name())

	return d, nil
}

func (d *Device) Refresh(ctx context.Context) (bool, error) {
	d.log.Info("Refreshing device", "id", d.Id(), "name", d.Name())

	var modified bool = false
	var err error

	switch d.Manufacturer() {
	case string(SHELLY):
		sd, ok := d.Impl().(*shellyapi.Device)
		if !ok || sd == nil {
			var err error
			device, err := shellyapi.NewDeviceFromSummary(ctx, d.log, d)
			if err != nil {
				d.log.Error(err, "Failed to create device from summary", "device", d.DeviceSummary)
				return false, err
			}
			sd, ok = device.(*shellyapi.Device)
			if !ok {
				return false, fmt.Errorf("device is not a Shelly device: %T", device)
			}
			d.WithImpl(sd)
		}

		// //  TODO: Load KVS revision from device & compare it with the one in the DB. If they are different, update the device in the DB.
		// status := system.GetStatus(ctx, sd)

		// kvsRevision, err := kvs.GetRevision(ctx, sd)
		// if err != nil {
		// 	dm.log.Error(err, "Failed to get device KVS revision", "id", device.Id(), "name", device.Name())
		// 	continue
		// }

		if sd.Ip() == nil && sd.Id() != "" && sd.Name() != "" {
			ip, err := mynet.MyResolver(d.log).LookupHost(ctx, sd.Name())
			if err != nil {
				d.log.Error(err, "Failed to resolve device host", "device", d.DeviceSummary)
				return false, err
			}
			sd.Host_ = ip[0]
			modified = true
		}

		modified, err = sd.Refresh(ctx, types.ChannelDefault)
		if err != nil {
			d.log.Error(err, "Failed to update device", "device", d.DeviceSummary)
			return false, err
		}
		if !modified {
			d.log.Info("Device is up to date", "device", d.DeviceSummary)
			return false, nil
		}
		d.DeviceSummary.Id_ = sd.Id()
		d.DeviceSummary.Host_ = sd.Host()
		d.DeviceSummary.Name_ = sd.Name()
		d.DeviceSummary.MAC = sd.Mac().String()
		d.ConfigRevision = sd.ConfigRevision()
		d.Info = sd.Info()
		d.Config = sd.Config()
		sd.ResetModified()
	}

	return true, nil
}

func (d *Device) WithZeroConfEntry(entry *zeroconf.ServiceEntry) *Device {
	d.log.Info("Updating device", "id", d.Id, "zeroconf entry", entry)
	if len(entry.AddrIPv4) > 0 {
		return d.WithHost(entry.AddrIPv4[0].String())
	} else {
		return d.WithHost(entry.HostName)
	}
}
