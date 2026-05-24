package cloud

import (
	"net/http"
	"reflect"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

type empty struct{}

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Cloud

type Verb string

func (v Verb) String() string { return string(v) }

const (
	GetStatus Verb = "Cloud.GetStatus"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	l.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(GetStatus.String(), types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
}
