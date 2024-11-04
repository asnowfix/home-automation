package shelly

import (
	"devices/shelly/mqtt"
	shttp "devices/shelly/shttp"
	"devices/shelly/types"
	"fmt"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
)

func Init(log logr.Logger) {
	registrar.Init(log)
	shttp.Init(log, &registrar)
	mqtt.Init(log, &registrar)
}

var registrar Registrar

// return singleton registrar
func GetRegistrar() *Registrar {
	return &registrar
}

type Registrar struct {
	log      logr.Logger
	methods  map[string]map[string]types.MethodHandler
	channel  types.Channel
	channels []types.DeviceCaller
}

var listMethodsHandler = types.MethodHandler{
	Allocate:   func() any { return new(Methods) },
	HttpQuery:  map[string]string{},
	HttpMethod: http.MethodGet,
}

func (r *Registrar) Init(log logr.Logger) {
	r.log = log
	r.channel = types.ChannelHttp
	r.channels = make([]types.DeviceCaller, 3 /*sizeof(Channel)*/)

	r.methods = make(map[string]map[string]types.MethodHandler)
	r.RegisterMethodHandler("Shelly", "ListMethods", listMethodsHandler)
	// Shelly.PutTLSClientKey
	// Shelly.PutTLSClientCert
	// Shelly.PutUserCA
	// Shelly.SetAuth
	// Shelly.Update
	// Shelly.CheckForUpdate
	// Shelly.DetectLocation
	// Shelly.ListTimezones
	// Shelly.GetComponents
	// Shelly.GetStatus
	// Shelly.FactoryReset
	// Shelly.ResetWiFiConfig
	// Shelly.GetConfig
	r.RegisterMethodHandler("Shelly", "GetDeviceInfo", types.MethodHandler{
		Allocate: func() any { return new(DeviceInfo) },
		HttpQuery: map[string]string{
			"ident": "true",
		},
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Shelly", "Reboot", types.MethodHandler{
		Allocate:   func() any { return new(string) },
		HttpQuery:  map[string]string{},
		HttpMethod: http.MethodGet,
	})
}

func (r *Registrar) RegisterMethodHandler(c string, v string, m types.MethodHandler) {
	r.log.Info("Registering", "component", c, "verb", v)
	if _, exists := r.methods[c]; !exists {
		r.methods[c] = make(map[string]types.MethodHandler)
		r.log.Info("Added", "component", c)
	}
	if _, exists := r.methods[c][v]; !exists {
		r.methods[c][v] = m
		r.log.Info("Registered", "component", c, "verb", v, "http_method", m.HttpMethod, " http_query", m.HttpQuery)
	}
	m.Method = fmt.Sprintf("%s.%s", c, v)
	r.log.Info("Registered methods", "num", len(r.methods))
}

var ChannelRegistered = errors.Errorf("channel registration")

func (r *Registrar) RegisterDeviceCaller(ch types.Channel, dc types.DeviceCaller) {
	r.log.Info("Registering", "channel", ch, "caller", dc)

	// err := errors.New(fmt.Sprintf("Registering %v for channel %s", dc, ch))
	err := errors.Wrap(fmt.Errorf("registering %v for channel %s", dc, ch), "foo")
	// err := errors.New(ChannelRegistered)
	r.log.Error(err, "Registering", "channel", ch, "caller", dc)

	r.channels[ch] = dc
}

func (r *Registrar) CallE(d types.Device, ch types.Channel, mh types.MethodHandler, params any) (any, error) {
	out := mh.Allocate()
	r.log.Info("calling", "channel", ch, "method", mh.HttpMethod, "params", params, "out_type", reflect.TypeOf(out))
	return r.channels[ch](d, mh, out, params)
}
