package devices

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

type DeviceManager struct {
	storage *DeviceStorage
	mu      sync.Mutex
	cancel  context.CancelFunc
	log     logr.Logger
}

func NewDeviceManager(log logr.Logger, storage *DeviceStorage) *DeviceManager {
	return &DeviceManager{
		storage: storage,
		log:     log.WithName("DeviceManager"),
	}
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

func (dm *DeviceManager) StopDiscovery() {
	dm.log.Info("Stopping device discovery")
	if dm.cancel != nil {
		dm.cancel()
	}
}
