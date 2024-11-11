package script

import (
	"devices/shelly/types"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler("Script", "GetConfig", types.MethodHandler{
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
