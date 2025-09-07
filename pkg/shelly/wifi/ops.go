package wifi

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

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	GetConfig     Verb = "Wifi.GetConfig"
	SetConfig     Verb = "Wifi.SetConfig"
	GetStatus     Verb = "Wifi.GetStatus"
	Scan          Verb = "Wifi.Scan"
	ListAPClients Verb = "Wifi.ListAPClients"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(SetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(SetConfigResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Scan.String(), types.MethodHandler{
		Allocate:   func() any { return new(ScanResult) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(ListAPClients.String(), types.MethodHandler{
		Allocate:   func() any { return new(ListAPClientsResult) },
		HttpMethod: http.MethodGet,
	})
}

func DoGetConfig(ctx context.Context, via types.Channel, device types.Device) (*Config, error) {
	out, err := device.CallE(ctx, via, string(GetConfig), nil)
	if err != nil {
		return nil, err
	}
	res, ok := out.(*Config)
	if !ok {
		return nil, fmt.Errorf("expected Config, got %T", out)
	}
	return res, nil
}

func DoSetConfig(ctx context.Context, via types.Channel, device types.Device, config *Config) (any, error) {
	out, err := device.CallE(ctx, via, string(SetConfig), &SetConfigRequest{
		Config: *config,
	})
	if err != nil {
		return nil, err
	}
	res, ok := out.(*SetConfigResponse)
	if !ok {
		return nil, fmt.Errorf("expected SetConfigResponse, got %T", out)
	}
	return res, nil
}

func DoGetStatus(ctx context.Context, via types.Channel, device types.Device) (*Status, error) {
	out, err := device.CallE(ctx, via, GetStatus.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get device wifi status")
		return nil, err
	}
	res, ok := out.(*Status)
	if ok && res != nil {
		return res, nil
	}
	return nil, fmt.Errorf("invalid response to get device wifi status (type=%s, expected=%s)", reflect.TypeOf(out), reflect.TypeOf(Status{}))
}
