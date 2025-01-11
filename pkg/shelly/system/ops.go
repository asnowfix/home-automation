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
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler("System", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
	})
	r.RegisterMethodHandler("System", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	// System.SetConfig
}
