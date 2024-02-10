package system

import (
	"devices/shelly"
	"devices/shelly/types"
	"net/http"
)

func init() {
	shelly.RegisterMethodHandler("System", "GetConfig", types.MethodHandler{
		Allocate:  func() any { return new(Configuration) },
		HttpQuery: map[string]string{},
	})
	shelly.RegisterMethodHandler("System", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpQuery:  map[string]string{},
		HttpMethod: http.MethodGet,
	})
	// System.SetConfig
}
