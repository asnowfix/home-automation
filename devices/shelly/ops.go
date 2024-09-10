package shelly

import (
	"devices/shelly/types"
	"fmt"
	"log"
	"net/http"
	"reflect"
)

var methods = make(map[string]map[string]types.MethodHandler)

var channel Channel = Http

// methods := map[string]map[string]{
// 	"Shelly": {
// 		"ListMethods": types.MethodHandler{
// 			Allocate:   func() any { return new(Methods) },
// 			HttpQuery:  map[string]string{},
// 			HttpMethod: http.MethodGet,
// 		}
// 	}
// }

func init() {
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
	log.Default().Printf("Registered %v methods handlers", len(methods))
}

func Call(device *Device, component string, verb string, params any) any {
	data, err := CallE(device, component, verb, params)
	if err != nil {
		log.Default().Printf("calling device %v: %v", device.Id, err)
		panic(err)
	}
	return data
}

func CallE(device *Device, c string, v string, params any) (any, error) {
	method := fmt.Sprintf("%v.%v", c, v)
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
	log.Default().Printf("calling channel:%s method:%v: parser:%v params:%v", channel, method, reflect.TypeOf(out), params)
	log.Default().Printf("channels:%v", channels)
	return channels[channel](device, verb, method, out, params)
	// switch channel {
	// case Http:
	// return shttp.Call(device, verb, method, out, verb.HttpQuery, params)
	// // case Mqtt:
	// // 	return mqtt.Call(device, fmt.Sprintf("%v.%v", c, v), out, params)
	// default:
	// 	return nil, fmt.Errorf("unsupported channel %v", channel)
	// }
}

var channels = make([]DeviceCaller, 3 /*sizeof(Channel*/)

func RegisterDeviceCaller(ch Channel, dc DeviceCaller) {
	log.Default().Printf("Registering %v for channel %s", dc, ch)

	channels[ch] = dc
}

type DeviceCaller func(device *Device, verb types.MethodHandler, method string, out any, params any) (any, error)

type Channel uint

const (
	Http Channel = iota
	Mqtt
	Udp
)

func (ch Channel) String() string {
	return [...]string{"Http", "Mqtt", "Udp"}[ch]
}
