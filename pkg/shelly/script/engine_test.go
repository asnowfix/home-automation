package script

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/dop251/goja"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// TestEngineDispatch verifies that a host can call into a running script
// through Dispatch(): the script registers a global function, and the host
// invokes it from another goroutine.
func TestEngineDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	src := `
		var calls = 0;
		function greet(name) {
			calls++;
			return "hello " + name + " #" + calls;
		}
	`

	eng, err := NewEngine(ctx, "engine_test.js", []byte(src), EngineOptions{
		EnableExternalCalls: true,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- eng.Start(ctx)
	}()

	results := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		err := eng.Dispatch(ctx, func(vm *goja.Runtime) {
			fn, ok := goja.AssertFunction(vm.Get("greet"))
			if !ok {
				t.Error("greet is not a function")
				return
			}
			v, err := fn(goja.Undefined(), vm.ToValue("daemon"))
			if err != nil {
				t.Errorf("greet() failed: %v", err)
				return
			}
			results = append(results, v.String())
		})
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
	}

	if len(results) != 2 || results[0] != "hello daemon #1" || results[1] != "hello daemon #2" {
		t.Fatalf("unexpected results: %v", results)
	}

	cancel()
	if err := <-done; err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("engine exited with error: %v", err)
	}
}

// TestEngineStopsOnContextCancel verifies the engine event loop ends when the
// context is cancelled (same lifecycle as Run()).
func TestEngineStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	mqtt.ResetClient()
	mqtt.SetClient(mqtt.NewMockClient())
	t.Cleanup(mqtt.ResetClient)

	eng, err := NewEngine(ctx, "idle_test.js", []byte(`var x = 1;`), EngineOptions{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- eng.Start(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("engine did not stop on context cancel")
	}
}
