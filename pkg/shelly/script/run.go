package script

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"time"

	"pkg/shelly/mqtt"

	"github.com/dop251/goja"
	"github.com/go-logr/logr"
)

func Run(ctx context.Context, name string, buf []byte, minify bool) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic(err)
	}
	if len(buf) == 0 {
		buf, err = fs.ReadFile(scripts, name)
		if err != nil {
			log.Error(err, "Unknown script", "name", name)
			return err
		}
	}
	handlers := make([]handler, 0)
	vm, err := createShellyRuntime(ctx, &handlers)
	if err != nil {
		log.Error(err, "Failed to create Shelly runtime", "name", name)
		return err
	}
	out, err := vm.RunScript(name, string(buf))
	if err != nil {
		log.Error(err, "Script evaluation failed", "name", name)
		return err
	}
	log.Info("Script evaluated", "name", name, "out", out)

	// If no handlers, just wait for context cancellation
	if len(handlers) == 0 {
		log.Info("No handlers registered, exiting")
		return nil
	}

	log.Info("Starting event loop", "handlers", len(handlers))

	// Build select cases for all handlers + context done
	cases := make([]reflect.SelectCase, len(handlers)+1)

	// First case: context cancellation
	cases[0] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	}

	// Remaining cases: handler channels
	for i, h := range handlers {
		cases[i+1] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(h.Wait()),
		}
	}

	// Event loop: wait on all channels simultaneously
	for {
		chosen, value, ok := reflect.Select(cases)

		if chosen == 0 {
			// Context cancelled
			log.Info("Context cancelled, exiting event loop")
			return ctx.Err()
		}

		// Message received from a handler
		if ok {
			handlerIdx := chosen - 1
			msg := value.Bytes()
			if err := handlers[handlerIdx].Handle(ctx, vm, msg); err != nil {
				log.Error(err, "Handler failed", "handler", handlerIdx)
			}
		} else {
			// Channel closed, remove it from cases
			log.Info("Handler channel closed", "handler", chosen-1)
			// Remove the closed channel by replacing it with the last one
			cases = append(cases[:chosen], cases[chosen+1:]...)
			handlers = append(handlers[:chosen-1], handlers[chosen:]...)

			// If no handlers left, exit
			if len(handlers) == 0 {
				log.Info("All handlers closed, exiting event loop")
				return nil
			}
		}
	}
}

type handler interface {
	Wait() <-chan []byte
	Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error
}

// createShellyRuntime creates a goja VM with Shelly API placeholders
func createShellyRuntime(ctx context.Context, handlers *[]handler) (*goja.Runtime, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	mqttBroker, err := mqtt.FromContext(ctx)
	if err != nil {
		log.Error(err, "MQTT broker not found")
		return nil, err
	}

	vm := goja.New()

	// Shelly object with all APIs from https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#shelly-apis
	shellyObj := vm.NewObject()

	// Shelly.call(method, params, callback, userdata)
	shellyObj.Set("call", func(call goja.FunctionCall) goja.Value {
		method := strings.ToLower(call.Argument(0).String())
		params := call.Argument(1)
		callback := call.Argument(2)

		log.Info("Shelly.call()", "method", method, "params", params.Export())

		if fn, ok := methods[method]; ok {
			result, err := fn(vm, method, params, callback)
			if err != nil {
				log.Error(err, "Shelly.call() failed", "method", method)
				return vm.ToValue(err)
			}
			return vm.ToValue(result)
		} else {
			log.Error(err, "Shelly.call() unknown method", "method", method)
			// Call the callback with null result if provided
			if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
				if callable, ok := goja.AssertFunction(callback); ok {
					// Call: callback(result, error_code, error_message, userdata)
					callable(goja.Undefined(), goja.Null(), vm.ToValue(0), goja.Null())
				}
			}
			return goja.Undefined()
		}
	})

	// Shelly.addStatusHandler(callback, userdata)
	shellyObj.Set("addStatusHandler", func(call goja.FunctionCall) goja.Value {
		log.V(1).Info("Shelly.addStatusHandler placeholder")
		return goja.Undefined()
	})

	// Shelly.addEventHandler(callback, userdata)
	shellyObj.Set("addEventHandler", func(call goja.FunctionCall) goja.Value {
		log.V(1).Info("Shelly.addEventHandler placeholder")
		return goja.Undefined()
	})

	// Shelly.getComponentStatus(component, id)
	shellyObj.Set("getComponentStatus", func(call goja.FunctionCall) goja.Value {
		component := call.Argument(0).String()
		log.V(1).Info("Shelly.getComponentStatus placeholder", "component", component)
		return vm.NewObject() // Return empty object
	})

	// Shelly.getComponentConfig(component, id)
	// https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#shellygetcomponentconfig
	shellyObj.Set("getComponentConfig", func(call goja.FunctionCall) goja.Value {
		component := call.Argument(0).String()
		id := 0
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			id = int(call.Argument(1).ToInteger())
		}

		log.Info("Shelly.getComponentConfig()", "component", component, "id", id)

		// Return component-specific configuration
		var config map[string]interface{}

		switch component {
		case "switch":
			config = map[string]interface{}{
				"id":                id,
				"name":              nil,
				"in_mode":           "follow",
				"initial_state":     "match_input",
				"auto_on":           false,
				"auto_on_delay":     60.0,
				"auto_off":          false,
				"auto_off_delay":    60.0,
				"power_limit":       4480,
				"voltage_limit":     280,
				"current_limit":     16.0,
				"input_id":          id,
				"temperature_limit": 90,
			}
		case "input":
			config = map[string]interface{}{
				"id":            id,
				"name":          nil,
				"type":          "switch",
				"invert":        false,
				"factory_reset": false,
			}
		case "temperature":
			config = map[string]interface{}{
				"id":           id,
				"name":         nil,
				"report_thr_C": 1.0,
				"offset_C":     0.0,
			}
		case "sys":
			config = map[string]interface{}{
				"device": map[string]interface{}{
					"name":         "My Shelly Device",
					"mac":          "AABBCCDDEEFF",
					"fw_id":        "20231107-164738/v1.14.1-gcb84623",
					"discoverable": true,
					"eco_mode":     false,
				},
				"location": map[string]interface{}{
					"tz":  "Europe/Berlin",
					"lat": 52.5200,
					"lon": 13.4050,
				},
				"debug": map[string]interface{}{
					"mqtt": map[string]interface{}{
						"enable": false,
					},
					"websocket": map[string]interface{}{
						"enable": false,
					},
					"udp": map[string]interface{}{
						"addr": nil,
					},
				},
				"ui_data": map[string]interface{}{},
				"rpc_udp": map[string]interface{}{
					"dst_addr":    nil,
					"listen_port": nil,
				},
				"sntp": map[string]interface{}{
					"server": "time.google.com",
				},
				"cfg_rev": 11,
			}
		case "mqtt":
			config = map[string]interface{}{
				"enable":          true,
				"server":          mqttBroker.GetServer(),
				"client_id":       "shelly1minig3-aabbccddeeff",
				"user":            nil,
				"topic_prefix":    "shelly1minig3-aabbccddeeff",
				"rpc_ntf":         true,
				"status_ntf":      false,
				"use_client_cert": false,
				"enable_rpc":      true,
				"enable_control":  true,
			}
		case "wifi":
			config = map[string]interface{}{
				"ap": map[string]interface{}{
					"ssid":    "ShellyMiniG3-AABBCCDDEEFF",
					"is_open": true,
					"enable":  false,
					"range_extender": map[string]interface{}{
						"enable": false,
					},
				},
				"sta": map[string]interface{}{
					"ssid":       "MyWiFi",
					"is_open":    false,
					"enable":     true,
					"ipv4mode":   "dhcp",
					"ip":         nil,
					"netmask":    nil,
					"gw":         nil,
					"nameserver": nil,
				},
				"sta1": map[string]interface{}{
					"ssid":       nil,
					"is_open":    true,
					"enable":     false,
					"ipv4mode":   "dhcp",
					"ip":         nil,
					"netmask":    nil,
					"gw":         nil,
					"nameserver": nil,
				},
				"roam": map[string]interface{}{
					"rssi_thr": -80,
					"interval": 60,
				},
			}
		default:
			// Return empty object for unknown components
			config = map[string]interface{}{}
		}

		return vm.ToValue(config)
	})

	// Shelly.getCurrentScriptId()
	shellyObj.Set("getCurrentScriptId", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(1)
	})

	// Shelly.emitEvent(name, data)
	shellyObj.Set("emitEvent", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		log.V(1).Info("Shelly.emitEvent placeholder", "name", name)
		return goja.Undefined()
	})

	vm.Set("Shelly", shellyObj)

	// Timer object
	timerObj := vm.NewObject()
	timerObj.Set("set", func(call goja.FunctionCall) goja.Value {
		log.V(1).Info("Timer.set placeholder")
		return vm.ToValue(1) // Return timer handle
	})
	timerObj.Set("clear", func(call goja.FunctionCall) goja.Value {
		log.V(1).Info("Timer.clear placeholder")
		return vm.ToValue(true)
	})
	vm.Set("Timer", timerObj)

	// MQTT object
	mqttObj := vm.NewObject()
	mqttObj.Set("subscribe", func(call goja.FunctionCall) goja.Value {

		topic := call.Argument(0).String()
		callback := call.Argument(1)

		log.Info("MQTT.subscribe()", "topic", topic)

		handler, err := mqttSubscribe(ctx, vm, topic, callback)
		if err != nil {
			log.Error(err, "MQTT.subscribe() failed", "topic", topic)
			return vm.ToValue(err)
		}
		*handlers = append(*handlers, handler)
		return vm.ToValue(true)
	})
	mqttObj.Set("unsubscribe", func(call goja.FunctionCall) goja.Value {
		topic := call.Argument(0).String()
		log.V(1).Info("MQTT.unsubscribe placeholder", "topic", topic)
		return vm.ToValue(true)
	})
	mqttObj.Set("publish", func(call goja.FunctionCall) goja.Value {
		topic := call.Argument(0).String()
		log.V(1).Info("MQTT.publish placeholder", "topic", topic)
		return vm.ToValue(true)
	})
	mqttObj.Set("setStatusHandler", func(call goja.FunctionCall) goja.Value {
		log.V(1).Info("MQTT.setStatusHandler placeholder")
		return goja.Undefined()
	})
	vm.Set("MQTT", mqttObj)

	// Script object
	scriptObj := vm.NewObject()

	// Script.storage object
	storageObj := vm.NewObject()
	storage := make(map[string]interface{}) // In-memory storage for testing
	storageObj.Set("getItem", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		if val, ok := storage[key]; ok {
			return vm.ToValue(val)
		}
		return goja.Null()
	})
	storageObj.Set("setItem", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		value := call.Argument(1).Export()
		storage[key] = value
		log.V(1).Info("Script.storage.setItem", "key", key, "value", value)
		return goja.Undefined()
	})
	scriptObj.Set("storage", storageObj)

	vm.Set("Script", scriptObj)

	// Global print function
	vm.Set("print", func(call goja.FunctionCall) goja.Value {
		args := make([]interface{}, len(call.Arguments))
		for i, arg := range call.Arguments {
			args[i] = arg.Export()
		}
		log.Info("Script print", "args", args)
		return goja.Undefined()
	})

	// Console object with log method
	consoleObj := vm.NewObject()
	consoleObj.Set("log", func(call goja.FunctionCall) goja.Value {
		args := make([]interface{}, len(call.Arguments))
		for i, arg := range call.Arguments {
			args[i] = arg.Export()
		}
		log.Info("Script console.log", "args", args)
		return goja.Undefined()
	})
	vm.Set("console", consoleObj)

	// Global JSON object (usually available, but ensure it's there)
	vm.RunString(`
		if (typeof JSON === 'undefined') {
			var JSON = {
				parse: function(s) { return eval('(' + s + ')'); },
				stringify: function(o) { return String(o); }
			};
		}
	`)

	return vm, nil
}

type methodFunc func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value) (interface{}, error)

var methods = map[string]methodFunc{
	"shelly.detectlocation": func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value) (interface{}, error) {
		// emulate https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly#shellydetectlocation
		if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
			if callable, ok := goja.AssertFunction(callback); ok {
				result := map[string]interface{}{
					"lat": 52.5200,
					"lon": 13.4050,
					"tz":  "Europe/Berlin",
				}
				// Call: callback(result, error_code, error_message)
				ret, err := callable(goja.Undefined(), vm.ToValue(result), vm.ToValue(0), goja.Null())
				if err != nil {
					return nil, err
				}
				return ret.Export(), nil
			}
		}
		return nil, nil
	},
	"kvs.getmany": func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value) (interface{}, error) {
		// emulate https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/KVS#kvsgetmany
		if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
			if callable, ok := goja.AssertFunction(callback); ok {
				result := map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"key":   "item1",
							"etag":  "0DhkTpVgJk9zc2soEXlpoLrw==",
							"value": "value item1",
						},
						map[string]interface{}{
							"key":   "normally-closed",
							"etag":  "0DXyU0CpLjyvZAV8GjRb2VzA==",
							"value": "true",
						},
						map[string]interface{}{
							"key":   "script/heater/set-point",
							"etag":  "0DXyU0CpLjyvZAV8GjRb2VzA==",
							"value": "19.0",
						},
						map[string]interface{}{
							"key":   "script/heater/enable-logging",
							"etag":  "0DXyU0CpLjyvZAV8GjRb2VzA==",
							"value": "true",
						},
						map[string]interface{}{
							"key":   "script/heater/internal-temperature-topic",
							"etag":  "0DXyU0CpLjyvZAV8GjRb2VzA==",
							"value": "shellies/shellyht-208500/sensor/temperature",
						},
						map[string]interface{}{
							"key":   "script/heater/external-temperature-topic",
							"etag":  "0DXyU0CpLjyvZAV8GjRb2VzA==",
							"value": "shellies/shellyht-EE45E9/sensor/temperature",
						},
					},
					"offset": 0,
					"total":  26,
				}
				// Call: callback(result, error_code, error_message)
				ret, err := callable(goja.Undefined(), vm.ToValue(result), vm.ToValue(0), goja.Null())
				if err != nil {
					return nil, err
				}
				return ret.Export(), nil
			}
		}
		return nil, nil
	},
	"http.get": func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value) (interface{}, error) {
		// emulate https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/HTTP#httpget
		// params: { url: string, timeout: number }
		paramsObj := params.ToObject(vm)
		url := paramsObj.Get("url").String()
		timeout := int(paramsObj.Get("timeout").ToInteger())

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
				if callable, ok := goja.AssertFunction(callback); ok {
					callable(goja.Undefined(), goja.Null(), vm.ToValue(-1), vm.ToValue(err.Error()))
				}
			}
			return nil, err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
				if callable, ok := goja.AssertFunction(callback); ok {
					callable(goja.Undefined(), goja.Null(), vm.ToValue(-1), vm.ToValue(err.Error()))
				}
			}
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
				if callable, ok := goja.AssertFunction(callback); ok {
					callable(goja.Undefined(), goja.Null(), vm.ToValue(-1), vm.ToValue(err.Error()))
				}
			}
			return nil, err
		}

		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
			if callable, ok := goja.AssertFunction(callback); ok {
				result := map[string]interface{}{
					"body":    string(body),
					"headers": headers,
					"status":  resp.StatusCode,
				}
				// Call: callback(result, error_code, error_message)
				ret, err := callable(goja.Undefined(), vm.ToValue(result), vm.ToValue(0), goja.Null())
				if err != nil {
					return nil, err
				}
				return ret.Export(), nil
			}
		}
		return nil, nil
	},
}

// Actual implementation for MQTT.subscribe <https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#mqttsubscribe>

func mqttSubscribe(ctx context.Context, vm *goja.Runtime, topic string, callback goja.Value) (handler, error) {
	if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
		if callable, ok := goja.AssertFunction(callback); ok {
			mc, err := mqtt.FromContext(ctx)
			if err != nil {
				return nil, err
			}
			in, err := mc.Subscriber(ctx, topic, 0)
			if err != nil {
				return nil, err
			}
			return &mqttHandler{
				topic:    topic,
				input:    in,
				callable: callable,
			}, nil
		}
	}
	return nil, nil
}

type mqttHandler struct {
	topic    string
	input    <-chan []byte
	callable goja.Callable
}

func (mh *mqttHandler) Wait() <-chan []byte {
	return mh.input
}

func (mh *mqttHandler) Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}
	// Call: callback(result, error_code, error_message)
	ret, err := mh.callable(goja.Undefined(), vm.ToValue(string(msg)), goja.Null(), goja.Null())
	if err != nil {
		log.Error(err, "MQTT callback", "topic", mh.topic, "error", err)
		return err
	}
	log.Info("MQTT callback", "topic", mh.topic, "result", ret)
	return nil
}
