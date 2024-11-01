package input

import (
	"devices/shelly/types"
	"net/http"
)

func Init(r types.MethodsRegistrar) {
	r.RegisterMethodHandler("Input", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Input", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
