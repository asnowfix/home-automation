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

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	GetConfig Verb = "Switch.GetConfig"
	SetConfig Verb = "Switch.SetConfig"
	GetStatus Verb = "Switch.GetStatus"
	Toggle    Verb = "Switch.Toggle"
	Set       Verb = "Switch.Set"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())

	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(SetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Config) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Toggle.String(), types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Set.String(), types.MethodHandler{
		Allocate:   func() any { return new(ToogleSetResponse) },
		HttpMethod: http.MethodGet,
	})
}
