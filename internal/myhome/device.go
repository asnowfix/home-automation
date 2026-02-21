package myhome

import (
	"context"
	"fmt"
	"myhome/sfr"
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
	RoomId         string             `db:"room_id" json:"room_id,omitempty"` // Room this device belongs to (optional, max one room)
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

		// Let the Shelly device's Refresh() handle communication via MQTT if no IP is available
		// Don't try to resolve via mDNS here as it may timeout and block MQTT communication
		modified, err = sd.Refresh(ctx, types.ChannelDefault)
		if err != nil {
			d.log.Error(err, "Failed to update device", "device", d.DeviceSummary)
			return false, err
		}

		// Always copy config and info to myhome device, even if not modified
		// This ensures they're available for saving to database
		d.WithId(sd.Id())
		d.WithHost(sd.Host())
		d.WithName(sd.Name())
		d.WithMAC(sd.Mac())
		d.ConfigRevision = sd.ConfigRevision()
		d.Info = sd.Info()
		d.Config = sd.Config()

		if !modified {
			d.log.Info("Device is up to date", "device", d.DeviceSummary)
			sd.ResetModified()
			return false, nil
		}

		sd.ResetModified()
	}

	return true, nil
}

func (d *Device) WithZeroConfEntry(ctx context.Context, entry *zeroconf.ServiceEntry) *Device {
	d.log.Info("Updating device", "id", d.Id, "zeroconf entry", entry)

	if len(entry.AddrIPv4) > 0 {
		d.WithHost(entry.AddrIPv4[0].String())
	} else if len(entry.AddrIPv6) > 0 {
		d.WithHost(entry.AddrIPv6[0].String())
	}

	if entry.Instance != "" && entry.Instance != d.Id() {
		d.WithName(entry.Instance)
	}

	ip := net.ParseIP(d.Host())
	if ip != nil {
		host, err := sfr.GetRouter(ctx).GetHostByIp(ctx, ip)
		if err != nil {
			d.log.Error(err, "Failed to get host by IP", "ip", d.Host())
			return d
		}
		d.WithMAC(host.Mac())
	}

	return d
}

func (d *Device) Switches(ctx context.Context) (*SwitchAllResult, error) {
	sd, ok := d.Impl().(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly device: %T", d.Impl())
	}
	switches, err := shelly.GetSwitchesSummary(ctx, sd)
	if err != nil {
		d.log.Error(err, "Failed to get switches summary")
		return nil, err
	}
	res := SwitchAllResult{
		DeviceID:   d.Id(),
		DeviceName: d.Name(),
		Switches:   switches,
	}
	return &res, nil
}

// DeviceShowParams represents parameters for device.show RPC
type DeviceShowParams struct {
	Identifier string `json:"identifier"` // Device identifier (id/name/host/MAC/IP)
}

// DeviceSetupParams represents parameters for device.setup RPC
type DeviceSetupParams struct {
	Identifier string `json:"identifier"`            // Device identifier (id/name/host/MAC/IP)
	Name       string `json:"name,omitempty"`        // Device name (overrides auto-derivation)
	StaEssid   string `json:"sta_essid,omitempty"`   // WiFi STA ESSID
	StaPasswd  string `json:"sta_passwd,omitempty"`  // WiFi STA password
	Sta1Essid  string `json:"sta1_essid,omitempty"`  // WiFi STA1 ESSID
	Sta1Passwd string `json:"sta1_passwd,omitempty"` // WiFi STA1 password
	ApPasswd   string `json:"ap_passwd,omitempty"`   // WiFi AP password
}

// DeviceSetRoomParams represents parameters for device.setroom RPC
type DeviceSetRoomParams struct {
	Identifier string `json:"identifier"` // Device identifier (id/name/host/MAC/IP)
	RoomId     string `json:"room_id"`    // Room ID to assign (empty string to clear)
}

// DeviceListByRoomParams represents parameters for device.listbyroom RPC
type DeviceListByRoomParams struct {
	RoomId string `json:"room_id"` // Room ID to list devices for
}

// DeviceListByRoomResult represents the result of device.listbyroom RPC
type DeviceListByRoomResult struct {
	Devices []*Device `json:"devices"`
}
