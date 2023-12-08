package sswitch

import (
	"devices/shelly"
)

func init() {
	Initialize()
}

func Initialize() {
	shelly.ConfigureMethod("Switch.GetConfig", shelly.MethodConfiguration{
		Allocate: func() any { return new(Configuration) },
		Params: map[string]string{
			"id": "0",
		},
	})
	shelly.ConfigureMethod("Switch.GetStatus", shelly.MethodConfiguration{
		Allocate: func() any { return new(Status) },
		Params: map[string]string{
			"id": "0",
		},
	})
	shelly.ConfigureMethod("Switch.Toogle", shelly.MethodConfiguration{
		Allocate: func() any { return new(Toogle) },
		Params: map[string]string{
			"id": "0",
		},
	})
}
