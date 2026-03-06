package devices

import (
	"context"
	"fmt"
	"myhome"
	"strconv"
	"sync"

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
	mutex         sync.Mutex
}

func NewCache(ctx context.Context, db DeviceRegistry) *Cache {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}

	c := &Cache{
		log: log.WithName("Cache"),
		db:  db,
	}
	c.Flush()
	return c
}

func (c *Cache) Flush() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.devices = make([]*myhome.Device, 0)
	c.devicesById = make(map[string]*myhome.Device)
	c.devicesByMAC = make(map[string]*myhome.Device)
	c.devicesByHost = make(map[string]*myhome.Device)
	c.devicesByName = make(map[string]*myhome.Device)
	return nil
}

// Load pre-populates the cache with all devices from the database
// This should be called on startup before MQTT listeners start, so that
// retained sensor messages can update devices that already exist in cache
func (c *Cache) Load(ctx context.Context) error {
	c.log.Info("Pre-populating cache from database")

	devices, err := c.db.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("failed to load devices from database: %w", err)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, d := range devices {
		c.devices = append(c.devices, d)
		c.devicesById[d.Id()] = d
		c.devicesByMAC[d.MAC] = d
		c.devicesByHost[d.Host_] = d
		c.devicesByName[d.Name()] = d
	}

	c.log.Info("Cache pre-populated", "device_count", len(devices))
	return nil
}

func (c *Cache) SetDevice(ctx context.Context, d *myhome.Device, overwrite bool) (bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for i, existing := range c.devices {
		if existing.Id() == d.Id() || existing.MAC == d.MAC || existing.Host_ == d.Host_ || existing.Name() == d.Name() {
			if !overwrite {
				return false, fmt.Errorf("device already exists: %v", *d)
			}
			c.devices = append(c.devices[:i], c.devices[i+1:]...)
			break
		}
	}
	d, err := c.insert(d)
	if err != nil {
		return true, err
	}
	return c.db.SetDevice(ctx, d, overwrite)
}

func (c *Cache) insert(d *myhome.Device) (*myhome.Device, error) {
	// No need to lock here as this is only called from SetDevice which already has the lock
	c.devices = append(c.devices, d)
	c.devicesById[d.Id()] = d
	c.devicesByMAC[d.MAC] = d
	c.devicesByHost[d.Host_] = d
	c.devicesByName[d.Name()] = d
	c.log.Info("inserted/updated device", "id", d.Id(), "name", d.Name())
	return d, nil
}

func (c *Cache) GetDeviceByAny(ctx context.Context, any string) (*myhome.Device, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

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
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var exists bool
	var d *myhome.Device
	d, exists = c.devicesById[id]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceById(ctx, id)
}

func (c *Cache) GetDeviceByMAC(ctx context.Context, mac string) (*myhome.Device, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var exists bool
	var d *myhome.Device
	d, exists = c.devicesByMAC[mac]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceByMAC(ctx, mac)
}

func (c *Cache) GetDeviceByHost(ctx context.Context, host string) (*myhome.Device, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var exists bool
	var d *myhome.Device
	d, exists = c.devicesByHost[host]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceByHost(ctx, host)
}

func (c *Cache) GetDeviceByName(ctx context.Context, name string) (*myhome.Device, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var exists bool
	var d *myhome.Device
	d, exists = c.devicesByName[name]
	if exists {
		return d, nil
	}
	return c.db.GetDeviceByName(ctx, name)
}

func (c *Cache) GetAllDevices(ctx context.Context) ([]*myhome.Device, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// TODO: use cache content
	return c.db.GetAllDevices(ctx)
}

func (c *Cache) GetDevicesMatchingAny(ctx context.Context, name string) ([]*myhome.Device, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// TODO: use cache content
	return c.db.GetDevicesMatchingAny(ctx, name)
}

func (c *Cache) ForgetDevice(ctx context.Context, id string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// TODO: use cache content
	return c.db.ForgetDevice(ctx, id)
}

func (c *Cache) SetDeviceRoom(ctx context.Context, identifier string, roomId string) (bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Update cache if device exists
	if d, exists := c.devicesById[identifier]; exists {
		d.RoomId = roomId
	} else if d, exists := c.devicesByName[identifier]; exists {
		d.RoomId = roomId
	} else if d, exists := c.devicesByMAC[identifier]; exists {
		d.RoomId = roomId
	} else if d, exists := c.devicesByHost[identifier]; exists {
		d.RoomId = roomId
	}

	return c.db.SetDeviceRoom(ctx, identifier, roomId)
}

func (c *Cache) GetDevicesByRoom(ctx context.Context, roomId string) ([]*myhome.Device, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// TODO: use cache content
	return c.db.GetDevicesByRoom(ctx, roomId)
}

// sensorFieldMap maps sensor names to their corresponding field pointers and types
var sensorFieldMap = map[string]struct {
	field interface{}
	isInt bool
}{
	"temperature":  {field: func(s *myhome.Sensors) **float64 { return &s.Temperature }, isInt: false},
	"humidity":     {field: func(s *myhome.Sensors) **float64 { return &s.Humidity }, isInt: false},
	"pressure":     {field: func(s *myhome.Sensors) **float64 { return &s.Pressure }, isInt: false},
	"illuminance":  {field: func(s *myhome.Sensors) **float64 { return &s.Illuminance }, isInt: false},
	"mass":         {field: func(s *myhome.Sensors) **float64 { return &s.Mass }, isInt: false},
	"dew_point":    {field: func(s *myhome.Sensors) **float64 { return &s.DewPoint }, isInt: false},
	"energy":       {field: func(s *myhome.Sensors) **float64 { return &s.Energy }, isInt: false},
	"power":        {field: func(s *myhome.Sensors) **float64 { return &s.Power }, isInt: false},
	"voltage":      {field: func(s *myhome.Sensors) **float64 { return &s.Voltage }, isInt: false},
	"current":      {field: func(s *myhome.Sensors) **float64 { return &s.Current }, isInt: false},
	"rotation":     {field: func(s *myhome.Sensors) **float64 { return &s.Rotation }, isInt: false},
	"distance_m":   {field: func(s *myhome.Sensors) **float64 { return &s.DistanceM }, isInt: false},
	"acceleration": {field: func(s *myhome.Sensors) **float64 { return &s.Acceleration }, isInt: false},
	"battery":      {field: func(s *myhome.Sensors) **int { return &s.Battery }, isInt: true},
	"motion":       {field: func(s *myhome.Sensors) **int { return &s.Motion }, isInt: true},
	"window":       {field: func(s *myhome.Sensors) **int { return &s.Window }, isInt: true},
	"button":       {field: func(s *myhome.Sensors) **int { return &s.Button }, isInt: true},
	"distance_mm":  {field: func(s *myhome.Sensors) **int { return &s.DistanceMM }, isInt: true},
	"timestamp":    {field: func(s *myhome.Sensors) **int { return &s.Timestamp }, isInt: true},
}

// UpdateSensorValue updates a sensor value in the cached device
// This does NOT persist to database - sensor values are ephemeral
func (c *Cache) UpdateSensorValue(ctx context.Context, deviceID string, sensor string, value string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	device, exists := c.devicesById[deviceID]
	if !exists {
		return fmt.Errorf("device not found in cache: %s", deviceID)
	}

	// Initialize Status and Sensors if needed
	if device.Status == nil {
		device.Status = &myhome.Status{}
	}
	if device.Status.Sensors == nil {
		device.Status.Sensors = &myhome.Sensors{}
	}

	// Look up sensor field mapping
	mapping, exists := sensorFieldMap[sensor]
	if !exists {
		c.log.V(1).Info("Unknown sensor type", "sensor", sensor, "value", value)
		return fmt.Errorf("unknown sensor type: %s", sensor)
	}

	// Parse and assign value based on type
	if mapping.isInt {
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid %s value: %s", sensor, value)
		}
		fieldPtr := mapping.field.(func(*myhome.Sensors) **int)(device.Status.Sensors)
		*fieldPtr = &v
	} else {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid %s value: %s", sensor, value)
		}
		fieldPtr := mapping.field.(func(*myhome.Sensors) **float64)(device.Status.Sensors)
		*fieldPtr = &v
	}

	c.log.V(1).Info("Updated sensor value in cache", "device_id", deviceID, "sensor", sensor, "value", value)
	return nil
}
