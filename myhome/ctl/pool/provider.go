package pool

import (
	"context"
	"fmt"
	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// poolProvider implements the DeviceProvider interface from internal/myhome/shelly/script
type poolProvider struct{}

func (p *poolProvider) GetDeviceByAny(ctx context.Context, identifier string) (*myhome.Device, error) {
	// Use myhome.TheClient.LookupDevices to find the device
	devices, err := myhome.TheClient.LookupDevices(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup device %s: %w", identifier, err)
	}
	if len(*devices) == 0 {
		return nil, fmt.Errorf("device not found: %s", identifier)
	}

	// Return the first matching device
	device := (*devices)[0]

	// Convert pkg/devices.Device to myhome.Device
	mac := ""
	if device.Mac() != nil {
		mac = device.Mac().String()
	}

	return &myhome.Device{
		DeviceSummary: myhome.DeviceSummary{
			DeviceIdentifier: myhome.DeviceIdentifier{
				Manufacturer_: device.Manufacturer(),
				Id_:           device.Id(),
			},
			MAC:   mac,
			Host_: device.Host(),
			Name_: device.Name(),
		},
	}, nil
}

func (p *poolProvider) GetShellyDevice(ctx context.Context, device *myhome.Device) (*shelly.Device, error) {
	// Use myhome.Foreach to get a properly initialized Shelly device
	// This ensures the device has all necessary information loaded
	var shellyDevice *shelly.Device
	var deviceErr error

	_, err := myhome.Foreach(ctx, hlog.Logger, device.Id(), types.ChannelDefault, func(ctx context.Context, log logr.Logger, via types.Channel, d devices.Device, args []string) (any, error) {
		sd, ok := d.(*shelly.Device)
		if !ok {
			deviceErr = fmt.Errorf("device is not a Shelly device")
			return nil, deviceErr
		}
		shellyDevice = sd
		return nil, nil
	}, nil)

	if err != nil {
		return nil, fmt.Errorf("failed to lookup shelly device: %w", err)
	}
	if deviceErr != nil {
		return nil, deviceErr
	}
	if shellyDevice == nil {
		return nil, fmt.Errorf("device not found: %s", device.Id())
	}

	return shellyDevice, nil
}
