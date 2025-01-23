package myhome

import "fmt"

type MethodHandler func(in any) (any, error)

type MethodSignature struct {
	NewParams func() any
	NewResult func() any
}

type Method struct {
	Signature MethodSignature
	ActionE   MethodHandler
}

func Methods(name string) (*Method, error) {
	m, exists := methods[name]
	if !exists {
		return nil, fmt.Errorf("unknown or unregistered method %s", name)
	}
	return m, nil
}

func RegisterMethodHandler(name string, mh MethodHandler) {
	s, exists := signatures[name]
	if !exists {
		panic(fmt.Errorf("unknown method %s", name))
	}
	methods[name] = &Method{
		Signature: s,
		ActionE:   mh,
	}
}

var methods map[string]*Method = make(map[string]*Method)

var signatures map[string]MethodSignature = map[string]MethodSignature{
	"device.list": {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &Devices{}
		},
	},
	"group.list": {
		NewParams: func() any {
			return nil
		},
		NewResult: func() any {
			return &Groups{}
		},
	},
	"group.create": {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return nil
		},
	},
	"group.delete": {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return nil
		},
	},
	"group.getdevices": {
		NewParams: func() any {
			return ""
		},
		NewResult: func() any {
			return Devices{}
		},
	},
	"group.adddevice": {
		NewParams: func() any {
			return GroupDevice{}
		},
		NewResult: func() any {
			return nil
		},
	},
	"group.removedevice": {
		NewParams: func() any {
			return GroupDevice{}
		},
		NewResult: func() any {
			return nil
		},
	},
}
