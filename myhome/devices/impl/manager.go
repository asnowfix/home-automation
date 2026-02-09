package impl

import (
	"context"
	"fmt"
	"myhome"
	"myhome/ctl/options"
	"myhome/daemon/watch"
	mhd "myhome/devices"
	"myhome/model"
	"myhome/mqtt"
	"myhome/sfr"
	"myhome/storage"
	"myhome/ui"
	"mynet"
	"net"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/blu"
	"pkg/shelly/gen1"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"reflect"
	"strings"
	"sync"
	"time"
	"tools"

	shellyscript "internal/myhome/shelly/script"
	shellysetup "internal/myhome/shelly/setup"

	"github.com/go-logr/logr"
)

type DeviceManager struct {
	dr             mhd.DeviceRegistry
	update         chan *myhome.Device
	refreshed      chan *myhome.Device
	cancel         context.CancelFunc
	log            logr.Logger
	mqttClient     mqtt.Client
	sseBroadcaster SSEBroadcaster // For broadcasting sensor updates to UI
	resolver       mynet.Resolver
	router         model.Router
	setupConfig    shellysetup.Config // Configuration for auto-setup of new devices
	setupInFlight  sync.Map           // Track devices currently being set up (device_id -> bool)
}

// SSEBroadcaster interface for broadcasting sensor updates to UI
type SSEBroadcaster interface {
	BroadcastSensorUpdate(deviceID string, sensor string, value string)
	BroadcastDeviceUpdate(deviceData ui.DeviceView)
}

// maxConcurrentRefreshes limits the number of concurrent device refresh goroutines
// to prevent goroutine leaks when mDNS or network operations are slow/blocked
const maxConcurrentRefreshes = 10

func NewDeviceManager(ctx context.Context, s *storage.DeviceStorage, resolver mynet.Resolver, mqttClient mqtt.Client, sseBroadcaster SSEBroadcaster) *DeviceManager {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}

	dm := &DeviceManager{
		dr:             mhd.NewCache(ctx, s),
		log:            log.WithName("DeviceManager"),
		update:         make(chan *myhome.Device, 64), // TODO configurable buffer size
		refreshed:      make(chan *myhome.Device, 64), // TODO configurable buffer size
		mqttClient:     mqttClient,
		resolver:       resolver,
		sseBroadcaster: sseBroadcaster,
	}

	myhome.RegisterMethodHandler(myhome.DevicesMatch, func(ctx context.Context, in any) (any, error) {
		name := in.(string)
		devices := make([]devices.Device, 0)

		var ds []*myhome.Device
		var err error

		if name == "*" {
			dm.log.Info("Getting all devices")
			ds, err = dm.dr.GetAllDevices(ctx)
		} else {
			name = strings.TrimPrefix(strings.TrimSuffix(name, "*"), "*")
			dm.log.Info("Getting devices matching name", "name", name)
			ds, err = dm.dr.GetDevicesMatchingAny(ctx, name)
		}
		if err != nil {
			dm.log.Error(err, "Failed to get all devices")
			return nil, err
		}
		dm.log.Info("Found devices", "num_devices", len(ds))
		for _, d := range ds {
			devices = append(devices, d)
		}
		return &devices, nil
	})
	myhome.RegisterMethodHandler(myhome.DeviceLookup, func(ctx context.Context, in any) (any, error) {
		name := in.(string)

		devices := make([]devices.Device, 0)
		device, err := dm.GetDeviceByAny(ctx, name)
		if err == nil {
			dm.log.Info("Found device by identifier", "identifier", name)
			devices = append(devices, device.DeviceSummary)
			return &devices, nil
		}

		return nil, fmt.Errorf("failed to get device by identifier: %v", err)
	})
	myhome.RegisterMethodHandler(myhome.DeviceShow, func(ctx context.Context, in any) (any, error) {
		params := in.(*myhome.DeviceShowParams)
		return dm.GetDeviceByAny(ctx, params.Identifier)
	})
	myhome.RegisterMethodHandler(myhome.DeviceForget, func(ctx context.Context, in any) (any, error) {
		return nil, dm.ForgetDevice(ctx, in.(string))
	})
	myhome.RegisterMethodHandler(myhome.DeviceRefresh, func(ctx context.Context, in any) (any, error) {
		log := dm.log.WithName("rpc/device.refresh")
		ident := in.(string)
		log.V(1).Info("New", "ident", ident)
		device, err := dm.GetDeviceByAny(ctx, ident)
		if err != nil {
			log.Error(err, "Failed to get device by identifier", "identifier", ident)
			return nil, err
		}
		// Ensure implementation is loaded
		if device.Impl() == nil {
			sd, err := shelly.NewDeviceFromSummary(ctx, log, device)
			if err != nil {
				return nil, err
			}
			device = device.WithImpl(sd)
		}

		modified, err := device.Refresh(ctx)

		// Even if refresh failed, save the device if it was modified
		// This handles cases where HTTP fails and clears the host
		if modified {
			if _, saveErr := dm.dr.SetDevice(ctx, device, true); saveErr != nil {
				log.Error(saveErr, "Failed to save modified device after refresh", "device_id", device.Id())
			} else {
				log.V(1).Info("Saved modified device after refresh", "device_id", device.Id(), "refresh_error", err != nil)
				dm.sseBroadcaster.BroadcastDeviceUpdate(ui.DeviceToView(device))
			}
		}

		// Return the original refresh error if any
		if err != nil {
			return nil, err
		}

		return device, nil
	})
	myhome.RegisterMethodHandler(myhome.DeviceSetup, func(ctx context.Context, in any) (any, error) {
		params := in.(*myhome.DeviceSetupParams)
		log := dm.log.WithName("rpc/device.setup")
		log.V(1).Info("New", "params", params)
		device, err := dm.GetDeviceByAny(ctx, params.Identifier)
		if err != nil {
			log.Error(err, "Failed to get device by identifier", "identifier", params.Identifier)
			return nil, err
		}

		// For Gen1 and BLU devices: skip device communication, but save name to DB if provided
		if shelly.IsGen1Device(device.Id()) || shelly.IsBluDevice(device.Id()) {
			deviceType := "Gen1"
			if shelly.IsBluDevice(device.Id()) {
				deviceType = "BLU"
			}
			log.V(1).Info("Skipping device communication for "+deviceType+" device", "device", device.Id())

			// Save name to DB if provided
			if params.Name != "" && params.Name != device.Name() {
				log.V(1).Info("Updating device name in DB", "device", device.Id(), "name", params.Name)
				device = device.WithName(params.Name)
				if _, err := dm.dr.SetDevice(ctx, device, true); err != nil {
					return nil, fmt.Errorf("failed to update device name in DB: %w", err)
				}
			}
			return nil, nil
		}

		// Ensure implementation is loaded and initialized
		sd, ok := device.Impl().(*shelly.Device)
		if !ok || sd == nil {
			impl, err := shelly.NewDeviceFromSummary(ctx, log, device)
			if err != nil {
				return nil, err
			}
			sd, ok = impl.(*shelly.Device)
			if !ok {
				return nil, fmt.Errorf("unexpected device implementation type: %T", impl)
			}
		}
		// Initialize device (sets up MQTT channels if needed)
		if err := sd.Init(ctx); err != nil {
			log.Error(err, "Failed to initialize device", "device", device.Id())
			// Continue anyway - setup may still work via HTTP
		}

		// Build setup config, merging RPC params with default config
		cfg := dm.setupConfig
		if params.MqttBroker != "" {
			cfg.MqttBroker = params.MqttBroker
		}

		// Build WiFi config from params
		wifiCfg := shellysetup.WifiConfig{
			StaEssid:   params.StaEssid,
			StaPasswd:  params.StaPasswd,
			Sta1Essid:  params.Sta1Essid,
			Sta1Passwd: params.Sta1Passwd,
			ApPasswd:   params.ApPasswd,
		}

		// Use provided name or fall back to device's current name
		targetName := params.Name
		if targetName == "" {
			targetName = sd.Name()
		}

		// Run setup
		err = shellysetup.SetupDeviceWithWifi(ctx, log.WithName(device.Id()), sd, targetName, cfg, wifiCfg)
		if err != nil {
			log.Error(err, "Failed to setup device", "device", device.Id())
			return nil, fmt.Errorf("setup failed: %w", err)
		}

		// Refresh device info from Shelly and update DB with new name
		if _, err := sd.Refresh(ctx, types.ChannelHttp); err != nil {
			log.Error(err, "Failed to refresh device after setup", "device", device.Id())
		} else {
			// Update device name in DB from refreshed Shelly info
			device = device.WithName(sd.Name())
			if _, err := dm.dr.SetDevice(ctx, device, true); err != nil {
				log.Error(err, "Failed to update device in DB after setup", "device", device.Id())
			}
		}

		log.V(1).Info("Setup complete", "device", device.Id())
		return nil, nil
	})
	myhome.RegisterMethodHandler(myhome.DeviceUpdate, func(ctx context.Context, in any) (any, error) {
		device := in.(*myhome.Device)
		log := dm.log.WithName("rpc/device.update")
		log.V(1).Info("New", "id", device.Id(), "name", device.Name())

		var modified bool
		if modified, err = dm.dr.SetDevice(ctx, device, true); err != nil {
			log.Error(err, "Failed to update device in DB", "device", device.Id())
			return nil, err
		}

		// Broadcast device update via SSE if broadcaster is available (only when a UI is connected)
		if modified {
			log.V(1).Info("Broadcasting device update (RPC)", "device", device.Id())
			deviceView := ui.DeviceToView(device)
			dm.sseBroadcaster.BroadcastDeviceUpdate(deviceView)
		}

		return nil, nil
	})
	myhome.RegisterMethodHandler(myhome.DeviceSetRoom, func(ctx context.Context, in any) (any, error) {
		var err error

		params := in.(*myhome.DeviceSetRoomParams)
		log := dm.log.WithName("rpc/device.setroom")
		log.V(1).Info("New", "identifier", params.Identifier, "room_id", params.RoomId)
		// Save room to database
		if _, err = dm.dr.SetDeviceRoom(ctx, params.Identifier, params.RoomId); err != nil {
			log.Error(err, "Failed to update device room in DB", "identifier", params.Identifier)
			return nil, err
		}

		// Also set room-id in device KVS (for Gen2+ Shelly devices)
		device, err := dm.GetDeviceByAny(ctx, params.Identifier)
		if err != nil {
			log.Error(err, "Failed to get device for KVS update", "identifier", params.Identifier)
			return nil, nil // DB update succeeded, KVS is best-effort
		}

		// Skip KVS for Gen1 and BLU devices (they don't support KVS)
		if shelly.IsGen1Device(device.Id()) || shelly.IsBluDevice(device.Id()) {
			return nil, nil
		}

		// Get or create Shelly device implementation
		sd, ok := device.Impl().(*shelly.Device)
		if !ok || sd == nil {
			impl, err := shelly.NewDeviceFromSummary(ctx, log, device)
			if err != nil {
				log.Error(err, "Failed to create device for KVS update", "identifier", params.Identifier)
				return nil, nil // DB update succeeded, KVS is best-effort
			}
			sd, ok = impl.(*shelly.Device)
			if !ok {
				return nil, nil
			}
		}

		// Set room-id in device KVS
		_, err = kvs.SetKeyValue(ctx, log, types.ChannelDefault, sd, "room-id", params.RoomId)
		if err != nil {
			log.Error(err, "Failed to set room-id in device KVS", "identifier", params.Identifier, "room_id", params.RoomId)
			// Don't return error - DB update succeeded
		} else {
			log.Info("Set room-id in device KVS", "identifier", params.Identifier, "room_id", params.RoomId)
		}

		return nil, nil
	})
	myhome.RegisterMethodHandler(myhome.DeviceListByRoom, func(ctx context.Context, in any) (any, error) {
		params := in.(*myhome.DeviceListByRoomParams)
		log := dm.log.WithName("rpc/device.listbyroom")
		log.V(1).Info("New", "room_id", params.RoomId)
		devices, err := dm.dr.GetDevicesByRoom(ctx, params.RoomId)
		if err != nil {
			log.Error(err, "Failed to get devices by room", "room_id", params.RoomId)
			return nil, err
		}
		return &myhome.DeviceListByRoomResult{Devices: devices}, nil
	})
	myhome.RegisterMethodHandler(myhome.ThermometerList, func(ctx context.Context, in any) (any, error) {
		return dm.HandleThermometerList(ctx)
	})
	myhome.RegisterMethodHandler(myhome.DoorList, func(ctx context.Context, in any) (any, error) {
		return dm.HandleDoorList(ctx)
	})

	return dm
}

func (dm *DeviceManager) UpdateChannel() chan<- *myhome.Device {
	return dm.update
}

func (dm *DeviceManager) Start(ctx context.Context) error {
	var err error

	dm.log.Info("Starting device manager")

	dm.router = sfr.GetRouter(ctx)

	// Register heater service handlers
	heaterService := shellyscript.NewHeaterService(dm.log, dm)
	heaterService.RegisterHandlers()

	go dm.storeDeviceLoop(logr.NewContext(ctx, dm.log.WithName("storeDeviceLoop")), dm.refreshed)
	go dm.deviceUpdaterLoop(logr.NewContext(ctx, dm.log.WithName("deviceUpdaterLoop")))
	go dm.runDeviceRefreshJob(logr.NewContext(ctx, dm.log.WithName("runDeviceRefreshJob")), options.Flags.RefreshInterval)

	// Loop on MQTT event devices discovery
	err = watch.StartMqttWatcher(ctx, dm.mqttClient, dm, dm)
	if err != nil {
		dm.log.Error(err, "Failed to watch MQTT events")
		return err
	}

	// Configure auto-setup for new devices (used by device updater loop)
	dm.setupConfig = shellysetup.Config{
		Resolver: dm.resolver,
	}
	// Use the current process MQTT broker for auto-setup
	if dm.mqttClient != nil {
		// GetServer returns host:port, use it directly
		dm.setupConfig.MqttBroker = dm.mqttClient.GetServer()
		dm.setupConfig.MqttPort = 0 // Signal that broker already includes port
	} else if options.Flags.MqttBroker != "" {
		dm.setupConfig.MqttBroker = options.Flags.MqttBroker
		dm.setupConfig.MqttPort = 1883
	} else {
		dm.setupConfig.MqttBroker = "mqtt.local"
		dm.setupConfig.MqttPort = 1883
	}
	dm.log.Info("Auto-setup configuration", "enabled", options.Flags.AutoSetup, "mqtt_broker", dm.setupConfig.MqttBroker)

	// Loop on ZeroConf devices discovery
	err = watch.ZeroConf(ctx, options.Flags.MdnsTimeout, dm, dm.dr, dm.resolver)
	if err != nil {
		dm.log.Error(err, "Failed to watch ZeroConf devices")
		return err
	}

	// Start Gen1 MQTT listener for sensor data (with SSE broadcaster)
	err = gen1.StartMqttListener(ctx, dm.mqttClient, dm.dr, dm.router, dm.sseBroadcaster)
	if err != nil {
		dm.log.Error(err, "Failed to start Gen1 MQTT listener")
		return err
	}

	// Start BLU listener for Shelly BLU device discovery (with SSE broadcaster)
	err = blu.StartBLUListener(ctx, dm.mqttClient, dm.dr, dm.sseBroadcaster)
	if err != nil {
		dm.log.Error(err, "Failed to start BLU listener")
		return err
	}

	return nil
}

func (dm *DeviceManager) deviceUpdaterLoop(ctx context.Context) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}
	log.Info("Starting updater loop", "max_concurrent_refreshes", maxConcurrentRefreshes, "auto_setup", options.Flags.AutoSetup)

	// Semaphore to limit concurrent refresh goroutines
	sem := make(chan struct{}, maxConcurrentRefreshes)

	for {
		select {
		case <-ctx.Done():
			log.Info("Exiting updater loop")
			return

		case device := <-dm.update:
			// Check if this is a new device that needs auto-setup
			isNewDevice := device.Info == nil
			deviceId := device.Id()

			// Acquire semaphore slot (blocks if all slots busy)
			sem <- struct{}{}
			log.V(1).Info("Updater loop: processing", "device", device.DeviceSummary, "is_new", isNewDevice)

			go func(d *myhome.Device, isNew bool, devId string) {
				defer func() { <-sem }() // Release slot when done

				// Trigger auto-setup for new devices if enabled
				if isNew && options.Flags.AutoSetup {
					dm.triggerAutoSetup(ctx, log, d, devId)
				}

				refreshOneDevice(logr.NewContext(tools.WithToken(ctx), log.WithName("refreshOneDevice").WithName(d.Name())), d, dm.router, dm.refreshed)
			}(device, isNewDevice, deviceId)
		}
	}
}

// triggerAutoSetup runs auto-setup for a new device in a goroutine
func (dm *DeviceManager) triggerAutoSetup(ctx context.Context, log logr.Logger, device *myhome.Device, deviceId string) {
	// Check if setup is already in progress for this device
	if _, loaded := dm.setupInFlight.LoadOrStore(deviceId, true); loaded {
		log.Info("Auto-setup already in progress for device", "device_id", deviceId)
		return
	}

	// Skip Gen1 devices - they don't support the same setup process
	if shelly.IsGen1Device(deviceId) {
		log.Info("Skipping auto-setup for Gen1 device", "device_id", deviceId)
		dm.setupInFlight.Delete(deviceId)
		return
	}

	// Skip BLU devices - they are passive sensors
	if shelly.IsBluDevice(deviceId) {
		log.Info("Skipping auto-setup for BLU device", "device_id", deviceId)
		dm.setupInFlight.Delete(deviceId)
		return
	}

	// Get the Shelly device implementation
	sd, ok := device.Impl().(*shelly.Device)
	if !ok {
		log.Error(nil, "Cannot auto-setup: device implementation is not a Shelly device", "device_id", deviceId, "impl_type", reflect.TypeOf(device.Impl()))
		dm.setupInFlight.Delete(deviceId)
		return
	}

	// Check if device is already set up (has watchdog script running)
	if shellysetup.IsDeviceSetUp(ctx, log, sd) {
		log.V(1).Info("Skipping auto-setup: device already set up", "device_id", deviceId)
		dm.setupInFlight.Delete(deviceId)
		return
	}

	log.Info("Starting auto-setup for new device", "device_id", deviceId, "mqtt_broker", dm.setupConfig.MqttBroker)

	go func() {
		defer dm.setupInFlight.Delete(deviceId)

		setupLog := log.WithName("autoSetup").WithName(deviceId)
		err := shellysetup.SetupDevice(ctx, setupLog, sd, sd.Name(), dm.setupConfig)
		if err != nil {
			setupLog.Error(err, "Auto-setup failed for device", "device_id", deviceId)
			return
		}
		setupLog.Info("Auto-setup completed successfully", "device_id", deviceId)
	}()
}

func refreshOneDevice(ctx context.Context, device *myhome.Device, router model.Router, refreshed chan<- *myhome.Device) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}

	// Skip Gen1 devices - they are updated via MQTT messages only
	if shelly.IsGen1Device(device.Id()) {
		log.V(1).Info("Skipping Gen1 device refresh (updated via MQTT)", "device", device.DeviceSummary)
		return
	}

	// Skip BLU devices (Generation 0) - they are updated via MQTT events only
	if shelly.IsBluDevice(device.Id()) {
		log.V(1).Info("Skipping BLU device refresh (updated via BLE+MQTT events)", "device", device.DeviceSummary)
		return
	}

	var modified bool = false
	mac, err := net.ParseMAC(device.MAC)
	host, err := router.GetHostByMac(ctx, mac)
	if err == nil {
		ip := host.Ip().String()
		if ip != device.Host() {
			log.V(1).Info("Changing IP", "device", device.DeviceSummary, "old_ip", device.Host(), "new_ip", ip)
			device = device.WithHost(ip)
			modified = true
		}
	} else {
		log.V(1).Info("Dropping IP", "device", device.DeviceSummary, "old_ip", device.Host())
		device = device.WithHost("")
		modified = true
	}

	updated, err := device.Refresh(ctx)
	if err != nil {
		log.Error(err, "Failed to refresh device", "device", device.DeviceSummary)
		return
	}

	if updated {
		modified = true
	}

	if !modified {
		log.V(1).Info("Device is up to date", "device", device.DeviceSummary)
		return
	}

	log.V(1).Info("Updated: preparing to store", "device", device.DeviceSummary)
	refreshed <- device
}

func (dm *DeviceManager) storeDeviceLoop(ctx context.Context, refreshed <-chan *myhome.Device) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}

	log.Info("Starting storage loop")
	for {
		select {
		case <-ctx.Done():
			return
		case device := <-refreshed:
			var modified bool = false
			modified, err = dm.dr.SetDevice(ctx, device, true)
			if err != nil {
				log.Error(err, "Failed to upsert", "device", device.DeviceSummary)
				continue
			}
			log.Info("Stored device", "device", device.DeviceSummary)

			// Broadcast device update via SSE if broadcaster is available (only when a UI is connected)
			if modified {
				log.V(1).Info("Broadcasting device update (storage loop)", "device", device.DeviceSummary)
				deviceView := ui.DeviceToView(device)
				dm.sseBroadcaster.BroadcastDeviceUpdate(deviceView)
			}
		}
	}
}

func (dm *DeviceManager) runDeviceRefreshJob(ctx context.Context, interval time.Duration) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	i := 0

	for {
		select {
		case <-ctx.Done():
			log.Info("Exiting known devices refresh loop")
			return
		case <-ticker.C:
			devices, err := dm.GetAllDevices(ctx)
			if err != nil {
				log.Error(err, "Failed to get all devices")
				return
			}

			// Filter out Gen1 and BLU devices (they are updated via MQTT only)
			gen2Devices := make([]*myhome.Device, 0)
			for _, d := range devices {
				if !shelly.IsGen1Device(d.Id()) && !strings.HasPrefix(d.Id(), "shellyblu-") {
					gen2Devices = append(gen2Devices, d)
				}
			}

			if len(gen2Devices) == 0 {
				log.V(1).Info("No Gen2+ devices to refresh (Gen1 and BLU devices are updated via MQTT)")
				continue
			}

			if i >= len(gen2Devices) {
				i = 0
			}

			log.Info("Refreshing device", "index", i, "total_gen2_devices", len(gen2Devices), "device", gen2Devices[i].DeviceSummary)
			dm.UpdateChannel() <- gen2Devices[i]
			i++
		}
	}
}

func (dm *DeviceManager) Flush() error {
	err := dm.dr.Flush()
	if err != nil {
		dm.log.Error(err, "Failed to flush device storage")
		return err
	}
	return nil
}

func (dm *DeviceManager) GetAllDevices(ctx context.Context) ([]*myhome.Device, error) {
	return dm.dr.GetAllDevices(ctx)
}

func (dm *DeviceManager) GetDevicesMatchingAny(ctx context.Context, name string) ([]*myhome.Device, error) {
	return dm.dr.GetDevicesMatchingAny(ctx, name)
}

func (dm *DeviceManager) GetDeviceByAny(ctx context.Context, any string) (*myhome.Device, error) {
	return dm.dr.GetDeviceByAny(ctx, any)
}

func (dm *DeviceManager) GetDeviceById(ctx context.Context, id string) (*myhome.Device, error) {
	return dm.dr.GetDeviceById(ctx, id)
}

func (dm *DeviceManager) GetDeviceByHost(ctx context.Context, host string) (*myhome.Device, error) {
	return dm.dr.GetDeviceByHost(ctx, host)
}

func (dm *DeviceManager) GetDeviceByMAC(ctx context.Context, mac string) (*myhome.Device, error) {
	return dm.dr.GetDeviceByMAC(ctx, mac)
}

func (dm *DeviceManager) GetDeviceByName(ctx context.Context, name string) (*myhome.Device, error) {
	return dm.dr.GetDeviceByName(ctx, name)
}

func (dm *DeviceManager) ForgetDevice(ctx context.Context, id string) error {
	return dm.dr.ForgetDevice(ctx, id)
}

func (dm *DeviceManager) SetDeviceRoom(ctx context.Context, identifier string, roomId string) (bool, error) {
	return dm.dr.SetDeviceRoom(ctx, identifier, roomId)
}

func (dm *DeviceManager) GetDevicesByRoom(ctx context.Context, roomId string) ([]*myhome.Device, error) {
	return dm.dr.GetDevicesByRoom(ctx, roomId)
}

func (dm *DeviceManager) SetDevice(ctx context.Context, d *myhome.Device, overwrite bool) (bool, error) {
	if d.Manufacturer_ == string(myhome.SHELLY) {
		sd, ok := d.Impl().(*shelly.Device)
		if !ok {
			return false, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(d.Impl()), d)
		}
		d.WithImpl(sd)
	}
	return dm.dr.SetDevice(ctx, d, overwrite)
}

func (dm *DeviceManager) CallE(ctx context.Context, method myhome.Verb, params any) (any, error) {
	dm.log.Info("Calling method", "method", method, "params", params)
	var err error
	mh, err := myhome.Methods(method)
	if err != nil {
		return nil, err
	}
	// if mh.InType != reflect.TypeOf(params) {
	// 	return nil, fmt.Errorf("invalid parameters for method %s: got %v, want %v", method, reflect.TypeOf(params), mh.InType)
	// }
	result, err := mh.ActionE(ctx, params)
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

// HandleThermometerList returns a list of devices with temperature sensing capability
func (dm *DeviceManager) HandleThermometerList(ctx context.Context) (*myhome.ThermometerListResult, error) {
	devices, err := dm.GetAllDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}

	thermometers := make([]myhome.ThermometerInfo, 0)

	for _, d := range devices {
		// Check for Gen1 H&T devices by Info.Application (if populated) and fallback to device ID prefix (e.g., "shellyht-208500")
		if (d.Info != nil && d.Info.Application == "shellyht") || strings.HasPrefix(d.Id(), "shellyht-") {
			thermometers = append(thermometers, myhome.ThermometerInfo{
				ID:        d.Id(),
				Name:      d.Name(),
				Type:      "Gen1",
				MqttTopic: fmt.Sprintf("shellies/%s/sensor/temperature", d.Id()),
				RoomId:    d.RoomId, // Add this field

			})
			continue
		}

		// Check for BLU devices with temperature capability
		if d.Info != nil && d.Info.BTHome != nil {
			for _, cap := range d.Info.BTHome.Capabilities {
				if cap == "temperature" {
					// BLU devices use MAC address in topic (with colons)
					thermometers = append(thermometers, myhome.ThermometerInfo{
						ID:        d.Id(),
						Name:      d.Name(),
						Type:      "BLU",
						MqttTopic: fmt.Sprintf("shelly-blu/events/%s", d.MAC),
						RoomId:    d.RoomId, // Add this field
					})
					break
				}
			}
		}
	}

	return &myhome.ThermometerListResult{Thermometers: thermometers}, nil
}

// HandleDoorList returns a list of devices with window/door sensing capability
func (dm *DeviceManager) HandleDoorList(ctx context.Context) (*myhome.DoorListResult, error) {
	devices, err := dm.GetAllDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}

	doors := make([]myhome.DoorInfo, 0)

	for _, d := range devices {
		// Check for BLU devices with window capability
		if d.Info != nil && d.Info.BTHome != nil {
			for _, cap := range d.Info.BTHome.Capabilities {
				if cap == "window" {
					// BLU devices use MAC address in topic (with colons)
					doors = append(doors, myhome.DoorInfo{
						ID:        d.Id(),
						Name:      d.Name(),
						Type:      "BLU",
						MqttTopic: fmt.Sprintf("shelly-blu/events/%s", d.MAC),
						RoomId:    d.RoomId,
					})
					break
				}
			}
		}
	}

	return &myhome.DoorListResult{Doors: doors}, nil
}

// GetShellyDevice returns a Shelly device for RPC calls (implements script.DeviceProvider)
func (dm *DeviceManager) GetShellyDevice(ctx context.Context, device *myhome.Device) (*shelly.Device, error) {
	sd, ok := device.Impl().(*shelly.Device)
	if !ok || sd == nil {
		impl, err := shelly.NewDeviceFromSummary(ctx, dm.log, device)
		if err != nil {
			return nil, err
		}
		sd, ok = impl.(*shelly.Device)
		if !ok {
			return nil, fmt.Errorf("unexpected device implementation type: %T", impl)
		}
	}
	return sd, nil
}
