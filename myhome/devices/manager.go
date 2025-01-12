package devices

import (
	"context"
	"encoding/json"
	"mymqtt"
	"pkg/shelly"
	"pkg/shelly/mqtt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

type DeviceManager struct {
	storage *DeviceStorage
	mu      sync.Mutex
	cancel  context.CancelFunc
	watch   chan bool
	log     logr.Logger
}

func NewDeviceManager(log logr.Logger, storage *DeviceStorage) *DeviceManager {
	return &DeviceManager{
		storage: storage,
		log:     log.WithName("DeviceManager"),
		cancel:  nil,
		watch:   nil,
	}
}

func (dm *DeviceManager) Start(mc *mymqtt.Client) error {
	var err error

	dm.log.Info("Starting device manager")

	// Loop on MQTT event devices discovery
	dm.watch, err = dm.WatchMqtt(mc)
	if err != nil {
		dm.log.Error(err, "Failed to watch MQTT events")
		return err
	}

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
	if dm.cancel != nil {
		dm.cancel() // Stop the context
	}
	if dm.watch != nil {
		close(dm.watch) // Close the watch channel to terminate the watch goroutine
	}
	dm.storage.Close() // Close the storage
}

func (dm *DeviceManager) WatchMqtt(mc *mymqtt.Client) (chan bool, error) {
	ch, err := mc.Subscribe("+/events/rpc", 0)
	if err != nil {
		dm.log.Error(err, "Failed to subscribe to shelly devices events")
		return nil, err
	}

	stop := make(chan bool, 1)

	go func() {
		for {
			select {
			// return if stop channel is closed
			case <-stop:
				return
			case msg := <-ch:
				dm.log.Info("Received message", "payload", string(msg))
				event := &mqtt.Event{}
				err := json.Unmarshal(msg, &event)
				if err != nil {
					dm.log.Error(err, "Failed to unmarshal event from payload", "payload", string(msg))
					continue
				}
				if event.Src[:6] != "shelly" {
					dm.log.Info("Skipping non-shelly event", "event", event)
					continue
				}
				deviceId := event.Src
				device, err := dm.storage.GetDeviceByManufacturerAndID("Shelly", deviceId)
				if err != nil {
					dm.log.Info("Device not found, creating new one", "device_id", deviceId)
					device = NewDevice("Shelly", deviceId)
					sd := shelly.NewDeviceFromId(dm.log, mc, deviceId)
					device.MAC = sd.MacAddress
					device.Host = sd.Ipv4_.String()
					out, err := json.Marshal(sd.Info)
					if err != nil {
						dm.log.Error(err, "Failed to marshal device info", "device_id", event.Src)
						continue
					}
					device.Info = string(out)
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
	}()

	return stop, nil
}

// function type that knows how to mak a zerofon entry into a device
type InitializeDeviceFromZeroConfEntry func(log logr.Logger, entry *zeroconf.ServiceEntry) (*Device, error)

type IdentifyDeviceFromZeroConfEntry func(log logr.Logger, entry *zeroconf.ServiceEntry) (*DeviceIdentifier, error)

func (dm *DeviceManager) DiscoverDevices(service string, interval time.Duration, identify IdentifyDeviceFromZeroConfEntry, init InitializeDeviceFromZeroConfEntry) {
	dm.log.Info("Starting device discovery", "service", service, "interval", interval)
	ctx, cancel := context.WithCancel(context.Background())
	dm.cancel = cancel

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				dm.updateDevices(service, interval, identify, init)
				time.Sleep(interval)
			}
		}
	}()
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
