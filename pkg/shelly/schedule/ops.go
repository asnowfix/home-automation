package schedule

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler("Schedule", "Create", types.MethodHandler{
		Allocate:   func() any { return new(Job) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Schedule", "Update", types.MethodHandler{
		Allocate:   func() any { return new(JobsRevision) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Schedule", "List", types.MethodHandler{
		Allocate:   func() any { return new(Scheduled) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Schedule", "Delete", types.MethodHandler{
		Allocate:   func() any { return new(JobId) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Schedule", "DeleteAll", types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
}
