package watch

import (
	"context"
	"fmt"
	"global"
	"myhome"
	"myhome/devices"
	"mynet"
	"pkg/shelly"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

func ZeroConf(ctx context.Context, dm devices.Manager, db devices.DeviceRegistry, dr mynet.Resolver) error {
	log := ctx.Value(global.LogKey).(logr.Logger)

	go func(ctx context.Context, log logr.Logger) error {
		stopped := make(chan struct{}, 1)
		scan := make(chan *zeroconf.ServiceEntry, 1)
		for {
			err := dr.BrowseService(ctx, shelly.MDNS_SHELLIES, "local.", scan)
			if err != nil {
				log.Error(err, "Failed to start ZeroConf browser")
				return err
			}
			log.Info("(Re)Started ZeroConf browser")

			go func(ctx context.Context, log logr.Logger, scan <-chan *zeroconf.ServiceEntry, stopped chan<- struct{}) error {
				for {
					select {
					case <-ctx.Done():
						log.Error(ctx.Err(), "Cancelled")
						stopped <- struct{}{}
						return ctx.Err()

					case entry, ok := <-scan:
						if !ok || entry == nil {
							log.Error(fmt.Errorf("entry=%v, ok=%v", entry, ok), "Failed to browse ZeroConf : terminating browser")
							return nil
						}
						log.Info("Browsed", "entry", entry)
						device, err := db.GetDeviceByAny(ctx, entry.Instance)
						if err != nil || device.Info == nil {
							sd, err := shelly.NewDeviceFromZeroConfEntry(ctx, log, dr, entry)
							if err != nil {
								log.Error(err, "Failed to create device from zeroconf entry", "entry", entry)
								continue
							}
							device, err = myhome.NewDeviceFromImpl(ctx, log, sd)
							if err != nil {
								log.Error(err, "Failed to create device from shelly device", "entry", entry)
								continue
							}
						} else {
							log.Info("Found device in DB", "device_id", device.Id(), "name", device.Name())
							if device.Impl() == nil {
								log.Info("Loading device details in memory", "device_id", device.Id(), "name", device.Name())
								sd, err := shelly.NewDeviceFromSummary(ctx, log, device)
								if err != nil {
									log.Error(err, "Failed to create device from summary", "device", device)
									continue
								}
								device = device.WithImpl(sd)
							}
							device = device.WithZeroConfEntry(entry)
						}
						dm.UpdateChannel() <- device
					}
				}
			}(ctx, log.WithName("DeviceManager#WatchZeroConf"), scan, stopped)

			select {
			case <-ctx.Done():
				log.Error(ctx.Err(), "Cancelled")
				return ctx.Err()
			case <-stopped:
				log.Info("Restarting ZeroConf browser")
			}
		}
	}(ctx, log)

	return nil
}

// // function type that knows how to mak a zerofon entry into a device
// type InitializeDeviceFromZeroConfEntry func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.Device, error)

// type IdentifyDeviceFromZeroConfEntry func(log logr.Logger, entry *zeroconf.ServiceEntry) (myhome.DeviceIdentifier, error)

// func DiscoverDevices(ctx context.Context, service string, interval time.Duration, identify IdentifyDeviceFromZeroConfEntry, init InitializeDeviceFromZeroConfEntry) {
// 	log := ctx.Value(global.LogKey).(logr.Logger)
// 	log.Info("Starting device discovery", "service", service, "interval", interval)
// 	go func(ctx context.Context, log logr.Logger) {
// 		for {
// 			select {
// 			case <-ctx.Done():
// 				log.Info("Cancelled")
// 				return
// 			default:
// 				updateDevices(ctx, service, interval, identify, init)
// 				time.Sleep(interval)
// 			}
// 		}
// 	}(ctx, log.WithName("DeviceManager#DiscoverDevices"))
// }

// func updateDevices(ctx context.Context, service string, timeout time.Duration, identify IdentifyDeviceFromZeroConfEntry, init InitializeDeviceFromZeroConfEntry) {
// 	log := ctx.Value(global.LogKey).(logr.Logger)
// 	resolver, err := zeroconf.NewResolver(nil)
// 	if err != nil {
// 		log.Error(err, "Failed to initialize resolver", "service", service, "timeout", timeout)
// 		return
// 	}
// 	log.Info("Initialized resolver", "service", service, "timeout", timeout)

// 	scan := make(chan *zeroconf.ServiceEntry)
// 	entries := make([]*zeroconf.ServiceEntry, 0)

// 	go func(results <-chan *zeroconf.ServiceEntry) {
// 		for entry := range results {
// 			entries = append(entries, entry)
// 		}
// 	}(scan)

// 	ctx, cancel := context.WithTimeout(context.Background(), timeout)
// 	defer cancel()

// 	err = resolver.Browse(ctx, service, "local.", scan)
// 	if err != nil {
// 		log.Error(err, "Failed to browse")
// 		return
// 	}

// 	<-ctx.Done()

// 	dm.mu.Lock()
// 	for _, entry := range entries {
// 		identity, err := identify(log, entry)
// 		if err != nil {
// 			log.Error(err, "Skipping unidentifiable", "entry", entry)
// 			continue
// 		}

// 		_, err = dm.storage.GetDeviceByManufacturerAndID(identity.Manufacturer, identity.Id)
// 		if err == nil {
// 			// Device already known
// 			continue
// 		}

// 		device, err := init(log, entry)
// 		if err != nil {
// 			log.Info("Skipping", "entry", entry, "error", err)
// 			continue
// 		}

// 		log.Info("Adding", "device", device)
// 		err = dm.storage.UpsertDevice(device.Device)
// 		if err != nil {
// 			log.Error(err, "Failed to add device", "device", device)
// 		}
// 	}
// 	dm.mu.Unlock()
// }

// Loop on ZeroConf devices discovery
// dm.DiscoverDevices(shelly.MDNS_SHELLIES, 300*time.Second, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.DeviceIdentifier, error) {
// 	log.Info("Identifying", "entry", entry)
// 	return &devices.DeviceIdentifier{
// 		Manufacturer: devices.Shelly,
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
// 			Manufacturer: devices.Shelly,
// 			ID:           sd.Id_,
// 		},
// 		MAC:  sd.MacAddress,
// 		Host: sd.Ipv4_.String(),
// 	}, nil
// })
