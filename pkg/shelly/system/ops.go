package system

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler("Sys", "SetConfig", types.MethodHandler{
		// InputType:  reflect.TypeOf(Config{}),
		Allocate: func() any { return nil },
	})
	r.RegisterMethodHandler("Sys", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Config) },
	})
	r.RegisterMethodHandler("Sys", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
