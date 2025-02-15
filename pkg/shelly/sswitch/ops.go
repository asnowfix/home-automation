package sswitch

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())

	r.RegisterMethodHandler("Switch", "GetConfig", types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "SetConfig", types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "Toggle", types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "Set", types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
}
