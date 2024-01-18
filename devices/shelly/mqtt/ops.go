package mqtt

import (
	"devices/shelly/types"
)

func Init(cm types.ConfigurationMethod) {
	cm("Mqtt", "GetStatus", types.MethodConfiguration{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
	})
	cm("Mqtt", "GetConfig", types.MethodConfiguration{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
	})
	// cm("Mqtt", "SetConfig", types.MethodConfiguration{
	// 	Allocate: func() any { return new(Configuration) },
	// 	Params: map[string]string{
	// 		"id": "0",
	// 	},
	// })
}
