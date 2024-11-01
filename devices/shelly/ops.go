package shelly

import (
	"devices/shelly/mqtt"
	shttp "devices/shelly/shttp"
	"devices/shelly/types"
	"fmt"
	"log"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
)

func Init(l logr.Logger) {
	registrar.Init()
	shttp.Init(&registrar, l)
	mqtt.Init(&registrar, l)
}

var registrar Registrar

// return singleton registrar
func GetRegistrar() *Registrar {
	return &registrar
}

type Registrar struct {
	methods  map[string]map[string]types.MethodHandler
	channel  types.Channel
	channels []types.DeviceCaller
}

var listMethodsHandler = types.MethodHandler{
	Allocate:   func() any { return new(Methods) },
	HttpQuery:  map[string]string{},
	HttpMethod: http.MethodGet,
}

func (r *Registrar) Init() {
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
	log.Default().Printf("Registering handler for method:%v.%v...", c, v)
	if _, exists := r.methods[c]; !exists {
		r.methods[c] = make(map[string]types.MethodHandler)
		log.Default().Printf("... Added API:%v", c)
	}
	if _, exists := r.methods[c][v]; !exists {
		r.methods[c][v] = m
		log.Default().Printf("... Added verb:%v.%v HTTP(method=%v params=%v)", c, v, m.HttpMethod, m.HttpQuery)
	}
	m.Method = fmt.Sprintf("%s.%s", c, v)
	log.Default().Printf("Registered %v methods handlers", len(r.methods))
}

var ChannelRegistered = errors.Errorf("channel registration")

func (r *Registrar) RegisterDeviceCaller(ch types.Channel, dc types.DeviceCaller) {
	log.Default().Printf("Registering %v for channel %s", dc, ch)

	// err := errors.New(fmt.Sprintf("Registering %v for channel %s", dc, ch))
	err := errors.Wrap(fmt.Errorf("registering %v for channel %s", dc, ch), "foo")
	// err := errors.New(ChannelRegistered)
	log.Default().Print(err)

	r.channels[ch] = dc
}

func (r *Registrar) CallE(d types.Device, ch types.Channel, mh types.MethodHandler, params any) (any, error) {
	out := mh.Allocate()
	log.Default().Printf("calling channel:%s method:%v parser:%v params:%v", ch, mh, reflect.TypeOf(out), params)
	return r.channels[ch](d, mh, out, params)
}
