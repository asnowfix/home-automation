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
	r.RegisterMethodHandler("Script", "SetConfig", types.MethodHandler{
		// InputType:  reflect.TypeOf(ConfigurationRequest{}),
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "GetConfig", types.MethodHandler{
		// InputType:  reflect.TypeOf(Configuration{}),
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "GetStatus", types.MethodHandler{
		//InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "Create", types.MethodHandler{
		// InputType:  reflect.TypeOf(Configuration{}),
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "PutCode", types.MethodHandler{
		// InputType:  reflect.TypeOf(PutCodeRequest{}),
		Allocate:   func() any { return new(PutCodeResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "GetCode", types.MethodHandler{
		// InputType:  reflect.TypeOf(GetCodeRequest{}),
		Allocate:   func() any { return new(GetCodeResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "Eval", types.MethodHandler{
		// InputType:  reflect.TypeOf(EvalRequest{}),
		Allocate:   func() any { return new(EvalResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Script", "Start", types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(FormerStatus) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Script", "Stop", types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(FormerStatus) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Script", "List", types.MethodHandler{
		// InputType:  nil,
		Allocate:   func() any { return new(ListResponse) },
		HttpMethod: http.MethodGet,
	})
}
