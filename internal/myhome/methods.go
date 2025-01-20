package myhome

import (
	"reflect"
)

type Method struct {
	InType  reflect.Type
	OutType reflect.Type
	ActionE func(in any) (any, error)
}

var Methods map[string]Method = map[string]Method{
	"devices.list": Method{
		InType:  reflect.TypeOf(nil),
		OutType: reflect.TypeOf([]Device{}),
		ActionE: nil,
	},
	"group.list": Method{
		InType:  reflect.TypeOf(nil),
		OutType: reflect.TypeOf([]Group{}),
		ActionE: nil,
	},
	"group.create": Method{
		InType:  reflect.TypeOf(""),
		OutType: reflect.TypeOf(nil),
		ActionE: nil,
	},
	"group.delete": Method{
		InType:  reflect.TypeOf(""),
		OutType: reflect.TypeOf(nil),
		ActionE: nil,
	},
	"group.getdevices": Method{
		InType:  reflect.TypeOf(""),
		OutType: reflect.TypeOf([]Device{}),
		ActionE: nil,
	},
}
