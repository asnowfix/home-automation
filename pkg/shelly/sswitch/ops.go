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

	r.RegisterMethodHandler(GetConfig, types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(SetConfig, types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetStatus, types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Toggle, types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Set, types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
}
