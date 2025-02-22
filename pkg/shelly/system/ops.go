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

const (
	SetConfig Verb = "SetConfig"
	GetConfig Verb = "GetConfig"
	GetStatus Verb = "GetStatus"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(SetConfig, types.MethodHandler{
		// InputType:  reflect.TypeOf(Config{}),
		Allocate: func() any { return nil },
	})
	r.RegisterMethodHandler(GetConfig, types.MethodHandler{
		Allocate: func() any { return new(Config) },
	})
	r.RegisterMethodHandler(GetStatus, types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
