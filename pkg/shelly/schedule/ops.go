package schedule

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

type Verb string

const (
	Create    Verb = "Create"
	Update    Verb = "Update"
	List      Verb = "List"
	Delete    Verb = "Delete"
	DeleteAll Verb = "DeleteAll"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(string(Create), types.MethodHandler{
		Allocate:   func() any { return new(Job) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(Update), types.MethodHandler{
		Allocate:   func() any { return new(JobsRevision) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(List), types.MethodHandler{
		Allocate:   func() any { return new(Scheduled) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(string(Delete), types.MethodHandler{
		Allocate:   func() any { return new(JobId) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(string(DeleteAll), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
}
