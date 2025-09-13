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
	setConfig Verb = "Sys.SetConfig"
	getConfig Verb = "Sys.GetConfig"
	getStatus Verb = "Sys.GetStatus"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(setConfig.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(SetConfigRequest{}),
		Allocate:   func() any { return &SetConfigResponse{} },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(getConfig.String(), types.MethodHandler{
		Allocate:   func() any { return &Config{} },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(getStatus.String(), types.MethodHandler{
		Allocate:   func() any { return &Status{} },
		HttpMethod: http.MethodGet,
	})
}

func GetConfig(ctx context.Context, via types.Channel, device types.Device) (*Config, error) {
	out, err := device.CallE(ctx, via, getConfig.String(), nil)
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

	// FIXME: unconfigured RPC UDP should be `null` but comes as empty string
	if config.RpcUdp != nil && config.RpcUdp.DestinationAddress == "" {
		config.RpcUdp = nil
	}

	return config, nil
}

func SetConfig(ctx context.Context, via types.Channel, device types.Device, config *Config) (*SetConfigResponse, error) {
	var req SetConfigRequest
	req.Config = *config
	out, err := device.CallE(ctx, via, setConfig.String(), &req)
	if err != nil {
		log.Error(err, "Unable to set config", "device", device.Id())
		return nil, err
	}
	res, ok := out.(*SetConfigResponse)
	if !ok {
		err = fmt.Errorf("unexpected response type: got %v, expected %v", reflect.TypeOf(out), reflect.TypeOf(&SetConfigResponse{}))
		log.Error(err, "Unexpected response type", "device", device.Id(), "response", out)
		return nil, err
	}
	return res, nil
}

func GetStatus(ctx context.Context, via types.Channel, device types.Device) (*Status, error) {
	out, err := device.CallE(ctx, via, getStatus.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get status", "device", device.Id())
		return nil, err
	}
	status, ok := out.(*Status)
	if !ok {
		err = fmt.Errorf("invalid response to get device status: type='%v' expected='*system.Status'", reflect.TypeOf(out))
		log.Error(err, "Invalid response to get device status", "device", device.Id())
		return nil, err
	}
	return status, nil
}
