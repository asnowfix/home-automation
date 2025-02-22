package kvs

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

type Verb string

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/KVS/>

const (
	Get     Verb = "KVS.Get"
	Set     Verb = "KVS.Set"
	Delete  Verb = "KVS.Delete"
	GetMany Verb = "KVS.GetMany"
	List    Verb = "KVS.List"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(Set, types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(Get, types.MethodHandler{
		Allocate:   func() any { return new(Value) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetMany, types.MethodHandler{
		Allocate:   func() any { return new(KeyValueItems) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(List, types.MethodHandler{
		Allocate:   func() any { return new(KeyItems) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Delete, types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodPost,
	})
}
