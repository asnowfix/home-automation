package shelly

import (
	shttp "devices/shelly/http"
	"devices/shelly/types"
	"fmt"
	"log"
	"net/http"
	"reflect"
)

var methods = make(map[string]map[string]types.MethodHandler)

// methods := map[string]map[string]{
// 	"Shelly": {
// 		"ListMethods": types.MethodHandler{
// 			Allocate:   func() any { return new(Methods) },
// 			HttpQuery:  map[string]string{},
// 			HttpMethod: http.MethodGet,
// 		}
// 	}
// }

func Init() {
	RegisterMethodHandler("Shelly", "ListMethods", types.MethodHandler{
		Allocate:   func() any { return new(Methods) },
		HttpQuery:  map[string]string{},
		HttpMethod: http.MethodGet,
	})
	// Shelly.PutTLSClientKey
	// Shelly.PutTLSClientCert
	// Shelly.PutUserCA
	// Shelly.SetAuth
	// Shelly.Update
	// Shelly.CheckForUpdate
	// Shelly.DetectLocation
	// Shelly.ListTimezones
	// Shelly.GetComponents
	// Shelly.GetStatus
	// Shelly.FactoryReset
	// Shelly.ResetWiFiConfig
	// Shelly.GetConfig
	RegisterMethodHandler("Shelly", "GetDeviceInfo", types.MethodHandler{
		Allocate: func() any { return new(DeviceInfo) },
		HttpQuery: map[string]string{
			"ident": "true",
		},
		HttpMethod: http.MethodGet,
	})
	RegisterMethodHandler("Shelly", "Reboot", types.MethodHandler{
		Allocate:   func() any { return new(string) },
		HttpQuery:  map[string]string{},
		HttpMethod: http.MethodGet,
	})

	log.Default().Printf("Registered %v APIs", len(methods))
}

func RegisterMethodHandler(c string, v string, m types.MethodHandler) {
	log.Default().Printf("Registering handler for method:%v.%v...", c, v)
	if _, exists := methods[c]; !exists {
		methods[c] = make(map[string]types.MethodHandler)
		log.Default().Printf("... Added API:%v", c)
	}
	if _, exists := methods[c][v]; !exists {
		methods[c][v] = m
		log.Default().Printf("... Added verb:%v.%v HTTP(method=%v params=%v)", c, v, m.HttpMethod, m.HttpQuery)
	}
}

func Call(device *Device, component string, verb string, body any) any {
	data, err := CallE(device, component, verb, body)
	if err != nil {
		log.Default().Print(err)
		panic(err)
	}
	return data
}

func CallE(device *Device, c string, v string, body any) (any, error) {
	log.Default().Printf("calling %v.%v body=%v", c, v, body)

	var verb types.MethodHandler

	if c == "Shelly" && v == "ListMethods" {
		// FIXME: Dirty shortcut (every call pays the price of the test)
		verb = methods[c][v]
	} else {
		found := false
		if comp, exists := device.Components[c]; exists {
			if verb, exists = comp[v]; exists {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("did not find any configuration for method: %v.%v", c, v)
		}
	}

	out := verb.Allocate()
	log.Default().Printf("found configuration for method: %v.%v: parser:%v params:%v", c, v, reflect.TypeOf(out), body)

	return shttp.Call(device.Ipv4, verb.HttpMethod, fmt.Sprintf("%v.%v", c, v), out, verb.HttpQuery, body)
}
