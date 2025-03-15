package mqtt

import (
	"pkg/shelly/types"
	"time"

	"net/http"
	"reflect"

	"github.com/go-logr/logr"
	"golang.org/x/exp/rand"
)

type empty struct{}

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	GetStatus Verb = "MQTT.GetStatus"
	GetConfig Verb = "MQTT.GetConfig"
	SetConfig Verb = "MQTT.SetConfig"
)

var registrar types.MethodsRegistrar

func Init(log logr.Logger, r types.MethodsRegistrar, timeout time.Duration) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	registrar = r
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(SetConfig.String(), types.MethodHandler{
		// params: ConfigResponse
		Allocate:   func() any { return new(ConfigResponse) },
		HttpMethod: http.MethodPost,
	})

	mqttChannel.Init(log, timeout)
	registrar.RegisterDeviceCaller(types.ChannelMqtt, types.DeviceCaller(mqttChannel.CallDevice))
}

func init() {
	rand.Seed(uint64(time.Now().UnixNano()))
}
