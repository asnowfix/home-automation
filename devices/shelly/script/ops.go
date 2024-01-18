package script

import (
	"devices/shelly/types"
)

func Init(cm types.ConfigurationMethod) {
	cm("Script", "GetConfig", types.MethodConfiguration{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
	})
	cm("Script", "GetStatus", types.MethodConfiguration{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
	})
}
