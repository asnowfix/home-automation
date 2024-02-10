package script

import (
	"devices/shelly"
	"devices/shelly/types"
	"net/http"
)

func init() {
	shelly.RegisterMethodHandler("Script", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	shelly.RegisterMethodHandler("Script", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
