package http

import (
	"net/http"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
)

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/HTTP>

type empty struct{}

type Verb string

func (v Verb) String() string {
	return string(v) // Convert Verb to string
}

const (
	Get  Verb = "GET"
	Post Verb = "POST"
)

func Init(log logr.Logger, r types.MethodsRegistrar) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())

	// register methods
	r.RegisterMethodHandler(Get.String(), types.MethodHandler{
		Allocate:   func() any { return new(Response) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler(Post.String(), types.MethodHandler{
		Allocate:   func() any { return new(Response) },
		HttpMethod: http.MethodPost,
	})

	// register channel
	r.RegisterDeviceCaller(types.ChannelHttp, types.DeviceCaller(httpChannel.callE))
}
