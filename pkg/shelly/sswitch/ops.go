package sswitch

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

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	getConfig Verb = "Switch.GetConfig"
	setConfig Verb = "Switch.SetConfig"
	getStatus Verb = "Switch.GetStatus"
	toggle    Verb = "Switch.Toggle"
	set       Verb = "Switch.Set"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())

	r.RegisterMethodHandler(getConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(setConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(getStatus.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(ToggleStatusRequest{}),
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(toggle.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(ToggleStatusRequest{}),
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(set.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(SetRequest{}),
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodPost,
	})
}

func doCall[reqT any, resT any](ctx context.Context, device types.Device, verb Verb, req *reqT) (*resT, error) {
	out, err := device.CallE(ctx, types.ChannelDefault, verb.String(), req)
	if err != nil {
		return nil, fmt.Errorf("failed to call %s on device %s: %w", verb, device.Id(), err)
	}

	result, ok := out.(*resT)
	if !ok {
		var expected resT
		return nil, fmt.Errorf("unexpected response type %T (should be *%T)", out, expected)
	}
	return result, nil
}

func Toggle(ctx context.Context, device types.Device, id int) (*ToogleSetResponse, error) {
	return doCall[ToggleStatusConfigRequest, ToogleSetResponse](ctx, device, toggle, &ToggleStatusConfigRequest{Id: id})
}

func Set(ctx context.Context, device types.Device, id int, on bool) (*ToogleSetResponse, error) {
	return doCall[SetRequest, ToogleSetResponse](ctx, device, set, &SetRequest{Id: id, On: on})
}

func GetStatus(ctx context.Context, device types.Device, id int) (*Status, error) {
	return doCall[ToggleStatusConfigRequest, Status](ctx, device, getStatus, &ToggleStatusConfigRequest{Id: id})
}

func GetConfig(ctx context.Context, device types.Device, id int) (*Config, error) {
	return doCall[ToggleStatusConfigRequest, Config](ctx, device, getConfig, &ToggleStatusConfigRequest{Id: id})
}

func SetConfig(ctx context.Context, device types.Device, id int, config *Config) (*ConfigurationResponse, error) {
	return doCall[ConfigurationRequest, ConfigurationResponse](ctx, device, setConfig, &ConfigurationRequest{Id: id, Configuration: *config})
}
