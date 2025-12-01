package script

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"pkg/shelly/mqtt"

	"github.com/dop251/goja"
	"github.com/go-logr/logr"
)

func Run(ctx context.Context, name string, buf []byte, minify bool) error {
	emptyState := &DeviceState{
		KVS:     make(map[string]interface{}),
		Storage: make(map[string]interface{}),
	}
	return RunWithDeviceState(ctx, name, buf, minify, emptyState)
}

// RunWithDeviceState runs a script with a provided device state for testing
func RunWithDeviceState(ctx context.Context, name string, buf []byte, minify bool, deviceState *DeviceState) error {
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

	mc, err := mqtt.FromContext(ctx)
	if err != nil {
		log.Error(err, "Failed to get MQTT client", "name", name)
		return err
	}

	vm, err := createShellyRuntime(ctx, mc, &handlers, deviceState)
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

	// Build select cases: context.Done() + all handler channels
	// This function rebuilds the cases array from current handlers
	buildCases := func() []reflect.SelectCase {
		cases := make([]reflect.SelectCase, len(handlers)+1)
		cases[0] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ctx.Done()),
		}
		for i, h := range handlers {
			cases[i+1] = reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(h.Wait()),
			}
		}
		return cases
	}

	cases := buildCases()
	needsRebuild := false

	// Event loop: wait on all channels simultaneously
	for {
		// Rebuild cases if needed (deferred from previous iteration)
		if needsRebuild {
			cases = buildCases()
			needsRebuild = false
		}

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
			handlerCountBefore := len(handlers)
			if err := handlers[handlerIdx].Handle(ctx, vm, msg); err != nil {
				log.Error(err, "Handler failed", "handler", handlerIdx)
			}
			// Check if new handlers were added during Handle()
			if len(handlers) != handlerCountBefore {
				log.Info("Handlers changed, will rebuild cases", "before", handlerCountBefore, "after", len(handlers))
				needsRebuild = true
			}
		} else {
			// Channel closed, remove it from cases
			log.Info("Handler channel closed", "handler", chosen-1)
			// Remove the closed handler
			handlers = append(handlers[:chosen-1], handlers[chosen:]...)

			// If no handlers left, exit
			if len(handlers) == 0 {
				log.Info("All handlers closed, exiting event loop")
				return nil
			}

			// Mark for rebuild at start of next iteration
			needsRebuild = true
		}
	}
}

type handler interface {
	Wait() <-chan []byte
	Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error
}

// createShellyRuntime creates a goja VM with Shelly API placeholders
func createShellyRuntime(ctx context.Context, mc mqtt.Client, handlers *[]handler, deviceState *DeviceState) (*goja.Runtime, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Generate unique device identifier (hostname-program-pid)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	programName := os.Args[0]
	if i := strings.LastIndex(programName, string(os.PathSeparator)); i != -1 {
		programName = programName[i+1:]
	}
	deviceId := fmt.Sprintf("%s-%s-%d", programName, hostname, os.Getpid())

	// Track MQTT subscriptions by topic for unsubscribe
	mqttSubscriptions := make(map[string]int)

	// Track timers by handle
	timers := make(map[int]*timerHandler)
	nextTimerHandle := 1

	vm := goja.New()

	// Shelly event handler system
	eh := NewEventsHandler(ctx, vm)

	// Define methods map with access to deviceState
	methods := createMethodsMap(deviceState)

	// Shelly object with all APIs from https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#shelly-apis
	shellyObj := vm.NewObject()

	// Shelly.call(method, params, callback, userdata)
	shellyObj.Set("call", func(call goja.FunctionCall) goja.Value {
		method := strings.ToLower(call.Argument(0).String())
		params := call.Argument(1)
		callback := call.Argument(2)
		userdata := goja.Undefined()
		if len(call.Arguments) > 3 {
			userdata = call.Argument(3)
		}

		log.Info("Shelly.call()", "method", method, "params", params.Export())

		if fn, ok := methods[method]; ok {
			result, err := fn(vm, method, params, callback, userdata)
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
					callable(goja.Undefined(), goja.Null(), vm.ToValue(0), goja.Null(), userdata)
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

	// [Shelly.addEventHandler(callback, userdata)](https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#shellyaddeventhandler-and-shellyaddstatushandler)
	shellyObj.Set("addEventHandler", func(call goja.FunctionCall) goja.Value {
		callback := call.Argument(0)
		userdata := goja.Undefined()
		if len(call.Arguments) > 1 {
			userdata = call.Argument(1)
		}
		log.Info("Shelly.addEventHandler", "userdata", userdata.Export())

		if goja.IsUndefined(callback) || goja.IsNull(callback) {
			log.Error(nil, "Shelly.addEventHandler called without callback")
			return goja.Undefined()
		}

		if callable, ok := goja.AssertFunction(callback); ok {
			eh.AddHandler(callable, userdata)
			log.V(1).Info("Shelly.addEventHandler registered", "handlers", len(eh.handlers))
		} else {
			log.Error(nil, "Shelly.addEventHandler callback is not a function")
		}
		return goja.Undefined()
	})

	// [Shelly.emitEvent(name, data)](https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#shellyemitevent)
	shellyObj.Set("emitEvent", func(call goja.FunctionCall) goja.Value {
		// event received Shell.addEventHandler() after Shelly.emitEvent() looks like:
		// {
		//   "component":"script:2",
		//   "name":"script",
		//   "id":2,
		//   "now":1763316548.15808582305,
		//   "info":{
		//     "component":"script:2",
		//     "id":2,
		//     "event":"user.check_control_loop",
		//     "data":{"reason":"forecast_ready"},
		//     "ts":1763316548.15999984741
		//   }
		// }
		var event struct {
			Component string  `json:"component"`
			Name      string  `json:"name"`
			Id        int     `json:"id"`
			Now       float64 `json:"now"`
			Info      struct {
				Component string    `json:"component"`
				Id        int       `json:"id"`
				Event     string    `json:"event"`
				Data      any       `json:"data,omitempty"`
				Timestamp time.Time `json:"timestamp"`
			} `json:"info"`
		}
		event.Component = "script:1"
		event.Name = "script"
		event.Id = eh.NextId()
		event.Now = float64(time.Now().UnixNano()) / 1e6
		event.Info.Component = event.Component
		event.Info.Id = event.Id
		event.Info.Event = call.Argument(0).String()
		event.Info.Data = call.Argument(1).Export()
		event.Info.Timestamp = time.Unix(0, int64(event.Now*1e6))

		log.V(1).Info("Shelly.emitEvent", "event", event)

		// Send event to channel (non-blocking)
		select {
		case eh.Broadcaster() <- vm.ToValue(event):
		default:
			log.Error(nil, "Event channel full, dropping event", "event", event)
		}

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
					"name":         deviceId,
					"mac":          "AABBCCDDEEFF",
					"fw_id":        "1.0.0-test",
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
				"server":          mc.GetServer(),
				"client_id":       deviceId,
				"user":            nil,
				"topic_prefix":    deviceId,
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

	// Shelly.getDeviceInfo()
	// <https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#shellygetdeviceinfo>
	// {
	// "name": "radiateur-bureau",
	// "id": "shellyplus1-b8d61a85a970",
	// "mac": "B8D61A85A970",
	// "slot": 0,
	// "model": "SNSW-001X16EU",
	// "gen": 2,
	// "fw_id": "20250924-062720/1.7.1-gd336f31",
	// "ver": "1.7.1",
	// "app": "Plus1",
	// "auth_en": false,
	// "auth_domain": null
	// }
	shellyObj.Set("getDeviceInfo", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(map[string]interface{}{
			"name":        "radiateur-bureau",
			"id":          "shellyplus1-b8d61a85a970",
			"mac":         "B8D61A85A970",
			"slot":        0,
			"model":       "SNSW-001X16EU",
			"gen":         2,
			"fw_id":       "20250924-062720/1.7.1-gd336f31",
			"ver":         "1.7.1",
			"app":         "Plus1",
			"auth_en":     false,
			"auth_domain": nil,
		})
	})

	vm.Set("Shelly", shellyObj)

	// Timer object
	timerObj := vm.NewObject()
	timerObj.Set("set", func(call goja.FunctionCall) goja.Value {
		// Timer.set(period, repeat, callback[, userdata]) -> timer_handle
		if len(call.Arguments) < 3 {
			log.Error(nil, "Timer.set requires at least 3 arguments")
			panic("Timer.set requires at least 3 arguments")
		}

		period := int64(call.Argument(0).ToInteger())
		repeat := call.Argument(1).ToBoolean()
		callback := call.Argument(2)
		var userdata goja.Value
		if len(call.Arguments) > 3 {
			userdata = call.Argument(3)
		} else {
			userdata = goja.Undefined()
		}

		if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
			if callable, ok := goja.AssertFunction(callback); ok {
				handle := nextTimerHandle
				nextTimerHandle++

				timer := &timerHandler{
					handle:    handle,
					period:    time.Duration(period) * time.Millisecond,
					repeat:    repeat,
					callable:  callable,
					userdata:  userdata,
					vm:        vm,
					startTime: time.Now(),
				}

				timers[handle] = timer
				*handlers = append(*handlers, timer)

				log.Info("Timer.set()", "handle", handle, "period", period, "repeat", repeat)
				return vm.ToValue(handle)
			}
		}

		log.Error(nil, "Timer.set callback is not a function")
		panic("Timer.set callback is not a function")
	})
	timerObj.Set("clear", func(call goja.FunctionCall) goja.Value {
		// Timer.clear(timer_handle) -> boolean or undefined
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}

		handle := int(call.Argument(0).ToInteger())
		log.Info("Timer.clear()", "handle", handle)

		if timer, ok := timers[handle]; ok {
			timer.Stop()
			delete(timers, handle)
			return vm.ToValue(true)
		}

		return vm.ToValue(false)
	})
	timerObj.Set("getInfo", func(call goja.FunctionCall) goja.Value {
		// Timer.getInfo(timer_handle) -> object or undefined
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}

		handle := int(call.Argument(0).ToInteger())

		if timer, ok := timers[handle]; ok {
			info := vm.NewObject()
			if timer.repeat {
				info.Set("interval", timer.period.Milliseconds())
			} else {
				info.Set("interval", 0)
			}
			// Calculate next invocation time in milliseconds uptime
			uptime := time.Since(timer.startTime).Milliseconds()
			next := timer.nextFire.Sub(timer.startTime).Milliseconds()
			info.Set("next", next)
			log.V(1).Info("Timer.getInfo()", "handle", handle, "interval", timer.period.Milliseconds(), "next", next, "uptime", uptime)
			return info
		}

		return goja.Undefined()
	})
	vm.Set("Timer", timerObj)

	// MQTT object
	mqttObj := vm.NewObject()
	// MQTT.subscribe(topic, callback) - 2 parameters per Shelly API docs
	// https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#mqttsubscribe
	mqttObj.Set("subscribe", func(call goja.FunctionCall) goja.Value {

		topic := call.Argument(0).String()
		callback := call.Argument(1)

		log.Info("MQTT.subscribe()", "topic", topic)

		handler, err := mqttSubscribe(ctx, mc, vm, topic, callback)
		if err != nil {
			log.Error(err, "MQTT.subscribe() failed", "topic", topic)
			return vm.ToValue(err)
		}
		// Track the handler index by topic
		handlerIdx := len(*handlers)
		*handlers = append(*handlers, handler)
		mqttSubscriptions[topic] = handlerIdx
		return vm.ToValue(true)
	})
	mqttObj.Set("unsubscribe", func(call goja.FunctionCall) goja.Value {
		topic := call.Argument(0).String()
		log.Info("MQTT.unsubscribe()", "topic", topic)

		// Find the handler for this topic
		if handlerIdx, ok := mqttSubscriptions[topic]; ok {
			if handlerIdx < len(*handlers) {
				if mh, ok := (*handlers)[handlerIdx].(*mqttHandler); ok {
					mh.Close()
					delete(mqttSubscriptions, topic)
					log.Info("Unsubscribed from topic", "topic", topic)
					return vm.ToValue(true)
				}
			}
		}

		log.V(1).Info("Topic not found in subscriptions", "topic", topic)
		return vm.ToValue(false)
	})
	mqttObj.Set("publish", func(call goja.FunctionCall) goja.Value {
		topic := call.Argument(0).String()
		message := call.Argument(1).String()

		log.Info("MQTT.publish()", "topic", topic, "message", message)

		err := mc.Publish(ctx, topic, []byte(message), mqtt.AtLeastOnce, false /*retain*/, "shelly/script/run")
		if err != nil {
			log.Error(err, "MQTT.publish() failed", "topic", topic)
			return vm.ToValue(false)
		}
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
	storageObj.Set("getItem", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		log.V(1).Info("Script.storage.getItem", "key", key)
		storage := deviceState.GetStorage()
		if val, ok := storage[key]; ok {
			// If the stored value is nil, treat it as missing/NULL and return null
			// without changing the underlying storage. This preserves "cooling-rate": null
			// and similar entries in device.json instead of turning them into "<nil>".
			if val == nil {
				log.V(1).Info("Script.storage.getItem", "key", key, "value", "null (stored nil)")
				return goja.Null()
			}
			// Storage only supports string values, but be defensive in case
			// older state files contain non-string, non-nil types.
			strVal, ok := val.(string)
			if !ok {
				strVal = fmt.Sprint(val)
				storage[key] = strVal
			}
			log.V(1).Info("Script.storage.getItem", "key", key, "value", strVal)
			return vm.ToValue(strVal)
		}
		// Missing key: return null, do not create it (matches Web Storage API semantics).
		log.V(1).Info("Script.storage.getItem", "key", key, "value", "null (missing)")
		return goja.Null()
	})
	storageObj.Set("setItem", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		// Script.storage follows the Web Storage API semantics and supports
		// only string values. Coerce the value to string using JS semantics.
		valueStr := call.Argument(1).String()
		storage := deviceState.GetStorage()
		storage[key] = valueStr
		// Keep length property roughly in sync with the underlying map.
		storageObj.Set("length", len(storage))
		log.Info("Script.storage.setItem", "key", key, "value", valueStr)
		log.V(1).Info("Script.storage.setItem", "storage", storage)
		return goja.Undefined()
	})
	// Initialize length property to reflect existing storage contents.
	storage := deviceState.GetStorage()
	storageObj.Set("length", len(storage))
	// Provide key(index) to enumerate stored keys, similar to the Web Storage API.
	storageObj.Set("key", func(call goja.FunctionCall) goja.Value {
		idx := int(call.Argument(0).ToInteger())
		if idx < 0 {
			return goja.Null()
		}
		storage := deviceState.GetStorage()
		i := 0
		for k := range storage {
			if i == idx {
				return vm.ToValue(k)
			}
			i++
		}
		return goja.Null()
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

	// Add device's events handler to process emitted events
	*handlers = append(*handlers, eh)

	return vm, nil
}

type methodFunc func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value, userdata goja.Value) (interface{}, error)

func createMethodsMap(deviceState *DeviceState) map[string]methodFunc {
	return map[string]methodFunc{
		"shelly.detectlocation": func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value, userdata goja.Value) (interface{}, error) {
			// emulate https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly#shellydetectlocation
			if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
				if callable, ok := goja.AssertFunction(callback); ok {
					result := map[string]interface{}{
						"lat": 52.5200,
						"lon": 13.4050,
						"tz":  "Europe/Berlin",
					}
					// Call: callback(result, error_code, error_message)
					ret, err := callable(goja.Undefined(), vm.ToValue(result), vm.ToValue(0), goja.Null(), userdata)
					if err != nil {
						return nil, err
					}
					return ret.Export(), nil
				}
			}
			return nil, nil
		},
		"kvs.get": func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value, userdata goja.Value) (interface{}, error) {
			// emulate https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/KVS#kvsget
			paramsObj := params.ToObject(vm)
			key := paramsObj.Get("key").String()

			if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
				if callable, ok := goja.AssertFunction(callback); ok {
					kvs := deviceState.GetKVS()
					if val, ok := kvs[key]; ok {
						result := map[string]interface{}{
							"key":   key,
							"etag":  "0DhkTpVgJk9zc2soEXlpoLrw==",
							"value": val,
						}
						// Call: callback(result, error_code, error_message)
						ret, err := callable(goja.Undefined(), vm.ToValue(result), vm.ToValue(0), goja.Null(), userdata)
						if err != nil {
							return nil, err
						}
						return ret.Export(), nil
					} else {
						// Key not found - add it with null value
						kvs[key] = nil
						// Call callback with error code -114 (key not found)
						callable(goja.Undefined(), goja.Null(), vm.ToValue(-114), vm.ToValue("Key not found"), userdata)
						return nil, nil
					}
				}
			}
			return nil, nil
		},
		"kvs.getmany": func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value, userdata goja.Value) (interface{}, error) {
			// emulate https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/KVS#kvsgetmany
			if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
				if callable, ok := goja.AssertFunction(callback); ok {
					kvs := deviceState.GetKVS()
					items := make([]interface{}, 0, len(kvs))
					for key, value := range kvs {
						items = append(items, map[string]interface{}{
							"key":   key,
							"etag":  "0DhkTpVgJk9zc2soEXlpoLrw==",
							"value": value,
						})
					}

					result := map[string]interface{}{
						"items":  items,
						"offset": 0,
						"total":  len(items),
					}
					// Call: callback(result, error_code, error_message)
					ret, err := callable(goja.Undefined(), vm.ToValue(result), vm.ToValue(0), goja.Null(), userdata)
					if err != nil {
						return nil, err
					}
					return ret.Export(), nil
				}
			}
			return nil, nil
		},
		"http.get": func(vm *goja.Runtime, method string, params goja.Value, callback goja.Value, userdata goja.Value) (interface{}, error) {
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
						callable(goja.Undefined(), goja.Null(), vm.ToValue(-1), vm.ToValue(err.Error()), userdata)
					}
				}
				return nil, err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
					if callable, ok := goja.AssertFunction(callback); ok {
						callable(goja.Undefined(), goja.Null(), vm.ToValue(-1), vm.ToValue(err.Error()), userdata)
					}
				}
				return nil, err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
					if callable, ok := goja.AssertFunction(callback); ok {
						callable(goja.Undefined(), goja.Null(), vm.ToValue(-1), vm.ToValue(err.Error()), userdata)
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
					ret, err := callable(goja.Undefined(), vm.ToValue(result), vm.ToValue(0), goja.Null(), userdata)
					if err != nil {
						return nil, err
					}
					return ret.Export(), nil
				}
			}
			return nil, nil
		},
	}
}

// Actual implementation for MQTT.subscribe <https://shelly-api-docs.shelly.cloud/gen2/Scripts/ShellyScriptLanguageFeatures#mqttsubscribe>
// MQTT.subscribe(topic, callback) - callback receives (topic, message)

func mqttSubscribe(ctx context.Context, mc mqtt.Client, vm *goja.Runtime, topic string, callback goja.Value) (handler, error) {
	if !goja.IsUndefined(callback) && !goja.IsNull(callback) {
		if callable, ok := goja.AssertFunction(callback); ok {
			in, err := mc.Subscribe(ctx, topic, 8, "shelly/script")
			if err != nil {
				return nil, err
			}
			return &mqttHandler{
				topic:    topic,
				input:    in,
				callable: callable,
				closed:   make(chan struct{}),
			}, nil
		}
	}
	return nil, nil
}

type mqttHandler struct {
	topic    string
	input    <-chan []byte
	callable goja.Callable
	closed   chan struct{}
}

func (mh *mqttHandler) Wait() <-chan []byte {
	// Return the closed channel if handler is closed, otherwise return input
	select {
	case <-mh.closed:
		// Return a closed channel
		ch := make(chan []byte)
		close(ch)
		return ch
	default:
		return mh.input
	}
}

func (mh *mqttHandler) Close() {
	select {
	case <-mh.closed:
		// Already closed
		return
	default:
		close(mh.closed)
	}
}

func (mh *mqttHandler) Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}
	// Call: callback(topic, message) - 2 parameters per Shelly API docs
	log.Info("MQTT callback", "topic", mh.topic, "msg", string(msg))
	_, err = mh.callable(goja.Undefined(), vm.ToValue(mh.topic), vm.ToValue(string(msg)))
	if err != nil {
		log.Error(err, "MQTT callback", "topic", mh.topic, "error", err)
		return err
	}
	return nil
}

// Timer handler implementation
type timerHandler struct {
	handle    int
	period    time.Duration
	repeat    bool
	callable  goja.Callable
	userdata  goja.Value
	vm        *goja.Runtime
	ticker    *time.Ticker
	timer     *time.Timer
	startTime time.Time
	nextFire  time.Time
	stopped   bool
	ch        chan []byte // cached channel, created once in Wait()
}

func (th *timerHandler) Wait() <-chan []byte {
	// Return cached channel if already started
	if th.ch != nil {
		return th.ch
	}

	th.ch = make(chan []byte)

	if th.repeat {
		// Periodic timer
		th.ticker = time.NewTicker(th.period)
		th.nextFire = time.Now().Add(th.period)
		go func() {
			for range th.ticker.C {
				if th.stopped {
					break
				}
				th.nextFire = time.Now().Add(th.period)
				th.ch <- []byte{} // Signal to fire callback
			}
			close(th.ch)
		}()
	} else {
		// One-shot timer
		// Treat 0ms as 1ms to ensure event loop is ready
		period := th.period
		if period == 0 {
			period = 1 * time.Millisecond
		}
		th.timer = time.NewTimer(period)
		th.nextFire = time.Now().Add(period)
		go func() {
			<-th.timer.C
			if !th.stopped {
				th.ch <- []byte{} // Signal to fire callback
			}
			close(th.ch)
		}()
	}

	return th.ch
}

func (th *timerHandler) Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	log.V(2).Info("Timer callback", "handle", th.handle, "repeat", th.repeat)

	// Call the callback with userdata
	_, err = th.callable(goja.Undefined(), th.userdata)
	if err != nil {
		log.Error(err, "Timer callback failed", "handle", th.handle)
		return err
	}

	return nil
}

func (th *timerHandler) Stop() {
	th.stopped = true
	if th.ticker != nil {
		th.ticker.Stop()
	}
	if th.timer != nil {
		th.timer.Stop()
	}
}

type shellyEventHandler struct {
	callback goja.Callable
	userdata goja.Value
}

func (seh *shellyEventHandler) Wait() <-chan []byte {
	ch := make(chan []byte)
	return ch
}

func (seh *shellyEventHandler) Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}
	// Call: callback(result, error_code, error_message)
	log.Info("Event callback", "msg", string(msg))
	_, err = seh.callback(goja.Undefined(), vm.ToValue(string(msg)), vm.ToValue(0), goja.Null(), seh.userdata)
	if err != nil {
		log.Error(err, "Event callback", "error", err)
		return err
	}
	return nil
}
