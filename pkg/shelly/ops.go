package shelly

import (
	"context"
	"fmt"
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

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	GetStatus        Verb = "Shelly.GetStatus"
	GetConfig        Verb = "Shelly.GetConfig"
	ListMethods      Verb = "Shelly.ListMethods"
	GetDeviceInfo    Verb = "Shelly.GetDeviceInfo"
	ListProfiles     Verb = "Shelly.ListProfiles"
	SetProfile       Verb = "Shelly.SetProfile"
	ListTimezones    Verb = "Shelly.ListTimezones"
	DetectLocation   Verb = "Shelly.DetectLocation"
	CheckForUpdate   Verb = "Shelly.CheckForUpdate"
	Update           Verb = "Shelly.Update"
	FactoryReset     Verb = "Shelly.FactoryReset"
	ResetWiFiConfig  Verb = "Shelly.ResetWiFiConfig"
	Reboot           Verb = "Shelly.Reboot"
	SetAuth          Verb = "Shelly.SetAuth"
	PutUserCA        Verb = "Shelly.PutUserCA"
	PutTLSClientCert Verb = "Shelly.PutTLSClientCert"
	PutTLSClientKey  Verb = "Shelly.PutTLSClientKey"
	GetComponents    Verb = "Shelly.GetComponents"
)

type empty struct{}

func Init(log logr.Logger, timeout time.Duration) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	registrar.Init(log)

	registrar.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	registrar.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	registrar.RegisterMethodHandler(ListMethods.String(), types.MethodHandler{
		Allocate:   func() any { return new(MethodsResponse) },
		HttpMethod: http.MethodGet,
	})
	registrar.RegisterMethodHandler(GetDeviceInfo.String(), types.MethodHandler{
		Allocate:   func() any { return new(DeviceInfo) },
		HttpMethod: http.MethodGet,
	})

	// TODO complete the lsit of handlers

	registrar.RegisterMethodHandler(GetComponents.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(ComponentsRequest{}),
		Allocate:   func() any { return new(ComponentsResponse) },
		HttpMethod: http.MethodPost,
	})
	registrar.RegisterMethodHandler(Reboot.String(), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
	registrar.RegisterMethodHandler(CheckForUpdate.String(), types.MethodHandler{
		Allocate:   func() any { return new(CheckForUpdateResponse) },
		HttpMethod: http.MethodGet,
	})
	registrar.RegisterMethodHandler(Reboot.String(), types.MethodHandler{
		Allocate:   func() any { return 0 },
		HttpMethod: http.MethodGet,
	})
	registrar.RegisterMethodHandler(FactoryReset.String(), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})

	// TODO complete the lsit of handlers

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
	methods  map[any]types.MethodHandler
	channel  types.Channel
	channels []types.DeviceCaller
}

func (r *Registrar) Init(log logr.Logger) {
	r.log = log
	r.channel = types.ChannelHttp
	r.channels = make([]types.DeviceCaller, 3 /*sizeof(Channel)*/)
	r.methods = make(map[any]types.MethodHandler)
}

func (r *Registrar) MethodHandlerE(m any) (types.MethodHandler, error) {
	mh, ok := r.methods[m]
	if !ok {
		return types.MethodHandler{}, fmt.Errorf("method not found in registrar: %s", m)
	}
	return mh, nil
}

// func (r *Registrar) MethodHandler(m string) types.MethodHandler {
// 	return r.methods[m]
// }

func (r *Registrar) RegisterMethodHandler(verb string, mh types.MethodHandler) {
	mh.Method = verb
	if mh.Allocate == nil {
		mh.Allocate = func() any {
			return make(map[string]interface{})
		}
	}
	r.methods[verb] = mh
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
