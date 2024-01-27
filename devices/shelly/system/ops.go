package system

import (
	"devices/shelly/types"
	"net/http"
)

func Init(cm types.MethodRegistration) {
	cm("System", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		Params:   map[string]string{},
	})
	cm("System", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		Params:     map[string]string{},
		HttpMethod: http.MethodGet,
	})
	// System.SetConfig
}
