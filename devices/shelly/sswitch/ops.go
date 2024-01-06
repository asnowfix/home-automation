package sswitch

import "devices/shelly/types"

func Init(cm types.ConfigurationMethod) {
	cm("Switch.GetConfig", types.MethodConfiguration{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
	})
	cm("Switch.GetStatus", types.MethodConfiguration{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
	})
	cm("Switch.Toogle", types.MethodConfiguration{
		Allocate: func() any { return new(Toogle) },
		Params: map[string]string{
			"id": "0",
		},
	})
}
