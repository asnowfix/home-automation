package ethernet

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

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Eth>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	getConfig   Verb = "Eth.GetConfig"
	setConfig   Verb = "Eth.SetConfig"
	getStatus   Verb = "Eth.GetStatus"
	listClients Verb = "Eth.ListClients"
)

// SetConfigResponse represents the response from SetConfig operation
type SetConfigResponse struct {
	Success bool `json:"success"` // Whether the operation was successful
}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(getConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(setConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(SetConfigResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(getStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(listClients.String(), types.MethodHandler{
		Allocate:   func() any { return new(ListClientsResponse) },
		HttpMethod: http.MethodGet,
	})
}

// GetConfig retrieves the current Ethernet configuration
func GetConfig(ctx context.Context, device types.Device, via types.Channel) (*Config, error) {
	out, err := device.CallE(ctx, via, getConfig.String(), nil)
	if err != nil {
		return nil, err
	}
	return out.(*Config), nil
}

// SetConfig updates the Ethernet configuration
func SetConfig(ctx context.Context, device types.Device, via types.Channel, config *Config) error {
	_, err := device.CallE(ctx, via, setConfig.String(), config)
	return err
}

// Status retrieves the current Ethernet status
func GetStatus(ctx context.Context, device types.Device, via types.Channel) (*Status, error) {
	out, err := device.CallE(ctx, via, getStatus.String(), nil)
	if err != nil {
		return nil, err
	}
	return out.(*Status), nil
}

func DoGetStatus(ctx context.Context, via types.Channel, device types.Device) (*Status, error) {
	out, err := device.CallE(ctx, via, getStatus.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get device ethernet status")
		return nil, err
	}
	res, ok := out.(*Status)
	if ok && res != nil {
		return res, nil
	}
	return nil, fmt.Errorf("invalid response to get device ethernet status (type=%s, expected=%s)", reflect.TypeOf(out), reflect.TypeOf(Status{}))
}
