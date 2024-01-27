package sswitch

import (
	"devices/shelly/types"
	"net/http"
)

func Init(cm types.MethodRegistration) {
	cm("Switch", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	cm("Switch", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	cm("Switch", "Toogle", types.MethodHandler{
		Allocate: func() any { return new(Toogle) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
}
