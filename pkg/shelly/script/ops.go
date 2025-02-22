package script

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Script>

type Verb string

const (
	SetConfig Verb = "Script.SetConfig"
	GetConfig Verb = "Script.GetConfig"
	GetStatus Verb = "Script.GetStatus"
	List      Verb = "Script.List"
	Create    Verb = "Script.Create"
	Start     Verb = "Script.Start"
	Delete    Verb = "Script.Delete"
	Stop      Verb = "Script.Stop"
	PutCode   Verb = "Script.PutCode"
	GetCode   Verb = "Script.GetCode"
	Eval      Verb = "Script.Eval"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(string(SetConfig), types.MethodHandler{
		// InputType:  reflect.TypeOf(ConfigurationRequest{}),
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(GetConfig), types.MethodHandler{
		// InputType:  reflect.TypeOf(Configuration{}),
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(GetStatus), types.MethodHandler{
		//InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(Create), types.MethodHandler{
		// InputType:  reflect.TypeOf(Configuration{}),
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(Delete), types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(PutCode), types.MethodHandler{
		// InputType:  reflect.TypeOf(PutCodeRequest{}),
		Allocate:   func() any { return new(PutCodeResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(GetCode), types.MethodHandler{
		// InputType:  reflect.TypeOf(GetCodeRequest{}),
		Allocate:   func() any { return new(GetCodeResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(Eval), types.MethodHandler{
		// InputType:  reflect.TypeOf(EvalRequest{}),
		Allocate:   func() any { return new(EvalResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(Start), types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(FormerStatus) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(Stop), types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(FormerStatus) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(List), types.MethodHandler{
		// InputType:  nil,
		Allocate:   func() any { return new(ListResponse) },
		HttpMethod: http.MethodGet,
	})
}
