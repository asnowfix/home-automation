package devices

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome"
	"myhome/storage"
	"mymqtt"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/mqtt"
	"pkg/shelly/types"
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const (
	Shelly = "Shelly"
)

type DeviceManager struct {
	storage       *storage.DeviceStorage
	mu            sync.Mutex
	update        chan *Device
	cancel        context.CancelFunc
	log           logr.Logger
	mqttClient    *mymqtt.Client
	devicesById   map[string]*Device
	devicesByMAC  map[string]*Device
	devicesByHost map[string]*Device
}

func NewDeviceManager(log logr.Logger, storage *storage.DeviceStorage, mqttClient *mymqtt.Client) *DeviceManager {
	return &DeviceManager{
		storage:       storage,
		log:           log.WithName("DeviceManager"),
		update:        make(chan *Device, 1),
		devicesById:   make(map[string]*Device),
		devicesByMAC:  make(map[string]*Device),
		devicesByHost: make(map[string]*Device),
		mqttClient:    mqttClient,
	}
}

func (dm *DeviceManager) Start(ctx context.Context) error {
	var err error

	dm.log.Info("Starting device manager")

	myhome.RegisterMethodHandler("device.list", func(in any) (any, error) {
		devices := myhome.Devices{
			Devices: make([]myhome.DeviceSummary, 0),
		}
		ds, err := dm.storage.GetAllDevices()
		if err != nil {
			log.Error(err, "Failed to get all devices")
			return nil, err
		}
		log.Info("Found devices", "num_devices", len(ds))
		for _, d := range ds {
			devices.Devices = append(devices.Devices, d.DeviceSummary)
		}
		return &devices, nil
	})
	myhome.RegisterMethodHandler(myhome.DeviceShow, func(in any) (any, error) {
		return dm.storage.GetDeviceByIdentifier(in.(string))
	})
	myhome.RegisterMethodHandler(myhome.GroupList, func(in any) (any, error) {
		return dm.storage.GetAllGroups()
	})
	myhome.RegisterMethodHandler(myhome.GroupCreate, func(in any) (any, error) {
		return dm.storage.AddGroup(in.(*myhome.Group))
	})
	myhome.RegisterMethodHandler(myhome.GroupDelete, func(in any) (any, error) {
		return dm.storage.RemoveGroup(in.(string))
	})
	myhome.RegisterMethodHandler(myhome.GroupListDevices, func(in any) (any, error) {
		return dm.storage.GetDevicesByGroupName(in.(string))
	})
	myhome.RegisterMethodHandler(myhome.GroupAddDevice, func(in any) (any, error) {
		return dm.storage.AddDeviceToGroup(in.(myhome.GroupDevice))
	})
	myhome.RegisterMethodHandler(myhome.GroupRemoveDevice, func(in any) (any, error) {
		return dm.storage.RemoveDeviceFromGroup(in.(myhome.GroupDevice))
	})

	ctx, dm.cancel = context.WithCancel(ctx)
	go func(ctx context.Context, log logr.Logger, dc <-chan *Device) error {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled")
				return ctx.Err()

			case device := <-dc:
				sd, ok := device.impl.(*shelly.Device)
				if ok {
					UpdateDeviceFromShelly(ctx, log, device, sd, types.ChannelMqtt)
				} else {
					log.Error(nil, "Unhandled device type", "device", device, "type", reflect.TypeOf(device.impl))
					continue
				}
				err = dm.storage.UpsertDevice(device.Device)
				if err != nil {
					log.Error(err, "Failed to upsert device", "device", device)
					continue
				} else {
					log.Info("Updated device", "device", device)
				}

				dm.devicesById[device.Id] = device
				dm.devicesByHost[device.Host] = device
				dm.devicesByMAC[device.MAC] = device
			}
		}
	}(ctx, log.WithName("DeviceManager#DeviceChannel"), dm.update)

	// // Load every devices from storage & init them
	// devices, err := dm.storage.GetAllDevices()
	// if err != nil {
	// 	log.Error(err, "Failed to get all devices")
	// 	return err
	// }
	// log.Info("Loaded", "devices", len(devices))
	// for _, device := range devices {
	// 	log.Info("Background update of device", "id", device.Id)
	// 	go func(log logr.Logger, device myhome.Device) {
	// 		var d Device = Device{
	// 			Device: device,
	// 		}
	// 		log.Info("Scheduling update of device", "id", device.Id)
	// 		dm.update <- d.WithImpl(shelly.NewMqttDevice(dm.log.WithName(device.Id), device.Id, dm.mqttClient))
	// 	}(log.WithName(device.Id), device)
	// }

	// Loop on MQTT event devices discovery
	err = dm.WatchMqtt(ctx, dm.mqttClient)
	if err != nil {
		dm.log.Error(err, "Failed to watch MQTT events")
		return err
	}

	// // Loop on ZeroConf devices discovery
	// err = dm.WatchZeroConf(ctx)
	// if err != nil {
	// 	dm.log.Error(err, "Failed to watch ZeroConf devices")
	// 	return err
	// }

	// Loop on ZeroConf devices discovery
	// dm.DiscoverDevices(shelly.MDNS_SHELLIES, 300*time.Second, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.DeviceIdentifier, error) {
	// 	log.Info("Identifying", "entry", entry)
	// 	return &devices.DeviceIdentifier{
	// 		Manufacturer: "Shelly",
	// 		ID:           entry.Instance,
	// 	}, nil
	// }, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.Device, error) {
	// 	sd, err := shelly.NewDeviceFromZeroConfEntry(log, entry)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	log.Info("Got", "shelly_device", sd)
	// 	return &devices.Device{
	// 		DeviceIdentifier: devices.DeviceIdentifier{
	// 			Manufacturer: "Shelly",
	// 			ID:           sd.Id_,
	// 		},
	// 		MAC:  sd.MacAddress,
	// 		Host: sd.Ipv4_.String(),
	// 	}, nil
	// })

	return nil
}

func (dm *DeviceManager) Shutdown() {
	dm.log.Info("Shutting down device manager")
	if dm.cancel != nil {
		dm.cancel()
		dm.cancel = nil
	}
	dm.storage.Close()
}

func (dm *DeviceManager) WatchMqtt(ctx context.Context, mc *mymqtt.Client) error {
	var sd *shelly.Device

	topic := "+/events/rpc"
	ch, err := mc.Subscriber(ctx, topic, 16)
	if err != nil {
		dm.log.Error(err, "Failed to subscribe to shelly devices events")
		return err
	}

	go func(ctx context.Context, log logr.Logger) error {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled", "topic", topic)
				return ctx.Err()

			case msg := <-ch:
				log.Info("Received message", "topic", topic, "payload", string(msg))
				event := &mqtt.Event{}
				err := json.Unmarshal(msg, &event)
				if err != nil {
					log.Error(err, "Failed to unmarshal event from payload", "payload", string(msg))
					continue
				}
				if event.Src[:6] != "shelly" {
					log.Info("Skipping non-shelly event", "event", event)
					continue
				}
				deviceId := event.Src
				mhd, err := dm.storage.GetDeviceByManufacturerAndID("Shelly", deviceId)
				var device Device = Device{
					Device: mhd,
				}
				if err != nil {
					log.Info("Device not found, creating new one", "device_id", deviceId)
					device = *NewDevice("Shelly", deviceId)
					sd = shelly.NewDeviceFromMqttId(ctx, dm.log, deviceId, mc)
					device.MAC = sd.MacAddress.String()
					device.Host = sd.Ipv4().String()
					UpdateDeviceFromShelly(ctx, dm.log, &device, sd, types.ChannelMqtt)
					// device.Name = device.Config.System.Device.Name
					device.impl = sd
				}

				// if device.Config == nil {
				// 	sd, ok := device.impl.(*shelly.Device)
				// 	if !ok {
				// 		dm.log.Info("Device is not a shelly device", "device_id", event.Src)
				// 	} else {
				// 		out, err := sd.CallE(ctx, types.ChannelMqtt, "Shelly", "GetConfig", nil)
				// 		if err != nil {
				// 			dm.log.Error(err, "Failed to get shelly config", "device_id", event.Src)
				// 		} else {
				// 			config, ok := out.(*shelly.Config)
				// 			if ok {
				// 				device.Config = config
				// 			} else {
				// 				dm.log.Info("discarding incomplete or unknown shelly config", "out", out)
				// 			}
				// 		}
				// 	}
				// }

				dm.log.Info("Updating device", "device", device)
				err = device.UpdateFromMqttEvent(event)
				if err != nil {
					dm.log.Error(err, "Failed to update device from MQTT event", "device_id", event.Src)
					continue
				}
				dm.log.Info("Storing updated device", "device", device)
				dm.storage.UpsertDevice(device.Device)
			}
		}
	}(ctx, log.WithName("DeviceManager#WatchMqtt"))

	return nil
}

func (dm *DeviceManager) WatchZeroConf(ctx context.Context) error {

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		dm.log.Error(err, "Failed to initialize ZeroConf resolver")
		return err
	}
	dm.log.Info("Initialized ZeroConf resolver")

	scan := make(chan *zeroconf.ServiceEntry, 1)

	err = resolver.Browse(ctx, shelly.MDNS_SHELLIES, "local.", scan)
	if err != nil {
		dm.log.Error(err, "Failed to browse ZeroConf")
		return err
	}
	dm.log.Info("Started ZeroConf browsing")

	go func(ctx context.Context, log logr.Logger, scan <-chan *zeroconf.ServiceEntry) error {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled")
				return ctx.Err()

			case entry := <-scan:
				log.Info("Browsed", "entry", entry)
				deviceId := entry.Instance
				_, err := dm.storage.GetDeviceByManufacturerAndID("Shelly", deviceId)
				if err != nil {
					log.Info("Device not found, creating new one", "device_id", deviceId)
					host := entry.HostName
					if len(entry.AddrIPv4) > 0 {
						host = string(entry.AddrIPv4[0])
					}
					device := NewDevice("Shelly", deviceId).WithHost(host)

					sd, err := shelly.NewDeviceFromZeroConfEntry(log, entry)
					if err != nil {
						log.Error(err, "Failed to parse device from zeroconf entry", "entry", entry)
						continue
					}
					device.impl = sd

					dm.update <- device
				}
			}
		}
	}(ctx, dm.log.WithName("DeviceManager#WatchZeroConf"), scan)

	return nil
}

// function type that knows how to mak a zerofon entry into a device
type InitializeDeviceFromZeroConfEntry func(log logr.Logger, entry *zeroconf.ServiceEntry) (*Device, error)

type IdentifyDeviceFromZeroConfEntry func(log logr.Logger, entry *zeroconf.ServiceEntry) (myhome.DeviceIdentifier, error)

func (dm *DeviceManager) DiscoverDevices(service string, interval time.Duration, identify IdentifyDeviceFromZeroConfEntry, init InitializeDeviceFromZeroConfEntry) {
	dm.log.Info("Starting device discovery", "service", service, "interval", interval)
	ctx, cancel := context.WithCancel(context.Background())
	dm.cancel = cancel

	go func(ctx context.Context, log logr.Logger) {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled")
				return
			default:
				dm.updateDevices(service, interval, identify, init)
				time.Sleep(interval)
			}
		}
	}(ctx, dm.log.WithName("DeviceManager#DiscoverDevices"))
}

func (dm *DeviceManager) updateDevices(service string, timeout time.Duration, identify IdentifyDeviceFromZeroConfEntry, init InitializeDeviceFromZeroConfEntry) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		dm.log.Error(err, "Failed to initialize resolver", "service", service, "timeout", timeout)
		return
	}
	dm.log.Info("Initialized resolver", "service", service, "timeout", timeout)

	scan := make(chan *zeroconf.ServiceEntry)
	entries := make([]*zeroconf.ServiceEntry, 0)

	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			entries = append(entries, entry)
		}
	}(scan)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = resolver.Browse(ctx, service, "local.", scan)
	if err != nil {
		dm.log.Error(err, "Failed to browse")
		return
	}

	<-ctx.Done()

	dm.mu.Lock()
	for _, entry := range entries {
		identity, err := identify(dm.log, entry)
		if err != nil {
			dm.log.Error(err, "Skipping unidentifiable", "entry", entry)
			continue
		}

		_, err = dm.storage.GetDeviceByManufacturerAndID(identity.Manufacturer, identity.Id)
		if err == nil {
			// Device already known
			continue
		}

		device, err := init(dm.log, entry)
		if err != nil {
			dm.log.Info("Skipping", "entry", entry, "error", err)
			continue
		}

		dm.log.Info("Adding", "device", device)
		err = dm.storage.UpsertDevice(device.Device)
		if err != nil {
			dm.log.Error(err, "Failed to add device", "device", device)
		}
	}
	dm.mu.Unlock()
}

func (dm *DeviceManager) GetDeviceByIdentifier(ctx context.Context, identifier string) (*Device, error) {
	var d *Device
	d, exists := dm.devicesById[identifier]
	if exists {
		return d, nil
	}
	d, exists = dm.devicesByMAC[identifier]
	if exists {
		return d, nil
	}
	d, exists = dm.devicesByHost[identifier]
	if exists {
		return d, nil
	}
	device, err := dm.storage.GetDeviceByIdentifier(identifier)
	if err != nil {
		return nil, err
	}
	return dm.Load(ctx, device.Id)
}

func (dm *DeviceManager) Load(ctx context.Context, id string) (*Device, error) {
	var err error
	d, err := dm.storage.GetDeviceByManufacturerAndID(Shelly, id) // TODO: support other manufacturers(id)
	if err != nil {
		return nil, err
	}
	return dm.Save(ctx, &Device{Device: d})
}

func (dm *DeviceManager) Save(ctx context.Context, d *Device) (*Device, error) {
	dm.devicesById[d.Id] = d
	dm.devicesByMAC[d.MAC] = d
	dm.devicesByHost[d.Host] = d
	if d.Manufacturer == Shelly {
		sd, ok := d.impl.(*shelly.Device)
		if !ok {
			sd = shelly.NewDeviceFromMqttId(ctx, dm.log, d.Id, dm.mqttClient)
		}
		groups, err := dm.storage.GetDeviceGroups(d.Manufacturer, d.Id)
		if err != nil {
			return nil, err
		}
		for _, group := range groups.Groups {
			_, err := kvs.SetKeyValue(ctx, log, types.ChannelMqtt, sd, fmt.Sprintf("group/%s", group.Name), "true")
			if err != nil {
				return nil, err
			}
		}
		d.impl = sd
	}

	return d, dm.storage.UpsertDevice(d.Device)
}

func (dm *DeviceManager) CallE(method myhome.Verb, params any) (any, error) {
	dm.log.Info("Calling method", "method", method, "params", params)
	var err error
	mh, err := myhome.Methods(method)
	if err != nil {
		return nil, err
	}
	// if mh.InType != reflect.TypeOf(params) {
	// 	return nil, fmt.Errorf("invalid parameters for method %s: got %v, want %v", method, reflect.TypeOf(params), mh.InType)
	// }
	result, err := mh.ActionE(params)
	if err != nil {
		return nil, err
	}
	// if mh.OutType != reflect.TypeOf(result) {
	// 	return nil, fmt.Errorf("invalid type for method %s: got %v, want %v", method, reflect.TypeOf(result), mh.OutType)
	// }
	return result, nil
}

func (dm *DeviceManager) MethodE(method myhome.Verb) (*myhome.Method, error) {
	mh, err := myhome.Methods(method)
	if err != nil {
		return nil, err
	}
	return mh, nil
}
