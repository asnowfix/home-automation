package system

import (
	"context"
	"fmt"
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	SetConfig Verb = "Sys.SetConfig"
	GetConfig Verb = "Sys.GetConfig"
	GetStatus Verb = "Sys.GetStatus"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(SetConfig.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(SetConfigRequest{}),
		Allocate:   func() any { return &SetConfigResponse{} },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return &Config{} },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return &Status{} },
		HttpMethod: http.MethodGet,
	})
}

func DoGetConfig(ctx context.Context, device types.Device) (*Config, error) {
	out, err := device.CallE(ctx, types.ChannelDefault, GetConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get config", "device", device.Id())
		return nil, err
	}
	config, ok := out.(*Config)
	if !ok {
		err = fmt.Errorf("invalid response to get device config: type='%v' expected='*system.Config'", reflect.TypeOf(out))
		log.Error(err, "Invalid response to get device config", "device", device.Id())
		return nil, err
	}
	return config, nil
}

func DoSetName(ctx context.Context, device types.Device, name string) (*SetConfigResponse, error) {
	log.Info("Setting name of device", "name", name, "device", device.Id())
	// c.Device.Name = name

	out, err := device.CallE(ctx, types.ChannelDefault, SetConfig.String(), &SetConfigRequest{
		Config: Config{
			Device: &DeviceConfig{
				Name: name,
			},
		},
	})
	if err != nil {
		log.Error(err, "Unable to set device name", "name", name, "device", device.Id())
		return nil, err
	}

	cres, ok := out.(*SetConfigResponse)
	if !ok {
		err = fmt.Errorf("invalid response to set device name: type='%v' expected='*system.SetConfigResponse'", reflect.TypeOf(out))
		log.Error(err, "Invalid response to set device name", "name", name, "device", device.Id())
		return nil, err
	}
	return cres, nil
}
