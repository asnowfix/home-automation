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

const (
	GetStatus Verb = "GetStatus"
	GetConfig Verb = "GetConfig"
	SetConfig Verb = "SetConfig"
)

var registrar types.MethodsRegistrar

func Init(log logr.Logger, r types.MethodsRegistrar, timeout time.Duration) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	registrar = r
	r.RegisterMethodHandler(string(GetStatus), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(GetConfig), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(SetConfig), types.MethodHandler{
		Allocate:   func() any { return new(ConfigResults) },
		HttpMethod: http.MethodPost,
	})

	mqttChannel.Init(log, timeout)
	registrar.RegisterDeviceCaller(types.ChannelMqtt, types.DeviceCaller(mqttChannel.CallDevice))
}

func init() {
	rand.Seed(uint64(time.Now().UnixNano()))
}
