package myhome

import "fmt"

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
	DeviceList: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &Devices{}
		},
	},
	DeviceLookup: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return &DeviceSummary{}
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
	GroupList: {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &Groups{}
		},
	},
	GroupCreate: {
		NewParams: func() any {
			return &GroupInfo{}
		},
		NewResult: func() any {
			return nil
		},
	},
	GroupDelete: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return nil
		},
	},
	GroupShow: {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return &Group{}
		},
	},
	GroupAddDevice: {
		NewParams: func() any {
			return &GroupDevice{}
		},
		NewResult: func() any {
			return nil
		},
	},
	GroupRemoveDevice: {
		NewParams: func() any {
			return &GroupDevice{}
		},
		NewResult: func() any {
			return nil
		},
	},
}
