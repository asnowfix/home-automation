package matter

import (
	"context"
	"net/http"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Matter

type Verb string

func (v Verb) String() string {
	return string(v)
}

const (
	setConfig    Verb = "Matter.SetConfig"
	getConfig    Verb = "Matter.GetConfig"
	getStatus    Verb = "Matter.GetStatus"
	getSetupCode Verb = "Matter.GetSetupCode"
	factoryReset Verb = "Matter.FactoryReset"
)

var log logr.Logger

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", "pkg/shelly/matter")
	
	r.RegisterMethodHandler(setConfig.String(), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(getConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(getStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(getSetupCode.String(), types.MethodHandler{
		Allocate:   func() any { return new(SetupCode) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(factoryReset.String(), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
}

// GetConfig retrieves the Matter configuration
func GetConfig(ctx context.Context, via types.Channel, device types.Device) (*Config, error) {
	out, err := device.CallE(ctx, via, getConfig.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get Matter config", "device", device.Id())
		return nil, err
	}
	return out.(*Config), nil
}

// SetConfig sets the Matter configuration
func SetConfig(ctx context.Context, via types.Channel, device types.Device, config *Config) error {
	req := SetConfigRequest{Config: *config}
	_, err := device.CallE(ctx, via, setConfig.String(), &req)
	if err != nil {
		log.Error(err, "Unable to set Matter config", "device", device.Id())
		return err
	}
	return nil
}

// GetStatus retrieves the Matter status
func GetStatus(ctx context.Context, via types.Channel, device types.Device) (*Status, error) {
	out, err := device.CallE(ctx, via, getStatus.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get Matter status", "device", device.Id())
		return nil, err
	}
	return out.(*Status), nil
}

// GetSetupCode retrieves the Matter setup codes (QR and manual)
func GetSetupCode(ctx context.Context, via types.Channel, device types.Device) (*SetupCode, error) {
	out, err := device.CallE(ctx, via, getSetupCode.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get Matter setup code", "device", device.Id())
		return nil, err
	}
	return out.(*SetupCode), nil
}

// DoFactoryReset performs a Matter factory reset (device will reboot)
func DoFactoryReset(ctx context.Context, via types.Channel, device types.Device) error {
	_, err := device.CallE(ctx, via, factoryReset.String(), nil)
	if err != nil {
		log.Error(err, "Unable to perform Matter factory reset", "device", device.Id())
		return err
	}
	return nil
}

// Disable disables the Matter component
func Disable(ctx context.Context, via types.Channel, device types.Device) error {
	cfg := &Config{Enable: false}
	return SetConfig(ctx, via, device, cfg)
}

// Enable enables the Matter component
func Enable(ctx context.Context, via types.Channel, device types.Device) error {
	cfg := &Config{Enable: true}
	return SetConfig(ctx, via, device, cfg)
}
