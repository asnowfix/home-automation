package sswitch

import (
	"devices/shelly/types"
	"net/http"
)

func Init(r types.MethodsRegistrar) {
	r.RegisterMethodHandler("Switch", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Switch", "Toggle", types.MethodHandler{
		Allocate: func() any { return new(Toogle) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
