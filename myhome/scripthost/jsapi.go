package scripthost

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	shellyscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/dop251/goja"
)

// installMyHomeAPI adds the MyHome global to a script VM. This is the only
// API surface daemon scripts have beyond the standard Shelly emulation:
// workflows live in JS, infrastructure stays behind these Go bindings.
//
// Callback convention follows Shelly.call: callback(result, error_code,
// error_message). All bindings are asynchronous (work runs on Go goroutines,
// callbacks are dispatched back onto the VM goroutine).
func (r *runner) installMyHomeAPI(ctx context.Context, vm *goja.Runtime) error {
	obj := vm.NewObject()

	// MyHome.instance() -> daemon instance name
	obj.Set("instance", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(r.svc.instance)
	})

	// MyHome.log(...) -> daemon structured log
	obj.Set("log", func(call goja.FunctionCall) goja.Value {
		args := make([]interface{}, len(call.Arguments))
		for i, a := range call.Arguments {
			args[i] = a.Export()
		}
		r.log.Info(fmt.Sprint(args...))
		return goja.Undefined()
	})

	// MyHome.on(name, fn) -> handle script.invoke calls addressed to this script
	obj.Set("on", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.ToValue("MyHome.on: second argument must be a function"))
		}
		r.invokeHandlers[name] = fn
		r.log.Info("Registered invoke handler", "name", name)
		return goja.Undefined()
	})

	// MyHome.call(method, params, callback) -> in-process myhome RPC verb
	obj.Set("call", func(call goja.FunctionCall) goja.Value {
		method := call.Argument(0).String()
		raw := exportJSON(call.Argument(1))
		callback := callbackOf(call, 2)

		go func() {
			callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			out, err := myhome.CallLocalE(callCtx, myhome.Verb(method), raw)
			r.deliver(ctx, callback, out, err)
		}()
		return goja.Undefined()
	})

	// MyHome.deviceCall(device, method, params, callback) -> RPC to a Shelly device
	obj.Set("deviceCall", func(call goja.FunctionCall) goja.Value {
		identifier := call.Argument(0).String()
		method := call.Argument(1).String()
		params := call.Argument(2).Export()
		callback := callbackOf(call, 3)

		go func() {
			callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			out, err := r.svc.deviceCall(callCtx, identifier, method, params)
			r.deliver(ctx, callback, out, err)
		}()
		return goja.Undefined()
	})

	// MyHome.uploadScript(device, scriptName, callback) -> upload + start an
	// embedded device script (firmware-grade operation, stays in Go)
	obj.Set("uploadScript", func(call goja.FunctionCall) goja.Value {
		identifier := call.Argument(0).String()
		scriptName := call.Argument(1).String()
		callback := callbackOf(call, 2)

		go func() {
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			err := r.svc.uploadScript(callCtx, identifier, scriptName)
			r.deliver(ctx, callback, map[string]interface{}{"uploaded": err == nil}, err)
		}()
		return goja.Undefined()
	})

	// MyHome.registerVerb(verb, fn) -> JS implementation of an existing RPC
	// verb (opt-in workflow replacement, e.g. heater.getconfig)
	obj.Set("registerVerb", func(call goja.FunctionCall) goja.Value {
		verb := myhome.Verb(call.Argument(0).String())
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.ToValue("MyHome.registerVerb: second argument must be a function"))
		}
		r.verbHandlers[verb] = fn
		if !r.registeredVerbs[verb] {
			if err := r.registerGoVerb(ctx, verb); err != nil {
				panic(vm.ToValue(err.Error()))
			}
			r.registeredVerbs[verb] = true
		}
		r.log.Info("Registered verb handler", "verb", verb)
		return goja.Undefined()
	})

	vm.Set("MyHome", obj)
	return nil
}

// registerGoVerb wires a myhome RPC verb to the script's JS handler. The Go
// handler survives engine restarts: it resolves the current engine and JS
// callable at call time.
func (r *runner) registerGoVerb(ctx context.Context, verb myhome.Verb) (err error) {
	defer func() {
		// RegisterMethodHandler panics on verbs missing from the signatures
		// map; surface that as a script error instead of killing the daemon.
		if p := recover(); p != nil {
			err = fmt.Errorf("MyHome.registerVerb(%s): %v", verb, p)
		}
	}()
	myhome.RegisterMethodHandler(verb, func(callCtx context.Context, in any) (any, error) {
		eng := r.engine()
		if eng == nil {
			return nil, fmt.Errorf("script %s is not running", r.name)
		}
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		var generic any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return nil, err
		}

		callCtx, cancel := context.WithTimeout(callCtx, 30*time.Second)
		defer cancel()

		return r.callJSHandler(callCtx, eng, generic, fmt.Sprintf("verb %s", verb), func() (goja.Callable, bool) {
			cb, ok := r.verbHandlers[verb]
			return cb, ok
		})
	})
	return nil
}

// jsOutcome carries an async handler result back to the Go caller.
type jsOutcome struct {
	out any
	err error
}

// callJSHandler invokes a JS handler as handler(params, respond) on the VM
// goroutine and waits for its result. Two completion styles are supported:
//   - synchronous: the handler returns a value (anything but undefined)
//   - asynchronous: the handler returns undefined and later calls
//     respond(result, error) — typically from MyHome.call/deviceCall
//     callbacks. The Go side blocks until respond() or callCtx timeout.
//
// lookup resolves the JS callable on the VM goroutine (handler maps are only
// touched there).
func (r *runner) callJSHandler(callCtx context.Context, eng *pkgscript.Engine, params any, what string, lookup func() (goja.Callable, bool)) (any, error) {
	resultCh := make(chan jsOutcome, 1)
	settle := func(out any, err error) {
		select {
		case resultCh <- jsOutcome{out: out, err: err}:
		default: // already settled (sync return + late respond): keep first
		}
	}

	err := eng.Dispatch(callCtx, func(vm *goja.Runtime) {
		cb, ok := lookup()
		if !ok {
			settle(nil, fmt.Errorf("script %s has no handler for %s", r.name, what))
			return
		}
		respond := vm.ToValue(func(call goja.FunctionCall) goja.Value {
			errArg := call.Argument(1)
			if !goja.IsUndefined(errArg) && !goja.IsNull(errArg) {
				settle(nil, fmt.Errorf("script %s %s: %s", r.name, what, errArg.String()))
			} else {
				settle(exportValue(call.Argument(0)), nil)
			}
			return goja.Undefined()
		})
		v, err := cb(goja.Undefined(), toJSValue(vm, params), respond)
		if err != nil {
			settle(nil, fmt.Errorf("script %s %s: %w", r.name, what, err))
			return
		}
		if v != nil && !goja.IsUndefined(v) {
			// Synchronous completion (null is a valid result)
			settle(exportValue(v), nil)
		}
		// undefined: handler completes asynchronously via respond()
	})
	if err != nil {
		return nil, err
	}
	select {
	case res := <-resultCh:
		return res.out, res.err
	case <-callCtx.Done():
		return nil, fmt.Errorf("script %s %s: %w (handler returned undefined and never called respond)", r.name, what, callCtx.Err())
	}
}

// exportValue converts a JS value to a generic Go value (nil for null/undefined).
func exportValue(v goja.Value) any {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	return v.Export()
}

func (s *Service) doDeviceCall(ctx context.Context, identifier, method string, params any) (any, error) {
	if s.provider == nil {
		return nil, fmt.Errorf("no device provider on this daemon")
	}
	device, err := s.provider.GetDeviceByAny(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("device %s not found: %w", identifier, err)
	}
	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return nil, fmt.Errorf("shelly device %s: %w", identifier, err)
	}
	return sd.CallE(ctx, types.ChannelDefault, method, params)
}

func (s *Service) doUploadScript(ctx context.Context, identifier, scriptName string) error {
	if s.provider == nil {
		return fmt.Errorf("no device provider on this daemon")
	}
	device, err := s.provider.GetDeviceByAny(ctx, identifier)
	if err != nil {
		return fmt.Errorf("device %s not found: %w", identifier, err)
	}
	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return fmt.Errorf("shelly device %s: %w", identifier, err)
	}
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		return fmt.Errorf("embedded script %s: %w", scriptName, err)
	}
	_, err = shellyscript.UploadWithVersion(ctx, s.log, types.ChannelDefault, sd, scriptName, buf, true, false)
	return err
}

// deliver invokes callback(result, error_code, error_message) on the VM
// goroutine. A nil callback means fire-and-forget.
func (r *runner) deliver(ctx context.Context, callback goja.Callable, out any, err error) {
	if callback == nil {
		if err != nil {
			r.log.Error(err, "Async call failed (no callback)")
		}
		return
	}
	eng := r.engine()
	if eng == nil {
		return
	}
	dispatchErr := eng.DispatchAsync(ctx, func(vm *goja.Runtime) {
		var ret goja.Value = goja.Null()
		code := 0
		msg := goja.Value(goja.Null())
		if err != nil {
			code = -1
			msg = vm.ToValue(err.Error())
		} else if out != nil {
			ret = toJSValue(vm, out)
		}
		if _, cbErr := callback(goja.Undefined(), ret, vm.ToValue(code), msg); cbErr != nil {
			r.log.Error(cbErr, "Callback failed")
		}
	})
	if dispatchErr != nil {
		r.log.Error(dispatchErr, "Failed to dispatch callback")
	}
}

// callbackOf extracts an optional callback argument.
func callbackOf(call goja.FunctionCall, idx int) goja.Callable {
	if len(call.Arguments) <= idx {
		return nil
	}
	if fn, ok := goja.AssertFunction(call.Argument(idx)); ok {
		return fn
	}
	return nil
}

// exportJSON converts a JS value into raw JSON (for CallLocalE).
func exportJSON(v goja.Value) json.RawMessage {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	raw, err := json.Marshal(v.Export())
	if err != nil {
		return nil
	}
	return raw
}

// toJSValue converts any Go value to a generic JS value through a JSON
// round-trip (typed structs become plain objects).
func toJSValue(vm *goja.Runtime, v any) goja.Value {
	if v == nil {
		return goja.Null()
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return goja.Undefined()
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return goja.Undefined()
	}
	return vm.ToValue(generic)
}
