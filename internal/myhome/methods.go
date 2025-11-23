package myhome

import (
	"fmt"
)

type MethodHandler func(in any) (any, error)

type MethodSignature struct {
	NewParams func() any
	NewResult func() any
}

type Method struct {
	Name      Verb
	Signature MethodSignature
	ActionE   MethodHandler
}

func Methods(name Verb) (*Method, error) {
	m, exists := methods[name]
	if !exists {
		return nil, fmt.Errorf("unknown or unregistered method %s", name)
	}
	return m, nil
}

func RegisterMethodHandler(name Verb, mh MethodHandler) {
	s, exists := signatures[name]
	if !exists {
		panic(fmt.Errorf("unknown method %s", name))
	}
	methods[name] = &Method{
		Name:      name,
		Signature: s,
		ActionE:   mh,
	}
}

var methods map[Verb]*Method = make(map[Verb]*Method)

var signatures map[Verb]MethodSignature = map[Verb]MethodSignature{
	DevicesMatch: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return &[]DeviceSummary{}
		},
	},
	DeviceLookup: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return &[]DeviceSummary{}
		},
	},
	DeviceShow: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return &Device{}
		},
	},
	DeviceForget: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return nil
		},
	},
	DeviceRefresh: {
		NewParams: func() any {
			return "" // device identifier (id/name/host/etc)
		},
		NewResult: func() any {
			return nil
		},
	},
	DeviceUpdate: {
		NewParams: func() any {
			return &Device{}
		},
		NewResult: func() any {
			return nil
		},
	},
	MqttRepeat: {
		NewParams: func() any {
			return "" // topic string
		},
		NewResult: func() any {
			return nil
		},
	},
	TemperatureGet: {
		NewParams: func() any {
			return &TemperatureGetParams{}
		},
		NewResult: func() any {
			return &TemperatureRoomConfig{}
		},
	},
	TemperatureSet: {
		NewParams: func() any {
			return &TemperatureSetParams{}
		},
		NewResult: func() any {
			return &TemperatureSetResult{}
		},
	},
	TemperatureList: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &TemperatureRoomList{}
		},
	},
	TemperatureDelete: {
		NewParams: func() any {
			return &TemperatureDeleteParams{}
		},
		NewResult: func() any {
			return &TemperatureDeleteResult{}
		},
	},
	TemperatureSetpoint: {
		NewParams: func() any {
			return &TemperatureGetSetpointParams{}
		},
		NewResult: func() any {
			return &TemperatureSetpointResult{}
		},
	},
	OccupancyGetStatus: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &OccupancyStatusResult{}
		},
	},
}
