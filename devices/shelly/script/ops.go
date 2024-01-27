package script

import (
	"devices/shelly/types"
	"net/http"
)

func Init(cm types.MethodRegistration) {
	cm("Script", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	cm("Script", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
