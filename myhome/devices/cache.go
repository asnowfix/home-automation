package devices

import (
	"context"
	"fmt"
	"global"
	"myhome"

	"github.com/go-logr/logr"
)

type Cache struct {
	log           logr.Logger
	db            DeviceRegistry
	devices       []*myhome.Device
	devicesById   map[string]*myhome.Device
	devicesByMAC  map[string]*myhome.Device
	devicesByHost map[string]*myhome.Device
	devicesByName map[string]*myhome.Device
}

func NewCache(ctx context.Context, db DeviceRegistry) *Cache {
	c := &Cache{
		log: ctx.Value(global.LogKey).(logr.Logger).WithName("Cache"),
		db:  db,
	}
	c.Flush()
	return c
}

func (c *Cache) Flush() error {
	c.devices = make([]*myhome.Device, 0)
	c.devicesById = make(map[string]*myhome.Device)
	c.devicesByMAC = make(map[string]*myhome.Device)
	c.devicesByHost = make(map[string]*myhome.Device)
	c.devicesByName = make(map[string]*myhome.Device)
	return nil
}

func (c *Cache) SetDevice(ctx context.Context, d *myhome.Device, overwrite bool) error {
	for i, existing := range c.devices {
		if existing.Id() == d.Id() || existing.MAC == d.MAC || existing.Host == d.Host || existing.Name() == d.Name() {
			if !overwrite {
				return fmt.Errorf("device already exists: %v", *d)
			}
			c.devices = append(c.devices[:i], c.devices[i+1:]...)
			break
		}
	}
	d, err := c.insert(d)
	if err != nil {
		return err
	}
	return c.db.SetDevice(ctx, d, overwrite)
}

func (c *Cache) insert(d *myhome.Device) (*myhome.Device, error) {
	c.devices = append(c.devices, d)
	c.devicesById[d.Id()] = d
	c.devicesByMAC[d.MAC] = d
	c.devicesByHost[d.Host] = d
	c.devicesByName[d.Name()] = d
	c.log.Info("inserted/updated device", "id", d.Id(), "name", d.Name())
	return d, nil
}

func (c *Cache) GetDeviceByAny(ctx context.Context, any string) (*myhome.Device, error) {
	var exists bool
	var d *myhome.Device
	d, exists = c.devicesById[any]
	if exists {
		return d, nil
	}
	d, exists = c.devicesByMAC[any]
	if exists {
		return d, nil
	}
	d, exists = c.devicesByHost[any]
	if exists {
		return d, nil
	}
	d, exists = c.devicesByName[any]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceByAny(ctx, any)
}

func (c *Cache) GetDeviceById(ctx context.Context, id string) (*myhome.Device, error) {
	var exists bool
	var d *myhome.Device
	d, exists = c.devicesById[id]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceById(ctx, id)
}

func (c *Cache) GetDeviceByMAC(ctx context.Context, mac string) (*myhome.Device, error) {
	var exists bool
	var d *myhome.Device
	d, exists = c.devicesByMAC[mac]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceByMAC(ctx, mac)
}

func (c *Cache) GetDeviceByHost(ctx context.Context, host string) (*myhome.Device, error) {
	var exists bool
	var d *myhome.Device
	d, exists = c.devicesByHost[host]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceByHost(ctx, host)
}

func (c *Cache) GetDeviceByName(ctx context.Context, name string) (*myhome.Device, error) {
	var exists bool
	var d *myhome.Device
	d, exists = c.devicesByName[name]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceByName(ctx, name)
}

func (c *Cache) GetAllDevices(ctx context.Context) ([]*myhome.Device, error) {
	// TODO: use cache content
	return c.db.GetAllDevices(ctx)
}

func (c *Cache) GetDevicesMatchingAny(ctx context.Context, name string) ([]*myhome.Device, error) {
	// TODO: use cache content
	return c.db.GetDevicesMatchingAny(ctx, name)
}

func (c *Cache) DeleteDevice(ctx context.Context, id string) error {
	// TODO: use cache content
	return c.db.DeleteDevice(ctx, id)
}
