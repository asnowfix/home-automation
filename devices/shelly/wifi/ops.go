package wifi

import (
	"devices/shelly/types"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler("WiFi", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
	})
	r.RegisterMethodHandler("WiFi", "SetConfig", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("WiFi", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	// System.SetConfig
}

func GetStatus(via types.Channel, d types.Device) (any, error) {
	out, err := d.CallE(via, "WiFi", "GetStatus", nil)
	if err != nil {
		log.Error(err, "Unable to get WiFi status")
		return nil, err
	}
	return out, nil
}

func ScanNetworks(via types.Channel, d types.Device) (any, error) {
	out, err := d.CallE(via, "WiFi", "Scan", nil)
	if err != nil {
		log.Error(err, "Unable to scan WiFi networks")
		return nil, err
	}
	return out, nil
}

func Connect(via types.Channel, d types.Device, ssid, password string) (any, error) {
	params := map[string]string{
		"ssid":     ssid,
		"password": password,
	}
	out, err := d.CallE(via, "WiFi", "Connect", params)
	if err != nil {
		log.Error(err, "Unable to connect to WiFi network")
		return nil, err
	}
	return out, nil
}

func Disconnect(via types.Channel, d types.Device) (any, error) {
	out, err := d.CallE(via, "WiFi", "Disconnect", nil)
	if err != nil {
		log.Error(err, "Unable to disconnect from WiFi network")
		return nil, err
	}
	return out, nil
}
