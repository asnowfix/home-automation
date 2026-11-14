package script

import (
	"errors"
	"sync/atomic"

	"github.com/dop251/goja"
)

// First knob of the "device-test mode" proposed in #250 and #496: a goja
// call-stack limit, off by default (DeviceState.MaxCallStackSize == 0, i.e.
// unlimited, the historical emulator behaviour).
//
// goja counts JS call FRAMES. Espruino (the Shelly firmware) is bounded by C
// stack consumed by expression nesting (#496), so a goja depth is only a
// rough proxy for the device's headroom, never a calibrated equivalent.
//
// goja reports an overflow as a *goja.StackOverflowError returned by the
// failing Run/Callable invocation; it is NOT a JS exception, so a JS
// try/catch in the script cannot intercept it. Many emulator callback sites
// discard the error returned by the callable, so an overflow inside a
// Go-invoked callback would otherwise vanish. assertFunction records every
// overflow so tests can detect it via StackOverflows().

var stackOverflows atomic.Int64

// StackOverflows returns the number of goja stack overflows seen by any
// emulated runtime in this process since ResetStackOverflows. Process-global:
// use it only from sequential (non t.Parallel) tests.
func StackOverflows() int64 { return stackOverflows.Load() }

// ResetStackOverflows zeroes the StackOverflows counter.
func ResetStackOverflows() { stackOverflows.Store(0) }

func noteStackOverflow(err error) {
	var so *goja.StackOverflowError
	if errors.As(err, &so) {
		stackOverflows.Add(1)
	}
}

// assertFunction is goja.AssertFunction whose callable also records stack
// overflows (see above).
func assertFunction(v goja.Value) (goja.Callable, bool) {
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return nil, false
	}
	return func(this goja.Value, args ...goja.Value) (goja.Value, error) {
		ret, err := fn(this, args...)
		noteStackOverflow(err)
		return ret, err
	}, true
}
