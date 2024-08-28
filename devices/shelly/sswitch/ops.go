package sswitch

import (
	"devices/shelly"
	"devices/shelly/types"
	"net/http"
)

func init() {
	shelly.RegisterMethodHandler("Switch", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	shelly.RegisterMethodHandler("Switch", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	shelly.RegisterMethodHandler("Switch", "Toggle", types.MethodHandler{
		Allocate: func() any { return new(Toogle) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
