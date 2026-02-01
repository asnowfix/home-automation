package devices

import (
	"context"
	"myhome"
)

type DeviceRegistry interface {
	Flush() error
	SetDevice(ctx context.Context, d *myhome.Device, overwrite bool) error
	GetDevicesMatchingAny(ctx context.Context, name string) ([]*myhome.Device, error)
	GetDeviceByAny(ctx context.Context, identifier string) (*myhome.Device, error)
	GetDeviceById(ctx context.Context, id string) (*myhome.Device, error)
	GetDeviceByHost(ctx context.Context, host string) (*myhome.Device, error)
	GetDeviceByMAC(ctx context.Context, mac string) (*myhome.Device, error)
	GetDeviceByName(ctx context.Context, name string) (*myhome.Device, error)
	ForgetDevice(ctx context.Context, id string) error
	GetAllDevices(ctx context.Context) ([]*myhome.Device, error)
	SetDeviceRoom(ctx context.Context, identifier string, roomId string) error
	GetDevicesByRoom(ctx context.Context, roomId string) ([]*myhome.Device, error)
}

type Manager interface {
	UpdateChannel() chan<- *myhome.Device
}
