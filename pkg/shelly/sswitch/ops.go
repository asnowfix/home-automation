package sswitch

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

type Verb string

const (
	GetConfig Verb = "GetConfig"
	SetConfig Verb = "SetConfig"
	GetStatus Verb = "GetStatus"
	Toggle    Verb = "Toggle"
	Set       Verb = "Set"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())

	r.RegisterMethodHandler(string(GetConfig), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(SetConfig), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(GetStatus), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(Toggle), types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(Set), types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
}
