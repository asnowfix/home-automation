package myhome

import (
	"context"
	"encoding/json"
	"net"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

type DeviceIdentifier struct {
	// The manufacturer of the device
	Manufacturer string `db:"manufacturer" json:"manufacturer"`
	// The unique identifier of the device, defined by the manufacturer
	Id_ string `db:"id" json:"id"`
}

type DeviceSummary struct {
	DeviceIdentifier
	MAC   string `db:"mac" json:"mac,omitempty"` // The Ethernet hardware address of the device, globally unique & assigned by the manufacturer
	Host_ string `db:"host" json:"host"`         // The host address of the device (Host address or resolvable hostname), assigned on this network
	Name_ string `db:"name" json:"name"`         // The local unique name of the device, defined by the user
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
	ConfigRevision uint32               `db:"config_revision" json:"config_revision"`
	Info           *shelly.DeviceInfo   `db:"-" json:"info"`
	Config         *shelly.Config       `db:"-" json:"config"`
	impl           any                  `db:"-" json:"-"` // Reference to the inner implementation
	log            logr.Logger          `db:"-" json:"-"`
	components     map[string]Component `db:"-" json:"-"`
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
	d.Manufacturer = string(manufacturer)
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
	d := NewDevice(log, Shelly, device.Id())
	d = d.WithImpl(device)
	d = d.WithMAC(device.Mac())
	d = d.WithHost(device.Host())
	d = d.WithName(device.Name())

	return d, nil
}

func (d *Device) UpdateFromShelly(ctx context.Context, sd *shelly.Device, via types.Channel) bool {
	updated := false

	if d.Id() == "" || d.Mac().String() == "" || d.Info == nil {
		out, err := sd.CallE(ctx, via, shelly.GetDeviceInfo.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device info (giving-up)")
			return updated
		}
		info, ok := out.(*shelly.DeviceInfo)
		if !ok {
			d.log.Error(err, "Invalid response to get device info (giving-up)", "response", out)
			return updated
		}
		if info.Id == "" || len(info.MacAddress) == 0 {
			d.log.Error(err, "Invalid response to get device info (giving-up)", "info", *info)
			return updated
		}

		d.Info = info
		d = d.WithId(info.Id).WithMAC(info.MacAddress)
		updated = true
	}

	if d.components == nil {
		out, err := sd.CallE(ctx, via, shelly.GetComponents.String(), &shelly.ComponentsRequest{
			Keys: []string{"config", "status"},
		})
		if err != nil {
			d.log.Error(err, "Unable to get device's components (continuing)")
		} else {
			crs, ok := out.(*shelly.ComponentsResponse)
			if ok && crs != nil {
				updated = true
			} else {
				d.log.Error(err, "Invalid response to get device's components (continuing)", "response", out)
			}
		}
	}

	if d.Config == nil {
		out, err := sd.CallE(ctx, via, shelly.GetConfig.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device config (continuing)")
		} else {
			c, ok := out.(*shelly.Config)
			if ok && c != nil {
				d.Config = c
				updated = true
			} else {
				d.log.Error(err, "Invalid response to get device config (continuing)", "response", out)
			}
		}
	}

	if d.ConfigRevision == 0 || d.Name() == "" {
		out, err := sd.CallE(ctx, via, system.GetConfig.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device system config (continuing)")
		} else {
			sc, ok := out.(*system.Config)
			if ok && sc != nil && sc.Device != nil {
				d.Name_ = sc.Device.Name
				d.ConfigRevision = sc.ConfigRevision
				// d.SetComponentStatus("system", nil, *sc) FIXME
				updated = true
			} else {
				d.log.Error(err, "Invalid response to get device system config (continuing)", "response", out)
			}
		}
	}

	if d.Host_ == "" {
		out, err := sd.CallE(ctx, via, wifi.GetStatus.String(), nil)
		if err != nil {
			d.log.Error(err, "Unable to get device wifi status (continuing)")
		} else {
			ws, ok := out.(*wifi.Status)
			if ok && ws != nil && ws.IP != "" {
				d.Host_ = ws.IP
				d.log.Error(err, "Invalid response to get device wifi status (continuing)", "response", out)
				updated = true
			} else {
				d.log.Error(err, "Invalid response to get device wifi status (continuing)", "response", out)
			}
		}
	}

	d.log.Info("Device update", "device", d, "updated", updated)
	return updated
}

func (d *Device) WithZeroConfEntry(entry *zeroconf.ServiceEntry) *Device {
	d.log.Info("Updating device", "id", d.Id, "zeroconf entry", entry)
	if len(entry.AddrIPv4) > 0 {
		return d.WithHost(entry.AddrIPv4[0].String())
	} else {
		return d.WithHost(entry.HostName)
	}
}
