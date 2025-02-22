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

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

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
	r.RegisterMethodHandler(SetConfig.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(ConfigurationRequest{}),
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(Configuration{}),
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		//InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Create.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(Configuration{}),
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(Delete.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(PutCode.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(PutCodeRequest{}),
		Allocate:   func() any { return new(PutCodeResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(GetCode.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(GetCodeRequest{}),
		Allocate:   func() any { return new(GetCodeResponse) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Eval.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(EvalRequest{}),
		Allocate:   func() any { return new(EvalResponse) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(Start.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(FormerStatus) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(Stop.String(), types.MethodHandler{
		// InputType:  reflect.TypeOf(Id{}),
		Allocate:   func() any { return new(FormerStatus) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(List.String(), types.MethodHandler{
		// InputType:  nil,
		Allocate:   func() any { return new(ListResponse) },
		HttpMethod: http.MethodGet,
	})
}
