package kvs

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
	r.RegisterMethodHandler("KVS", "Set", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("KVS", "Get", types.MethodHandler{
		Allocate:   func() any { return new(Value) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("KVS", "GetMany", types.MethodHandler{
		Allocate:   func() any { return new(KeyValueItems) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("KVS", "List", types.MethodHandler{
		Allocate:   func() any { return new(KeyItems) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("KVS", "Delete", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
