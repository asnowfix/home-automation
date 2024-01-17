package system

import "devices/shelly/types"

func Init(cm types.ConfigurationMethod) {
	cm("System", "GetConfig", types.MethodConfiguration{
		Allocate: func() any { return new(Configuration) },
		Params:   map[string]string{},
	})
	cm("System", "GetStatus", types.MethodConfiguration{
		Allocate: func() any { return new(Status) },
		Params:   map[string]string{},
	})
	// System.SetConfig
}
