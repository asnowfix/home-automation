package input

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

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Input>

const (
	SetConfig       Verb = "InputSetConfig" // TODO
	GetConfig       Verb = "Input.GetConfig"
	GetStatus       Verb = "Input.GetStatus"
	CheckExpression Verb = "Input.CheckExpression" // TODO
	ResetCounters   Verb = "Input.ResetCounters"   // TODO
	Trigger         Verb = "Input.Trigger"         // TODO
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(GetConfig.String(), types.MethodHandler{
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
