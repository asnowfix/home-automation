package sswitch

import (
	"context"
	"encoding/json"
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
		log:      log.WithName("Switch"),
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
func (s *Service) HandleToggle(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchResult, error) {
	device, sd, err := s.getDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	on := s.onValue(ctx, sd, params.SwitchId)

	result, err := pkgsswitch.Toggle(ctx, sd, params.SwitchId)
	if err != nil {
		return nil, err
	}

	return s.returnStatus(ctx, s.log.WithName("Toggle"), device, sd, params.SwitchId, on, !result.WasOn)
}

// HandleOn handles switch.on RPC method
func (s *Service) HandleOn(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchResult, error) {
	device, sd, err := s.getDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	on := s.onValue(ctx, sd, params.SwitchId)

	_, err = pkgsswitch.Set(ctx, sd, params.SwitchId, true)
	if err != nil {
		return nil, fmt.Errorf("failed to turn on switch on device %s: %w", sd.Id(), err)
	}

	return s.returnStatus(ctx, s.log.WithName("On"), device, sd, params.SwitchId, on, true)
}

// HandleOff handles switch.off RPC method
func (s *Service) HandleOff(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchResult, error) {
	device, sd, err := s.getDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	on := s.onValue(ctx, sd, params.SwitchId)

	_, err = pkgsswitch.Set(ctx, sd, params.SwitchId, false)
	if err != nil {
		return nil, fmt.Errorf("failed to turn off switch on device %s: %w", sd.Id(), err)
	}

	return s.returnStatus(ctx, s.log.WithName("Off"), device, sd, params.SwitchId, on, false)
}

// HandleStatus handles switch.status RPC method
func (s *Service) HandleStatus(ctx context.Context, params *myhome.SwitchParams) (*myhome.SwitchResult, error) {
	device, sd, err := s.getDevice(ctx, params.Identifier)
	if err != nil {
		return nil, err
	}

	status, err := pkgsswitch.GetStatus(ctx, sd, params.SwitchId)
	if err != nil {
		return nil, fmt.Errorf("failed to get switch status on device %s: %w", sd.Id(), err)
	}

	on := s.onValue(ctx, sd, params.SwitchId)

	return s.returnStatus(ctx, s.log.WithName("Status"), device, sd, params.SwitchId, on, status.Output)
}

// HandleAll handles switch.all RPC method
func (s *Service) HandleAll(ctx context.Context, params *myhome.SwitchAllParams) (*myhome.SwitchAllResult, error) {
	device, sd, err := s.getDevice(ctx, params.Identifier)
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

func (s *Service) getDevice(ctx context.Context, identifier string) (*myhome.Device, *shelly.Device, error) {
	device, err := s.provider.GetDeviceByAny(ctx, identifier)
	if err != nil {
		return nil, nil, fmt.Errorf("device not found: %w", err)
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get shelly device: %w", err)
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

	return device, sd, nil
}

func (s *Service) returnStatus(_ context.Context, log logr.Logger, device *myhome.Device, sd *shelly.Device, switchId int, on bool, newState bool) (*myhome.SwitchResult, error) {
	sr := myhome.SwitchResult{
		DeviceID:   device.Id(),
		DeviceName: device.Name(),
		SwitchId:   switchId,
		On:         newState == on,
	}
	log.Info("New state", "switch", sr, "on_value", on)
	return &sr, nil
}

// onValue checks the normally-closed KVS key to determine the "on" value for a device
func (s *Service) onValue(ctx context.Context, sd *shelly.Device, switchId int) bool {
	kv, err := kvs.GetValue(ctx, s.log, types.ChannelDefault, sd, string(myhome.NormallyClosedKey))
	if err != nil {
		s.log.Info("Unable to get value", "key", string(myhome.NormallyClosedKey), "reason", err)
		return true
	}

	// if switchId is 0, the value might be a boolean.  Otherwise, it is an array indexed by the switchId
	var nc bool
	if switchId == 0 {
		nc, _ = strconv.ParseBool(kv.Value)
	} else {
		var ncs []bool
		err = json.Unmarshal([]byte(kv.Value), &ncs)
		if err != nil {
			nc = false
		}
		nc = ncs[switchId]
	}
	return !nc
}
