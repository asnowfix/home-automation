package shelly

import (
	"context"
	"net/http"
	"pkg/shelly/input"
	"pkg/shelly/kvs"
	"pkg/shelly/mqtt"
	"pkg/shelly/script"
	shttp "pkg/shelly/shttp"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"reflect"
	"schedule"
	"time"

	"github.com/go-logr/logr"
)

type Verb string

const (
	GetDeviceInfo  Verb = "Shelly.GetDeviceInfo"
	ListMethods    Verb = "Shelly.ListMethods"
	Reboot         Verb = "Shelly.Reboot"
	SetConfig      Verb = "Shelly.SetConfig"
	GetConfig      Verb = "Shelly.GetConfig"
	CheckForUpdate Verb = "Shelly.CheckForUpdate"
	GetStatus      Verb = "Shelly.GetStatus"
	GetComponents  Verb = "Shelly.GetComponents"
)

func Init(log logr.Logger, timeout time.Duration) {
	registrar.Init(log)
	system.Init(log, &registrar)
	input.Init(log, &registrar)
	mqtt.Init(log, &registrar, timeout)
	schedule.Init(log, &registrar)
	script.Init(log, &registrar)
	shttp.Init(log, &registrar)
	sswitch.Init(log, &registrar)
	kvs.Init(log, &registrar)
}

var registrar Registrar

// return singleton registrar
func GetRegistrar() *Registrar {
	return &registrar
}

type Registrar struct {
	log      logr.Logger
	methods  map[string]types.MethodHandler
	channel  types.Channel
	channels []types.DeviceCaller
}

func (r *Registrar) Init(log logr.Logger) {
	r.log = log
	r.channel = types.ChannelHttp
	r.channels = make([]types.DeviceCaller, 3 /*sizeof(Channel)*/)

	r.methods = make(map[string]types.MethodHandler)
	r.RegisterMethodHandler(string(ListMethods), types.MethodHandler{
		// Method:     ListMethods,
		Allocate:   func() any { return new(MethodsResponse) },
		HttpMethod: http.MethodGet,
	})
	// Shelly.PutTLSClientKey
	// Shelly.PutTLSClientCert
	// Shelly.PutUserCA
	// Shelly.SetAuth
	// Shelly.Update
	// Shelly.CheckForUpdate
	// Shelly.DetectLocation
	// Shelly.ListTimezones
	r.RegisterMethodHandler(string(GetComponents), types.MethodHandler{
		// InputType:  reflect.TypeOf(ComponentsRequest{}),
		Allocate:   func() any { return new(ComponentsResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(GetStatus), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	// Shelly.FactoryReset
	// Shelly.ResetWiFiConfig
	r.RegisterMethodHandler(string(GetConfig), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(GetDeviceInfo), types.MethodHandler{
		Allocate:   func() any { return new(DeviceInfo) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(Reboot), types.MethodHandler{
		Allocate:   func() any { return new(string) },
		HttpMethod: http.MethodGet,
	})
}

func (r *Registrar) MethodHandler(m string) types.MethodHandler {
	return r.methods[m]
}

func (r *Registrar) RegisterMethodHandler(v string, m types.MethodHandler) {
	if m.Allocate == nil {
		m.Allocate = func() any {
			return make(map[string]interface{})
		}
	}
	r.methods[v] = m
}

func (r *Registrar) RegisterDeviceCaller(ch types.Channel, dc types.DeviceCaller) {
	r.log.Info("Registering", "channel", ch, "caller", dc)

	// err := errors.Wrap(fmt.Errorf("stack registering %v for channel %s", dc, ch), "registering")
	// r.log.Error(err, "Registering", "channel", ch, "caller", dc)

	r.channels[ch] = dc
}

func (r *Registrar) CallE(ctx context.Context, d types.Device, ch types.Channel, mh types.MethodHandler, params any) (any, error) {
	out := mh.Allocate()
	// r.log.Info("Calling", "channel", ch, "method", mh.Method, "http_method", mh.HttpMethod, "params", params, "out_type", reflect.TypeOf(out))
	r.log.Info("Calling", "channel", ch, "params", params, "out_type", reflect.TypeOf(out))
	return r.channels[ch](ctx, d, mh, out, params)
}
