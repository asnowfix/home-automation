package sswitch

import (
	"context"
	"fmt"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/kvs"
	pkgshelly "pkg/shelly/shelly"
	pkgsswitch "pkg/shelly/sswitch"
	"pkg/shelly/types"
	"strconv"

	"github.com/go-logr/logr"
)

// DeviceProvider interface for getting devices
type DeviceProvider interface {
	GetDeviceByAny(ctx context.Context, identifier string) (*myhome.Device, error)
	GetShellyDevice(ctx context.Context, device *myhome.Device) (*shelly.Device, error)
}

// Service handles switch RPC methods
type Service struct {
	log      logr.Logger
	provider DeviceProvider
}

// NewService creates a new switch service
func NewService(log logr.Logger, provider DeviceProvider) *Service {
	return &Service{
		log:      log.WithName("SwitchService"),
		provider: provider,
	}
}

// RegisterHandlers registers the switch RPC handlers
func (s *Service) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.SwitchToggle, func(ctx context.Context, in any) (any, error) {
		return s.HandleToggle(ctx, in.(*myhome.SwitchParams))
	})
	myhome.RegisterMethodHandler(myhome.SwitchOn, func(ctx context.Context, in any) (any, error) {
		return s.HandleOn(ctx, in.(*myhome.SwitchParams))
	})
	myhome.RegisterMethodHandler(myhome.SwitchOff, func(ctx context.Context, in any) (any, error) {
		return s.HandleOff(ctx, in.(*myhome.SwitchParams))
	})
	myhome.RegisterMethodHandler(myhome.SwitchStatus, func(ctx context.Context, in any) (any, error) {
		return s.HandleStatus(ctx, in.(*myhome.SwitchParams))
	})
	myhome.RegisterMethodHandler(myhome.SwitchAll, func(ctx context.Context, in any) (any, error) {
		return s.HandleAll(ctx, in.(*myhome.SwitchAllParams))
	})
}

// HandleToggle handles switch.toggle RPC method
func (s *Service) HandleToggle(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchToggleResult, error) {
	device, sd, err := s.getShellyDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	// Ensure device has a communication channel ready
	// For devices without IP, MQTT must be initialized
	if !sd.IsMqttReady() && !sd.IsHttpReady() {
		// Refresh will initialize MQTT if needed
		_, refreshErr := sd.Refresh(ctx, types.ChannelDefault)
		if refreshErr != nil {
			s.log.Error(refreshErr, "Failed to refresh device (MQTT init)", "device_id", device.Id())
			// Continue anyway - the CallE will fail with a better error if channel is still not ready
		}
	}

	out, err := sd.CallE(ctx, types.ChannelDefault, pkgsswitch.Toggle.String(), &pkgsswitch.ToggleStatusRequest{Id: params.SwitchId})
	if err != nil {
		return nil, fmt.Errorf("failed to toggle switch on device %s: %w", sd.Id(), err)
	}

	result, ok := out.(*pkgsswitch.ToogleSetResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T", out)
	}

	return &myhome.SwitchToggleResult{
		DeviceID:   device.Id(),
		DeviceName: device.Name(),
		Result:     result,
	}, nil
}

// HandleOn handles switch.on RPC method
func (s *Service) HandleOn(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchOnOffResult, error) {
	device, sd, err := s.getShellyDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	off := s.offValue(ctx, sd)
	out, err := sd.CallE(ctx, types.ChannelDefault, pkgsswitch.Set.String(), &pkgsswitch.SetRequest{Id: params.SwitchId, On: !off})
	if err != nil {
		return nil, fmt.Errorf("failed to turn on switch on device %s: %w", sd.Id(), err)
	}

	result, ok := out.(*pkgsswitch.ToogleSetResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T", out)
	}

	return &myhome.SwitchOnOffResult{
		DeviceID:   device.Id(),
		DeviceName: device.Name(),
		Result:     result,
	}, nil
}

// HandleOff handles switch.off RPC method
func (s *Service) HandleOff(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchOnOffResult, error) {
	device, sd, err := s.getShellyDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	off := s.offValue(ctx, sd)
	out, err := sd.CallE(ctx, types.ChannelDefault, pkgsswitch.Set.String(), &pkgsswitch.SetRequest{Id: params.SwitchId, On: off})
	if err != nil {
		return nil, fmt.Errorf("failed to turn off switch on device %s: %w", sd.Id(), err)
	}

	result, ok := out.(*pkgsswitch.ToogleSetResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T", out)
	}

	return &myhome.SwitchOnOffResult{
		DeviceID:   device.Id(),
		DeviceName: device.Name(),
		Result:     result,
	}, nil
}

// HandleStatus handles switch.status RPC method
func (s *Service) HandleStatus(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchStatusResult, error) {
	device, sd, err := s.getShellyDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	out, err := sd.CallE(ctx, types.ChannelDefault, pkgsswitch.GetStatus.String(), &pkgsswitch.ToggleStatusRequest{Id: params.SwitchId})
	if err != nil {
		return nil, fmt.Errorf("failed to get switch status on device %s: %w", sd.Id(), err)
	}

	status, ok := out.(*pkgsswitch.Status)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T", out)
	}

	return &myhome.SwitchStatusResult{
		DeviceID:   device.Id(),
		DeviceName: device.Name(),
		Status:     status,
	}, nil
}

// HandleAll handles switch.all RPC method
func (s *Service) HandleAll(ctx context.Context, params *myhome.SwitchAllParams) (*myhome.SwitchAllResult, error) {
	device, sd, err := s.getShellyDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	switches, err := pkgshelly.GetSwitchesSummary(ctx, sd)
	if err != nil {
		return nil, fmt.Errorf("failed to get switches summary on device %s: %w", sd.Id(), err)
	}

	return &myhome.SwitchAllResult{
		DeviceID:   device.Id(),
		DeviceName: device.Name(),
		Switches:   switches,
	}, nil
}

// getShellyDevice resolves a device identifier to a myhome.Device and shelly.Device
func (s *Service) getShellyDevice(ctx context.Context, identifier string) (*myhome.Device, *shelly.Device, error) {
	device, err := s.provider.GetDeviceByAny(ctx, identifier)
	if err != nil {
		return nil, nil, fmt.Errorf("device not found: %w", err)
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get shelly device: %w", err)
	}

	return device, sd, nil
}

// offValue checks the normally-closed KVS key to determine the "off" value for a device
func (s *Service) offValue(ctx context.Context, sd *shelly.Device) bool {
	out, err := sd.CallE(ctx, types.ChannelDefault, kvs.Get.String(), pkgsswitch.NormallyClosedKey)
	if err != nil {
		s.log.Info("Unable to get value", "key", pkgsswitch.NormallyClosedKey, "reason", err)
		return false
	}
	kv, ok := out.(*kvs.GetResponse)
	if !ok {
		s.log.Error(nil, "Invalid value", "key", pkgsswitch.NormallyClosedKey, "value", out)
		return false
	}
	off, err := strconv.ParseBool(kv.Value)
	if err != nil {
		s.log.Error(err, "Invalid value", "key", pkgsswitch.NormallyClosedKey, "value", kv.Value)
		return false
	}
	return off
}
