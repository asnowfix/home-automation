package impl

import (
	"context"
	"fmt"
	"global"
	"myhome"
	"myhome/daemon/watch"
	mhd "myhome/devices"
	"myhome/groups"
	"myhome/storage"
	"mymqtt"
	"mynet"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/types"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
)

type DeviceManager struct {
	dr         mhd.DeviceRegistry
	gr         mhd.GroupRegistry
	update     chan *myhome.Device
	cancel     context.CancelFunc
	log        logr.Logger
	mqttClient *mymqtt.Client
	resolver   mynet.Resolver
}

func NewDeviceManager(ctx context.Context, s *storage.DeviceStorage, resolver mynet.Resolver, mqttClient *mymqtt.Client) *DeviceManager {
	log := ctx.Value(global.LogKey).(logr.Logger)
	return &DeviceManager{
		dr:         mhd.NewCache(ctx, s),
		gr:         s,
		log:        log.WithName("DeviceManager"),
		update:     make(chan *myhome.Device, 64), // TODO configurable buffer size
		mqttClient: mqttClient,
		resolver:   resolver,
	}
}

func (dm *DeviceManager) UpdateChannel() chan<- *myhome.Device {
	return dm.update
}

func (dm *DeviceManager) Start(ctx context.Context) error {
	var err error

	dm.log.Info("Starting device manager")

	myhome.RegisterMethodHandler(myhome.DevicesMatch, func(in any) (any, error) {
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
	myhome.RegisterMethodHandler(myhome.DeviceLookup, func(in any) (any, error) {
		name := in.(string)

		devices := make([]devices.Device, 0)
		device, err := dm.GetDeviceByAny(ctx, name)
		if err == nil {
			dm.log.Info("Found device by identifier", "identifier", name)
			devices = append(devices, device.DeviceSummary)
			return &devices, nil
		}
		dm.log.Info("Failed to get device by any identifier: trying group", "identifier", name)

		gd, err := dm.gr.GetDevicesByGroupName(name)
		if err == nil {
			dm.log.Info("Found devices by group name", "group", name)
			for _, d := range gd {
				devices = append(devices, d)
			}
			return &devices, nil
		}

		return nil, fmt.Errorf("failed to get device by group or any identifier: %v", err)
	})
	myhome.RegisterMethodHandler(myhome.DeviceShow, func(in any) (any, error) {
		return dm.GetDeviceByAny(ctx, in.(string))
	})
	myhome.RegisterMethodHandler(myhome.DeviceForget, func(in any) (any, error) {
		return nil, dm.ForgetDevice(ctx, in.(string))
	})
	myhome.RegisterMethodHandler(myhome.GroupList, func(in any) (any, error) {
		return dm.gr.GetAllGroups()
	})
	myhome.RegisterMethodHandler(myhome.GroupCreate, func(in any) (any, error) {
		return dm.gr.AddGroup(in.(*myhome.GroupInfo))
	})
	myhome.RegisterMethodHandler(myhome.GroupDelete, func(in any) (any, error) {
		return dm.gr.RemoveGroup(in.(string))
	})
	myhome.RegisterMethodHandler(myhome.GroupShow, func(in any) (any, error) {
		name := in.(string)

		gi, err := dm.gr.GetGroupInfo(in.(string))
		if err != nil {
			dm.log.Error(err, "Failed to get group info", "group", name)
			return nil, err
		}

		gd, err := dm.gr.GetDevicesByGroupName(name)
		if err != nil {
			dm.log.Error(err, "Failed to get devices for group", "group", name)
			return nil, err
		}

		g := myhome.Group{
			GroupInfo: *gi,
			Devices:   make([]myhome.DeviceSummary, 0),
		}
		for _, d := range gd {
			g.Devices = append(g.Devices, d.DeviceSummary)
		}

		return &g, nil
	})
	myhome.RegisterMethodHandler(myhome.GroupAddDevice, func(in any) (any, error) {
		return dm.gr.AddDeviceToGroup(in.(*myhome.GroupDevice))
	})
	myhome.RegisterMethodHandler(myhome.GroupRemoveDevice, func(in any) (any, error) {
		return dm.gr.RemoveDeviceFromGroup(in.(*myhome.GroupDevice))
	})

	ctx, dm.cancel = context.WithCancel(ctx)
	go func(ctx context.Context, log logr.Logger, dc <-chan *myhome.Device) error {
		log.Info("Starting updater loop")
		for {
			select {
			case <-ctx.Done():
				log.Info("Exiting updater loop")
				return ctx.Err()

			case device := <-dc:
				log.Info("Updater loop: processing", "device", device.DeviceSummary)

				sd, ok := device.Impl().(*shelly.Device)
				if !ok {
					log.Error(nil, "Unhandled device type", "device", device.DeviceSummary, "type", reflect.TypeOf(device.Impl()))
					continue
				}

				// //  TODO: Load KVS revision from device & compare it with the one in the DB. If they are different, update the device in the DB.
				// status := system.GetStatus(ctx, sd)

				// kvsRevision, err := kvs.GetRevision(ctx, sd)
				// if err != nil {
				// 	dm.log.Error(err, "Failed to get device KVS revision", "id", device.Id(), "name", device.Name())
				// 	continue
				// }
				// YYY

				groups, err := groups.GetDeviceGroups(ctx, sd)
				if err != nil {
					dm.log.Error(err, "Failed to get device groups", "device", device.DeviceSummary)
					continue
				}
				if len(groups) > 0 {
					dm.log.Info("Device claims to be in groups", "device", device.DeviceSummary, "groups", groups)

					for _, group := range groups {
						out, err := dm.gr.AddGroup(&myhome.GroupInfo{
							Name: group,
						})
						if err != nil {
							dm.log.Error(err, "Failed to add group", "group", group)
						}
						gi := out.(*myhome.GroupInfo)
						_, err = dm.gr.AddDeviceToGroup(&myhome.GroupDevice{
							Group:        gi.Name,
							Manufacturer: device.Manufacturer(),
							Id:           device.Id(),
						})
						if err != nil {
							dm.log.Error(err, "Failed to add device to group", "device", device.DeviceSummary, "group", group)
							continue
						}
						// Add GroupInfo Key/Values to device
						for k, v := range gi.KeyValues() {
							kvs.SetKeyValue(ctx, dm.log, types.ChannelDefault, sd, k, v)
						}
					}
				}

				modified, err := device.Refresh(ctx)
				if err != nil {
					dm.log.Error(err, "Failed to refresh device", "device", device.DeviceSummary)
					continue
				}
				if !modified {
					dm.log.Info("Device is up to date", "device", device.DeviceSummary)
					continue
				}

				err = dm.dr.SetDevice(ctx, device, true)
				if err != nil {
					dm.log.Error(err, "Failed to upsert", "device", device.DeviceSummary)
					continue
				}
				dm.log.Info("Updated", "device", device.DeviceSummary)
			}
		}
	}(ctx, dm.log.WithName("DeviceManager#Updater"), dm.update)

	// Load every devices from storage & init them
	devices, err := dm.dr.GetAllDevices(ctx)
	if err != nil {
		dm.log.Error(err, "Failed to get all devices")
		return err
	}
	dm.log.Info("Loaded devices", "num", len(devices))
	for _, device := range devices {
		if device.Id_ == "" {
			dm.log.Info("Skipping update of device without Id", "device", device)
			continue
		} else {
			dm.log.Info("Preparing update of device", "id", device.Id(), "name", device.Name())
			sd, err := shelly.NewDeviceFromSummary(ctx, dm.log, device)
			if err != nil {
				dm.log.Error(err, "Failed to create device from summary", "id", device.Id(), "name", device.Name())
				continue
			}
			dm.update <- device.WithImpl(sd)
		}
	}

	// Loop on MQTT event devices discovery
	err = watch.Mqtt(ctx, dm.mqttClient, dm, dm)
	if err != nil {
		dm.log.Error(err, "Failed to watch MQTT events")
		return err
	}

	// Loop on ZeroConf devices discovery
	err = watch.ZeroConf(ctx, dm, dm.dr, dm.resolver)
	if err != nil {
		dm.log.Error(err, "Failed to watch ZeroConf devices")
		return err
	}

	return nil
}

func (dm *DeviceManager) Flush() error {
	err := dm.dr.Flush()
	if err != nil {
		dm.log.Error(err, "Failed to flush device storage")
		return err
	}
	err = dm.gr.Flush()
	if err != nil {
		dm.log.Error(err, "Failed to flush group storage")
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

func (dm *DeviceManager) SetDevice(ctx context.Context, d *myhome.Device, overwrite bool) error {
	if d.Manufacturer_ == string(myhome.Shelly) {
		sd, ok := d.Impl().(*shelly.Device)
		if !ok {
			return fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(d.Impl()), d)
		}
		groups, err := dm.gr.GetDeviceGroups(d.Manufacturer(), d.Id())
		if err != nil {
			return err
		}
		for _, group := range groups.Groups {
			_, err := kvs.SetKeyValue(ctx, dm.log, types.ChannelMqtt, sd, fmt.Sprintf("group/%s", group.Name), "true")
			if err != nil {
				return err
			}
		}
		d.WithImpl(sd)
	}
	return dm.dr.SetDevice(ctx, d, overwrite)
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
