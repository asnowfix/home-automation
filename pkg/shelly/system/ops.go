package system

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys>

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	SetConfig Verb = "Sys.SetConfig"
	GetConfig Verb = "Sys.GetConfig"
	GetStatus Verb = "Sys.GetStatus"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(SetConfig.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(SetConfigRequest{}),
		Allocate: func() any { return new(SetConfigResponse) },
	})
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate: func() any { return new(Config) },
	})
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
