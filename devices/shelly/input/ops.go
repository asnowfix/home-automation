package input

import (
	"devices/shelly/types"
	"net/http"
)

func Init(cm types.MethodRegistration) {
	cm("Input", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	cm("Input", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
