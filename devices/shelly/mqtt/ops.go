package mqtt

import (
	"devices/shelly/types"
	"net/http"
)

func Init(cm types.MethodRegistration) {
	cm("Mqtt", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	cm("Mqtt", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	cm("Mqtt", "SetConfig", types.MethodHandler{
		Allocate: func() any { return new(ConfigResults) },
		Params: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodPost,
	})
}
