package devices

import (
	"context"
	"encoding/json"
	"mymqtt"
	"pkg/shelly"
	"pkg/shelly/mqtt"
	"pkg/shelly/types"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

type DeviceManager struct {
	storage *DeviceStorage
	mu      sync.Mutex
	update  chan *Device
	cancel  context.CancelFunc
	log     logr.Logger
	devices map[string]any
}

func NewDeviceManager(log logr.Logger, storage *DeviceStorage) *DeviceManager {
	return &DeviceManager{
		storage: storage,
		log:     log.WithName("DeviceManager"),
		update:  make(chan *Device, 1),
		devices: make(map[string]any),
	}
}

func (dm *DeviceManager) Start(ctx context.Context, mc *mymqtt.Client) error {
	var err error

	dm.log.Info("Starting device manager")

	ctx, dm.cancel = context.WithCancel(ctx)
	go func(ctx context.Context, log logr.Logger, dc <-chan *Device) error {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled")
				return ctx.Err()

			case device := <-dc:
				sd, ok := device.impl.(*shelly.Device)
				if ok && device.MAC == nil {
					// TODO: select HTTP when MQTT is not configured
					err := sd.Init(mc, types.ChannelMqtt)
					if err != nil {
						log.Error(err, "Failed to init shelly device", "device_id", device.ID)
						continue
					}
					device.MAC = sd.MacAddress
					device.Host = sd.Ipv4_.String()
					out, err := json.Marshal(sd.Info)
					if err != nil {
						log.Error(err, "Failed to marshal device info", "device_id", device.ID)
						continue
					}
					device.Info = string(out)
				}
				err = dm.storage.UpsertDevice(device)
				if err != nil {
					log.Error(err, "Failed to upsert device", "device", device)
					continue
				}

				dm.devices[device.ID] = device
			}
		}
	}(ctx, log.WithName("DeviceManager#NewDevices"), dm.update)

	// Load every devices from storage & init them
	devices, err := dm.storage.GetAllDevices()
	if err != nil {
		return err
	}
	for _, device := range devices {
		dm.update <- device.WithImpl(shelly.NewDeviceFromId(dm.log.WithName(device.ID), device.ID))
	}

	// Loop on MQTT event devices discovery
	err = dm.WatchMqtt(ctx, mc)
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

func (dm *DeviceManager) Stop() {
	dm.log.Info("Stopping device manager")
	if dm.cancel != nil {
		dm.cancel()
		dm.cancel = nil
	}
	dm.storage.Close()
}

func (dm *DeviceManager) WatchMqtt(ctx context.Context, mc *mymqtt.Client) error {
	var sd *shelly.Device

	ch, err := mc.Subscribe("+/events/rpc", 0)
	if err != nil {
		dm.log.Error(err, "Failed to subscribe to shelly devices events")
		return err
	}

	go func(ctx context.Context, log logr.Logger) error {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled")
				return ctx.Err()

			case msg := <-ch:
				log.Info("Received message", "payload", string(msg))
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
				device, err := dm.storage.GetDeviceByManufacturerAndID("Shelly", deviceId)
				if err != nil {
					log.Info("Device not found, creating new one", "device_id", deviceId)
					device = NewDevice("Shelly", deviceId)
					sd = shelly.NewDeviceFromId(dm.log, deviceId)
					sd.Init(mc, types.ChannelMqtt)
					device.MAC = sd.MacAddress
					device.Host = sd.Ipv4_.String()
					out, err := json.Marshal(sd.Info)
					if err != nil {
						dm.log.Error(err, "Failed to marshal device info", "device_id", event.Src)
						continue
					}
					device.Info = string(out)
					device.impl = sd
				}

				if device.Config == "" {
					sd, ok := device.impl.(*shelly.Device)
					if !ok {
						dm.log.Info("Device is not a shelly device", "device_id", event.Src)
					} else {
						out, err := sd.CallE(types.ChannelMqtt, "Shelly", "GetConfig", nil)
						if err != nil {
							dm.log.Error(err, "Failed to get shelly config", "device_id", event.Src)
						} else {
							config, ok := out.(map[string]interface{})
							if ok {
								c, err := json.Marshal(config)
								if err != nil {
									dm.log.Error(err, "Failed to marshal shelly config", "device_id", event.Src)
								} else {
									device.Config = string(c)
								}
							} else {
								dm.log.Info("shelly config is not valid JSON", "out", out)
							}
						}
					}
				}

				dm.log.Info("Updating device", "device", device)
				err = device.UpdateFromMqttEvent(event)
				if err != nil {
					dm.log.Error(err, "Failed to update device from MQTT event", "device_id", event.Src)
					continue
				}
				dm.log.Info("Storing updated device", "device", device)
				dm.storage.UpsertDevice(device)
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

type IdentifyDeviceFromZeroConfEntry func(log logr.Logger, entry *zeroconf.ServiceEntry) (*DeviceIdentifier, error)

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

		_, err = dm.storage.GetDeviceByManufacturerAndID(identity.Manufacturer, identity.ID)
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
		err = dm.storage.UpsertDevice(device)
		if err != nil {
			dm.log.Error(err, "Failed to add device", "device", device)
		}
	}
	dm.mu.Unlock()
}

func (dm *DeviceManager) GetDeviceByIdentifier(identifier string) (*Device, error) {
	dm.devices
	d, err := dm.storage.GetDeviceByIdentifier(identifier)
}
