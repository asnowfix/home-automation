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

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

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

	r.RegisterMethodHandler(Set.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(Get.String(), types.MethodHandler{
		Allocate:   func() any { return new(GetResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetMany.String(), types.MethodHandler{
		Allocate:   func() any { return new(GetManyResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(List.String(), types.MethodHandler{
		Allocate:   func() any { return new(ListResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Delete.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodPost,
	})
}
