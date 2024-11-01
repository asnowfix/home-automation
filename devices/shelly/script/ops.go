package script

import (
	"devices/shelly/types"
	"net/http"
)

func Init(r types.MethodsRegistrar) {
	r.RegisterMethodHandler("Script", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Script", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
