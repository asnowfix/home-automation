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

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	Create    Verb = "Schedule.Create"
	Update    Verb = "Schedule.Update"
	List      Verb = "Schedule.List"
	Delete    Verb = "Schedule.Delete"
	DeleteAll Verb = "Schedule.DeleteAll"
)

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler(Create.String(), types.MethodHandler{
		Allocate:   func() any { return new(Job) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(Update.String(), types.MethodHandler{
		Allocate:   func() any { return new(JobsRevision) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(List.String(), types.MethodHandler{
		Allocate:   func() any { return new(Scheduled) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Delete.String(), types.MethodHandler{
		Allocate:   func() any { return new(JobId) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler(DeleteAll.String(), types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
}
