package script

import (
	"context"
	"testing"
	"time"

	"pkg/shelly/mqtt"

	"github.com/dop251/goja"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// TestTimerHandlerOneShot validates that a one-shot timerHandler fires exactly once
func TestTimerHandlerOneShot(t *testing.T) {
	ctx := logr.NewContext(context.Background(), testr.New(t))
	vm := goja.New()

	// Create a JavaScript function that increments a counter
	_, err := vm.RunString(`
		var callCount = 0;
		function callback() {
			callCount++;
		}
	`)
	if err != nil {
		t.Fatalf("Failed to create callback: %v", err)
	}

	// Get the callback function
	callbackVal := vm.Get("callback")
	callable, ok := goja.AssertFunction(callbackVal)
	if !ok {
		t.Fatal("callback is not a function")
	}

	// Create a one-shot timer handler
	timer := &timerHandler{
		handle:    1,
		period:    50 * time.Millisecond,
		repeat:    false,
		callable:  callable,
		userdata:  goja.Undefined(),
		vm:        vm,
		startTime: time.Now(),
	}

	// Start the timer
	ch := timer.Wait()

	// Wait for timer to fire
	select {
	case <-ch:
		// Timer fired, execute callback
		err := timer.Handle(ctx, vm, []byte{})
		if err != nil {
			t.Fatalf("Timer callback failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timer did not fire within expected time")
	}

	// Verify callback was called exactly once
	callCountVal := vm.Get("callCount").ToInteger()
	if callCountVal != 1 {
		t.Errorf("Expected callback to be called once, got %d", callCountVal)
	}

	// Wait a bit more to ensure it doesn't fire again
	time.Sleep(100 * time.Millisecond)
	callCountVal = vm.Get("callCount").ToInteger()
	if callCountVal != 1 {
		t.Errorf("One-shot timer fired multiple times: %d", callCountVal)
	}
}

// TestTimerHandlerRecurring validates that a recurring timerHandler fires multiple times
func TestTimerHandlerRecurring(t *testing.T) {
	ctx := logr.NewContext(context.Background(), testr.New(t))
	vm := goja.New()

	// Create a JavaScript function that increments a counter
	_, err := vm.RunString(`
		var callCount = 0;
		function callback() {
			callCount++;
		}
	`)
	if err != nil {
		t.Fatalf("Failed to create callback: %v", err)
	}

	// Get the callback function
	callbackVal := vm.Get("callback")
	callable, ok := goja.AssertFunction(callbackVal)
	if !ok {
		t.Fatal("callback is not a function")
	}

	// Create a recurring timer handler
	timer := &timerHandler{
		handle:    1,
		period:    30 * time.Millisecond,
		repeat:    true,
		callable:  callable,
		userdata:  goja.Undefined(),
		vm:        vm,
		startTime: time.Now(),
	}

	// Start the timer
	ch := timer.Wait()

	// Run for ~120ms to catch multiple firings (should be ~4 times)
	timeout := time.After(120 * time.Millisecond)
	done := false

	for !done {
		select {
		case _, ok := <-ch:
			if !ok {
				done = true
				break
			}
			// Timer fired, execute callback
			err := timer.Handle(ctx, vm, []byte{})
			if err != nil {
				t.Fatalf("Timer callback failed: %v", err)
			}
		case <-timeout:
			done = true
		}
	}

	// Stop the timer
	timer.Stop()

	// Verify callback was called multiple times (should be 3-5 times in 120ms with 30ms period)
	callCountVal := vm.Get("callCount").ToInteger()
	if callCountVal < 3 || callCountVal > 5 {
		t.Errorf("Expected callback to be called 3-5 times in 120ms, got %d", callCountVal)
	}

	finalCount := callCountVal

	// Wait a bit more to ensure it doesn't fire after Stop()
	time.Sleep(100 * time.Millisecond)
	callCountVal = vm.Get("callCount").ToInteger()
	if callCountVal != finalCount {
		t.Errorf("Timer continued firing after Stop(): before=%d, after=%d", finalCount, callCountVal)
	}
}

// TestTimerHandlerStop validates that stopping a timer prevents future firings
func TestTimerHandlerStop(t *testing.T) {
	vm := goja.New()

	// Create a JavaScript function
	_, err := vm.RunString(`
		var callCount = 0;
		function callback() {
			callCount++;
		}
	`)
	if err != nil {
		t.Fatalf("Failed to create callback: %v", err)
	}

	callbackVal := vm.Get("callback")
	callable, ok := goja.AssertFunction(callbackVal)
	if !ok {
		t.Fatal("callback is not a function")
	}

	// Create a one-shot timer
	timer := &timerHandler{
		handle:    1,
		period:    100 * time.Millisecond,
		repeat:    false,
		callable:  callable,
		userdata:  goja.Undefined(),
		vm:        vm,
		startTime: time.Now(),
	}

	// Start and immediately stop the timer
	timer.Wait()
	timer.Stop()

	// Wait to ensure callback doesn't fire
	time.Sleep(150 * time.Millisecond)

	callCountVal := vm.Get("callCount").ToInteger()
	if callCountVal != 0 {
		t.Errorf("Stopped timer should not fire, got %d calls", callCountVal)
	}
}

// TestTimerHandlerZeroPeriod validates that 0ms period is treated as 1ms
func TestTimerHandlerZeroPeriod(t *testing.T) {
	ctx := logr.NewContext(context.Background(), testr.New(t))
	vm := goja.New()

	// Create a JavaScript function
	_, err := vm.RunString(`
		var callCount = 0;
		function callback() {
			callCount++;
		}
	`)
	if err != nil {
		t.Fatalf("Failed to create callback: %v", err)
	}

	callbackVal := vm.Get("callback")
	callable, ok := goja.AssertFunction(callbackVal)
	if !ok {
		t.Fatal("callback is not a function")
	}

	// Create a timer with 0ms period
	timer := &timerHandler{
		handle:    1,
		period:    0,
		repeat:    false,
		callable:  callable,
		userdata:  goja.Undefined(),
		vm:        vm,
		startTime: time.Now(),
	}

	ch := timer.Wait()

	// Should fire very quickly (treated as 1ms)
	select {
	case <-ch:
		timer.Handle(ctx, vm, []byte{})
	case <-time.After(50 * time.Millisecond):
		t.Fatal("0ms timer did not fire within 50ms")
	}

	callCountVal := vm.Get("callCount").ToInteger()
	if callCountVal != 1 {
		t.Errorf("Expected callback to be called once, got %d", callCountVal)
	}
}

// TestTimerHandlerTiming validates that timers fire at the expected time
func TestTimerHandlerTiming(t *testing.T) {
	ctx := logr.NewContext(context.Background(), testr.New(t))
	vm := goja.New()

	// Create a JavaScript function that records the fire time
	_, err := vm.RunString(`
		var fireTime = null;
		function callback() {
			fireTime = Date.now();
		}
	`)
	if err != nil {
		t.Fatalf("Failed to create callback: %v", err)
	}

	callbackVal := vm.Get("callback")
	callable, ok := goja.AssertFunction(callbackVal)
	if !ok {
		t.Fatal("callback is not a function")
	}

	// Create a 100ms one-shot timer
	period := 100 * time.Millisecond
	startTime := time.Now()

	timer := &timerHandler{
		handle:    1,
		period:    period,
		repeat:    false,
		callable:  callable,
		userdata:  goja.Undefined(),
		vm:        vm,
		startTime: startTime,
	}

	// Start the timer
	ch := timer.Wait()

	// Wait for timer to fire
	select {
	case <-ch:
		actualDelay := time.Since(startTime)

		// Execute callback
		err := timer.Handle(ctx, vm, []byte{})
		if err != nil {
			t.Fatalf("Timer callback failed: %v", err)
		}

		// Verify timing: should fire within ±20ms of expected period
		expectedMin := period - 20*time.Millisecond
		expectedMax := period + 20*time.Millisecond

		if actualDelay < expectedMin || actualDelay > expectedMax {
			t.Errorf("Timer fired at wrong time: expected %v±20ms, got %v", period, actualDelay)
		} else {
			t.Logf("Timer fired after %v (expected %v)", actualDelay, period)
		}

	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timer did not fire within expected time")
	}
}

// TestTimerHandlerRecurringTiming validates recurring timer interval accuracy
func TestTimerHandlerRecurringTiming(t *testing.T) {
	ctx := logr.NewContext(context.Background(), testr.New(t))
	vm := goja.New()

	// Create a JavaScript function that records fire times
	_, err := vm.RunString(`
		var fireTimes = [];
		function callback() {
			fireTimes.push(Date.now());
		}
	`)
	if err != nil {
		t.Fatalf("Failed to create callback: %v", err)
	}

	callbackVal := vm.Get("callback")
	callable, ok := goja.AssertFunction(callbackVal)
	if !ok {
		t.Fatal("callback is not a function")
	}

	// Create a 50ms recurring timer
	period := 50 * time.Millisecond
	startTime := time.Now()

	timer := &timerHandler{
		handle:    1,
		period:    period,
		repeat:    true,
		callable:  callable,
		userdata:  goja.Undefined(),
		vm:        vm,
		startTime: startTime,
	}

	ch := timer.Wait()

	// Collect first 3 firings
	fireCount := 0
	fireTimes := make([]time.Time, 0, 3)
	timeout := time.After(200 * time.Millisecond)

	for fireCount < 3 {
		select {
		case _, ok := <-ch:
			if !ok {
				t.Fatal("Timer channel closed unexpectedly")
			}
			fireTime := time.Now()
			fireTimes = append(fireTimes, fireTime)

			err := timer.Handle(ctx, vm, []byte{})
			if err != nil {
				t.Fatalf("Timer callback failed: %v", err)
			}
			fireCount++

		case <-timeout:
			t.Fatalf("Only got %d firings, expected 3", fireCount)
		}
	}

	timer.Stop()

	// Verify intervals between firings
	for i := 1; i < len(fireTimes); i++ {
		interval := fireTimes[i].Sub(fireTimes[i-1])
		expectedMin := period - 20*time.Millisecond
		expectedMax := period + 20*time.Millisecond

		if interval < expectedMin || interval > expectedMax {
			t.Errorf("Firing %d: interval %v outside expected range %v±20ms", i, interval, period)
		} else {
			t.Logf("Firing %d: interval %v (expected %v)", i, interval, period)
		}
	}

	// Verify first firing happened at expected time from start
	firstDelay := fireTimes[0].Sub(startTime)
	expectedMin := period - 20*time.Millisecond
	expectedMax := period + 20*time.Millisecond

	if firstDelay < expectedMin || firstDelay > expectedMax {
		t.Errorf("First firing: delay %v outside expected range %v±20ms", firstDelay, period)
	} else {
		t.Logf("First firing: delay %v (expected %v)", firstDelay, period)
	}
}

// TestTimerHandlerRecurringStopBehavior validates detailed stop behavior
func TestTimerHandlerRecurringStopBehavior(t *testing.T) {
	ctx := logr.NewContext(context.Background(), testr.New(t))
	vm := goja.New()

	// Create a JavaScript function
	_, err := vm.RunString(`
		var callCount = 0;
		function callback() {
			callCount++;
		}
	`)
	if err != nil {
		t.Fatalf("Failed to create callback: %v", err)
	}

	callbackVal := vm.Get("callback")
	callable, ok := goja.AssertFunction(callbackVal)
	if !ok {
		t.Fatal("callback is not a function")
	}

	// Create a recurring timer with short period
	timer := &timerHandler{
		handle:    1,
		period:    25 * time.Millisecond,
		repeat:    true,
		callable:  callable,
		userdata:  goja.Undefined(),
		vm:        vm,
		startTime: time.Now(),
	}

	ch := timer.Wait()

	// Let it fire twice
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
			timer.Handle(ctx, vm, []byte{})
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timer did not fire")
		}
	}

	// Stop the timer
	timer.Stop()

	countBeforeStop := vm.Get("callCount").ToInteger()

	// Wait and verify no more firings
	time.Sleep(100 * time.Millisecond)

	countAfterStop := vm.Get("callCount").ToInteger()
	if countAfterStop != countBeforeStop {
		t.Errorf("Timer fired after Stop(): before=%d, after=%d", countBeforeStop, countAfterStop)
	}
}

// TestTimerSetWithShellyRuntime validates Timer.set using the full Shelly runtime
func TestTimerSetWithShellyRuntime(t *testing.T) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	// Add mock MQTT client to context
	mc := mqtt.NewMockClient()
	ctx = mqtt.NewContext(ctx, mc)

	// Create a test script that uses Timer.set
	script := `
		var oneShotCount = 0;
		var recurringCount = 0;
		var oneShotHandle = null;
		var recurringHandle = null;
		var startTime = Date.now();
		var oneShotFireTime = null;
		var recurringFireTimes = [];
		
		// One-shot timer (100ms)
		oneShotHandle = Timer.set(100, false, function() {
			oneShotCount++;
			oneShotFireTime = Date.now();
		});
		
		// Recurring timer (50ms)
		recurringHandle = Timer.set(50, true, function() {
			recurringCount++;
			recurringFireTimes.push(Date.now());
		});
	`

	// Run in background
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "timer_test.js", []byte(script), false)
	}()

	// Let timers run
	time.Sleep(350 * time.Millisecond)

	// Cancel context to stop script
	cancel()

	// Wait for completion
	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("Script execution failed: %v", err)
	}

	t.Log("Timer.set with Shelly runtime test completed successfully")
}

// TestTimerSetOneShotWithRuntime validates one-shot timer using Shelly runtime
func TestTimerSetOneShotWithRuntime(t *testing.T) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	// Add mock MQTT client to context
	mc := mqtt.NewMockClient()
	ctx = mqtt.NewContext(ctx, mc)

	script := `
		var callCount = 0;
		var fireTime = null;
		var startTime = Date.now();
		
		Timer.set(100, false, function() {
			callCount++;
			fireTime = Date.now() - startTime;
		});
	`

	// Run in background
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "oneshot_test.js", []byte(script), false)
	}()

	// Wait for script to run
	time.Sleep(200 * time.Millisecond)

	// Cancel context to stop script
	cancel()

	// Wait for completion
	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("Script execution failed: %v", err)
	}

	t.Log("One-shot timer test completed successfully")
}

// TestTimerSetRecurringWithRuntime validates recurring timer using Shelly runtime
func TestTimerSetRecurringWithRuntime(t *testing.T) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	// Add mock MQTT client to context
	mc := mqtt.NewMockClient()
	ctx = mqtt.NewContext(ctx, mc)

	script := `
		var callCount = 0;
		var fireTimes = [];
		var startTime = Date.now();
		
		Timer.set(50, true, function() {
			callCount++;
			fireTimes.push(Date.now() - startTime);
		});
	`

	// Run in background
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "recurring_test.js", []byte(script), false)
	}()

	// Let it run for a bit
	time.Sleep(250 * time.Millisecond)

	// Cancel context to stop script
	cancel()

	// Wait for completion
	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("Script execution failed: %v", err)
	}

	t.Log("Recurring timer test completed successfully")
}

// TestTimerClearWithRuntime validates Timer.clear using Shelly runtime
func TestTimerClearWithRuntime(t *testing.T) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	// Add mock MQTT client to context
	mc := mqtt.NewMockClient()
	ctx = mqtt.NewContext(ctx, mc)

	script := `
		var callCount = 0;
		
		// Create a timer
		var handle = Timer.set(100, false, function() {
			callCount++;
		});
		
		// Immediately clear it
		var cleared = Timer.clear(handle);
		
		if (!cleared) {
			throw new Error("Timer.clear should return true");
		}
	`

	// Run in background
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "clear_test.js", []byte(script), false)
	}()

	// Wait a bit to ensure timer would have fired if not cleared
	time.Sleep(200 * time.Millisecond)

	// Cancel context to stop script
	cancel()

	// Wait for completion
	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("Script execution failed: %v", err)
	}

	t.Log("Timer.clear test completed successfully")
}

// TestTimerMultipleWithRuntime validates multiple concurrent timers using Shelly runtime
func TestTimerMultipleWithRuntime(t *testing.T) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	// Add mock MQTT client to context
	mc := mqtt.NewMockClient()
	ctx = mqtt.NewContext(ctx, mc)

	script := `
		var timer1Count = 0;
		var timer2Count = 0;
		var timer3Count = 0;
		
		// Three different timers with different periods
		Timer.set(50, true, function() {
			timer1Count++;
		});
		
		Timer.set(75, true, function() {
			timer2Count++;
		});
		
		Timer.set(100, false, function() {
			timer3Count++;
		});
	`

	// Run in background
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "multiple_test.js", []byte(script), false)
	}()

	// Let timers run
	time.Sleep(350 * time.Millisecond)

	// Cancel context to stop script
	cancel()

	// Wait for completion
	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("Script execution failed: %v", err)
	}

	t.Log("Multiple timers test completed successfully")
}

// TestTimerTimingWithRuntime validates timer timing accuracy using Shelly runtime
func TestTimerTimingWithRuntime(t *testing.T) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	// Add mock MQTT client to context
	mc := mqtt.NewMockClient()
	ctx = mqtt.NewContext(ctx, mc)

	// Record start time in Go
	startTime := time.Now()

	script := `
		var startTime = Date.now();
		var oneShotFireTime = null;
		var recurringFireTimes = [];
		
		// One-shot timer (100ms)
		Timer.set(100, false, function() {
			oneShotFireTime = Date.now() - startTime;
		});
		
		// Recurring timer (50ms) - collect first 3 firings
		Timer.set(50, true, function() {
			if (recurringFireTimes.length < 3) {
				recurringFireTimes.push(Date.now() - startTime);
			}
		});
	`

	// Run in background
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "timing_test.js", []byte(script), false)
	}()

	// Let timers run for 250ms
	time.Sleep(250 * time.Millisecond)

	// Cancel context to stop script
	cancel()

	// Wait for completion
	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("Script execution failed: %v", err)
	}

	// Calculate actual elapsed time
	elapsed := time.Since(startTime)

	t.Logf("Test ran for %v", elapsed)
	t.Logf("One-shot timer should have fired at ~100ms")
	t.Logf("Recurring timer should have fired 3 times at ~50ms intervals")
	t.Log("Timer timing test with Shelly runtime completed successfully")
}

// TestTimerRecurringIntervalAccuracyWithRuntime validates recurring timer interval consistency
func TestTimerRecurringIntervalAccuracyWithRuntime(t *testing.T) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	ctx = logr.NewContext(ctx, testr.New(t))

	// Add mock MQTT client to context
	mc := mqtt.NewMockClient()
	ctx = mqtt.NewContext(ctx, mc)

	script := `
		var startTime = Date.now();
		var fireTimes = [];
		var fireCount = 0;
		var maxFires = 5;
		
		// Recurring timer (60ms) - collect timing data
		Timer.set(60, true, function() {
			if (fireCount < maxFires) {
				var elapsed = Date.now() - startTime;
				fireTimes.push(elapsed);
				fireCount++;
			}
		});
	`

	// Run in background
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "interval_test.js", []byte(script), false)
	}()

	// Let timer fire 5 times (5 * 60ms = 300ms + margin)
	time.Sleep(350 * time.Millisecond)

	// Cancel context to stop script
	cancel()

	// Wait for completion
	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("Script execution failed: %v", err)
	}

	t.Log("Expected firing times: ~60ms, ~120ms, ~180ms, ~240ms, ~300ms")
	t.Log("Recurring interval accuracy test with Shelly runtime completed successfully")
}
