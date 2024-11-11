package sswitch

import (
	"devices/shelly/types"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	// setup logger
	log = l
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())

	// register methods
	r.RegisterMethodHandler("Switch", "GetConfig", types.MethodHandler{
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "Toggle", types.MethodHandler{
		Allocate:   func() any { return new(Request) },
		HttpMethod: http.MethodGet,
	})
}
