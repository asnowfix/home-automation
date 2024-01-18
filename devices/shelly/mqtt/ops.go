package mqtt

import (
	"devices/shelly/types"
)

func Init(cm types.ConfigurationMethod) {
	cm("MQTT", "GetStatus", types.MethodConfiguration{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
	})
	cm("MQTT", "GetConfig", types.MethodConfiguration{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
	})
	// cm("MQTT", "SetConfig", types.MethodConfiguration{
	// 	Allocate: func() any { return new(Configuration) },
	// 	Params: map[string]string{
	// 		"id": "0",
	// 	},
	// })
}
