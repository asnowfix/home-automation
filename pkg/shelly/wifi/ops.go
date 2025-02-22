package wifi

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	GetConfig     Verb = "GetConfig"
	SetConfig     Verb = "SetConfig"
	GetStatus     Verb = "GetStatus"
	Scan          Verb = "Scan"          // TODO
	ListAPClients Verb = "ListAPClients" //TODO
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate: func() any { return new(Config) },
	})
	r.RegisterMethodHandler(SetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
