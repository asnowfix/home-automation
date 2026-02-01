package ble

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

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/BLE

type Verb string

func (v Verb) String() string {
	return string(v)
}

const (
	GetConfig                  Verb = "BLE.GetConfig"
	SetConfig                  Verb = "BLE.SetConfig"
	GetStatus                  Verb = "BLE.GetStatus"
	CloudRelayList             Verb = "BLE.CloudRelay.List"
	CloudRelayListInfos        Verb = "BLE.CloudRelay.ListInfos"
	StartBluTrvAssociations    Verb = "BLE.StartBluTrvAssociations"
)

// Init registers BLE component methods with the device method registrar
func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
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
	r.RegisterMethodHandler(CloudRelayList.String(), types.MethodHandler{
		Allocate:   func() any { return new(CloudRelayListResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(CloudRelayListInfos.String(), types.MethodHandler{
		Allocate:   func() any { return new(CloudRelayListInfosResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(StartBluTrvAssociations.String(), types.MethodHandler{
		Allocate:   func() any { return new(interface{}) }, // Returns null on success
		HttpMethod: http.MethodPost,
	})
}

// DoGetConfig retrieves the BLE configuration from the device
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

// DoSetConfig updates the BLE configuration on the device
func DoSetConfig(ctx context.Context, via types.Channel, device types.Device, config *Config) (*SetConfigResponse, error) {
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

// DoGetStatus retrieves the BLE status from the device
func DoGetStatus(ctx context.Context, via types.Channel, device types.Device) (*Status, error) {
	out, err := device.CallE(ctx, via, GetStatus.String(), nil)
	if err != nil {
		log.Error(err, "Unable to get device BLE status")
		return nil, err
	}
	res, ok := out.(*Status)
	if ok && res != nil {
		return res, nil
	}
	return nil, fmt.Errorf("invalid response to get device BLE status (type=%s, expected=%s)", reflect.TypeOf(out), reflect.TypeOf(Status{}))
}

// DoCloudRelayList retrieves the list of MAC addresses for devices managed by Cloud
func DoCloudRelayList(ctx context.Context, via types.Channel, device types.Device) (*CloudRelayListResponse, error) {
	out, err := device.CallE(ctx, via, string(CloudRelayList), nil)
	if err != nil {
		return nil, err
	}
	res, ok := out.(*CloudRelayListResponse)
	if !ok {
		return nil, fmt.Errorf("expected CloudRelayListResponse, got %T", out)
	}
	return res, nil
}

// DoCloudRelayListInfos retrieves extended information about devices managed by Cloud
func DoCloudRelayListInfos(ctx context.Context, via types.Channel, device types.Device, offset int) (*CloudRelayListInfosResponse, error) {
	req := &CloudRelayListInfosRequest{}
	if offset > 0 {
		req.Offset = offset
	}
	
	out, err := device.CallE(ctx, via, string(CloudRelayListInfos), req)
	if err != nil {
		return nil, err
	}
	res, ok := out.(*CloudRelayListInfosResponse)
	if !ok {
		return nil, fmt.Errorf("expected CloudRelayListInfosResponse, got %T", out)
	}
	return res, nil
}

// DoStartBluTrvAssociations starts BluTrv device associations
// Available only on devices that support BLUTRV devices (e.g., Shelly BLU Gateway Gen3)
func DoStartBluTrvAssociations(ctx context.Context, via types.Channel, device types.Device, req *StartBluTrvAssociationsRequest) error {
	_, err := device.CallE(ctx, via, string(StartBluTrvAssociations), req)
	return err
}
