package system

import (
	"devices/shelly/types"
	"net/http"
)

func Init(r types.MethodsRegistrar) {
	r.RegisterMethodHandler("System", "GetConfig", types.MethodHandler{
		Allocate:  func() any { return new(Configuration) },
		HttpQuery: map[string]string{},
	})
	r.RegisterMethodHandler("System", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpQuery:  map[string]string{},
		HttpMethod: http.MethodGet,
	})
	// System.SetConfig
}
