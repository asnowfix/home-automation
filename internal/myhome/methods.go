package myhome

import (
	"myhome/devices"
	"reflect"
)

type MethodSignature struct {
	InType  reflect.Type
	OutType reflect.Type
}

type MethodHandler struct {
	MethodSignature
	Method func(in any) (any, error)
}

var Methods map[string]MethodSignature = map[string]MethodSignature{
	"devices.list": MethodSignature{
		InType:  reflect.TypeOf(nil),
		OutType: reflect.TypeOf([]devices.Device{}),
	},
	"group.list": MethodSignature{
		InType:  reflect.TypeOf(nil),
		OutType: reflect.TypeOf([]devices.Group{}),
	},
	"group.create": MethodSignature{
		InType:  reflect.TypeOf(&devices.Group{}),
		OutType: reflect.TypeOf(nil),
	},
	"group.delete": MethodSignature{
		InType:  reflect.TypeOf(""),
		OutType: reflect.TypeOf(nil),
	},
	"group.getdevices": MethodSignature{
		InType:  reflect.TypeOf(""),
		OutType: reflect.TypeOf([]devices.Device{}),
	},
}
