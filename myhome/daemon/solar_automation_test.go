package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	beem "github.com/asnowfix/home-automation/pkg/beem"
	"github.com/go-logr/logr"
)

// mockPumpController records SetPump calls so tests can assert pump state transitions.
type mockPumpController struct {
	calls []bool // true=on, false=off
}

func (m *mockPumpController) SetPump(_ context.Context, on bool) error {
	m.calls = append(m.calls, on)
	return nil
}

func (m *mockPumpController) lastCall() (on bool, ok bool) {
	if len(m.calls) == 0 {
		return false, false
	}
	return m.calls[len(m.calls)-1], true
}

// send pushes a sample to ch and gives the goroutine time to process it.
func send(ch chan<- beem.PowerSample, w float64) {
	ch <- beem.PowerSample{SolarW: w, Source: "test", TS: time.Now()}
	time.Sleep(20 * time.Millisecond)
}

func defaultCfg() SolarConfig {
	return SolarConfig{
		StartThresholdW: 500,
		StopThresholdW:  200,
		StartDelay:      0, // zero delay → deterministic single-sample transitions
		StopDelay:       0,
		DailyTargetSec:  0,
	}
}

// TestSolarAutomation_ImmediateStartStop verifies that with zero delays
// the pump starts on the first above-threshold sample and stops on the
// first below-stop-threshold sample.
func TestSolarAutomation_ImmediateStartStop(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, defaultCfg())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	// First sample at or above start threshold → pump should turn on.
	send(ch, 600)
	on, ok := pump.lastCall()
	if !ok || !on {
		t.Fatalf("expected pump ON after first sample above threshold; calls=%v", pump.calls)
	}

	// Sample below stop threshold → pump should turn off.
	send(ch, 100)
	on, ok = pump.lastCall()
	if !ok || on {
		t.Fatalf("expected pump OFF after sample below stop threshold; calls=%v", pump.calls)
	}
}

// TestSolarAutomation_StayRunningBetweenThresholds ensures the pump keeps running
// when solar is between stop and start thresholds while already running.
func TestSolarAutomation_StayRunningBetweenThresholds(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, defaultCfg())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600) // start pump
	if n := len(pump.calls); n != 1 {
		t.Fatalf("expected 1 call after start, got %d", n)
	}

	// Send several samples between 200 and 500 (hysteresis band) — no change expected.
	for i := 0; i < 5; i++ {
		send(ch, 350)
	}
	if n := len(pump.calls); n != 1 {
		t.Errorf("expected no change in hysteresis band; calls=%v", pump.calls)
	}
}

// TestSolarAutomation_StartDelayPreventsEarlyStart verifies that a start delay
// prevents the pump from turning on until the threshold has been held long enough.
func TestSolarAutomation_StartDelayPreventsEarlyStart(t *testing.T) {
	cfg := defaultCfg()
	cfg.StartDelay = 10 * time.Second // long delay

	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	// Single sample above threshold — delay not elapsed yet.
	send(ch, 600)
	if len(pump.calls) != 0 {
		t.Errorf("pump should not start before start delay elapses; calls=%v", pump.calls)
	}

	// Drop below threshold — resets the timer.
	send(ch, 100)

	// Back above threshold — still no start since delay is 10s.
	send(ch, 600)
	if len(pump.calls) != 0 {
		t.Errorf("pump should not start after threshold reset; calls=%v", pump.calls)
	}
}

// TestSolarAutomation_StopDelayPreventsEarlyStop verifies that a stop delay
// prevents the pump from turning off on a brief solar dip.
func TestSolarAutomation_StopDelayPreventsEarlyStop(t *testing.T) {
	cfg := defaultCfg()
	cfg.StopDelay = 10 * time.Second // long delay

	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600) // start pump
	if n := len(pump.calls); n != 1 || !pump.calls[0] {
		t.Fatalf("expected pump ON; calls=%v", pump.calls)
	}

	// Brief dip below stop threshold — stop delay hasn't elapsed.
	send(ch, 50)
	if n := len(pump.calls); n != 1 {
		t.Errorf("pump should not stop before stop delay elapses; calls=%v", pump.calls)
	}

	// Recovery above stop threshold — stop timer should reset.
	send(ch, 400)
	if n := len(pump.calls); n != 1 {
		t.Errorf("pump should not stop after solar recovery; calls=%v", pump.calls)
	}
}

// TestSolarAutomation_ContextCancelStopsPump verifies that cancelling the context
// turns off the pump if it was running.
func TestSolarAutomation_ContextCancelStopsPump(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, defaultCfg())

	ctx, cancel := context.WithCancel(context.Background())
	sa.Start(ctx)

	send(ch, 600) // start pump
	if len(pump.calls) == 0 || !pump.calls[0] {
		t.Fatalf("expected pump ON; calls=%v", pump.calls)
	}

	cancel() // cancel context → goroutine should emit an OFF command
	time.Sleep(50 * time.Millisecond)

	on, ok := pump.lastCall()
	if !ok || on {
		t.Errorf("expected pump OFF after context cancel; calls=%v", pump.calls)
	}
}

// seedDailyRuntime records a single ON→OFF interval of the given length,
// starting one hour into today, so PoolRuntimeTracker.DailyRuntimeSec reports
// exactly `seconds` for the rest of the test.
func seedDailyRuntime(t *testing.T, s *events.Storage, deviceID string, seconds float64) {
	t.Helper()
	base := float64(time.Now().Truncate(24*time.Hour).Unix()) + 3600
	insertSwitchEvent(t, s, deviceID, "switch.on", base)
	insertSwitchEvent(t, s, deviceID, "switch.off", base+seconds)
}

// TestSolarAutomation_SoftStopWhenTargetReachedAndSolarGone verifies the
// soft-stop transition: once the daily target is reached, the pump stops only
// when solar also drops below the start threshold.
func TestSolarAutomation_SoftStopWhenTargetReachedAndSolarGone(t *testing.T) {
	s := newTestEventsStorage(t)
	seedDailyRuntime(t, s, "pool-device", 4000) // already past the soft target, below the ceiling
	tracker := NewPoolRuntimeTracker(logr.Discard(), s, "pool-device")

	cfg := defaultCfg()
	cfg.DailyTargetSec = 3600
	cfg.MaxRotationSec = 7200

	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, tracker, pump, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600) // runtime (4000s) < ceiling (7200s) → solar start permitted
	if n := len(pump.calls); n != 1 || !pump.calls[0] {
		t.Fatalf("expected pump ON; calls=%v", pump.calls)
	}

	// Solar drops below the start threshold while the soft target is already met.
	send(ch, 400)
	on, ok := pump.lastCall()
	if !ok || on {
		t.Errorf("expected soft stop (target reached + solar below start threshold); calls=%v", pump.calls)
	}
}

// TestSolarAutomation_KeepsRunningPastSoftTargetWhileSolarHigh verifies that
// reaching the soft daily target alone does not stop the pump — solar still
// above the start threshold means free energy keeps over-filtering.
func TestSolarAutomation_KeepsRunningPastSoftTargetWhileSolarHigh(t *testing.T) {
	s := newTestEventsStorage(t)
	seedDailyRuntime(t, s, "pool-device", 4000) // past the soft target, below the ceiling
	tracker := NewPoolRuntimeTracker(logr.Discard(), s, "pool-device")

	cfg := defaultCfg()
	cfg.DailyTargetSec = 3600
	cfg.MaxRotationSec = 7200

	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, tracker, pump, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600) // start
	if n := len(pump.calls); n != 1 {
		t.Fatalf("expected pump to start; calls=%v", pump.calls)
	}

	// Soft target already met, but solar stays well above the start threshold.
	for i := 0; i < 3; i++ {
		send(ch, 700)
	}
	if n := len(pump.calls); n != 1 {
		t.Errorf("expected pump to keep running past the soft target while solar is high; calls=%v", pump.calls)
	}
}

// TestSolarAutomation_HardCeilingStopsRegardlessOfSolar verifies that the hard
// ceiling stops a running pump even while solar production is still high.
func TestSolarAutomation_HardCeilingStopsRegardlessOfSolar(t *testing.T) {
	s := newTestEventsStorage(t)
	seedDailyRuntime(t, s, "pool-device", 8000) // past the hard ceiling
	tracker := NewPoolRuntimeTracker(logr.Discard(), s, "pool-device")

	cfg := defaultCfg()
	cfg.DailyTargetSec = 3600
	cfg.MaxRotationSec = 7200

	pump := &mockPumpController{}
	sa := NewSolarAutomation(logr.Discard(), make(chan beem.PowerSample), tracker, pump, cfg)

	// Exercise the running-state transition directly: the pump may have been
	// started by the JS schedule rather than the solar trigger, so the hard
	// ceiling must stop it regardless of how it got into the RUNNING state.
	sample := beem.PowerSample{SolarW: 900, Source: "test", TS: time.Now()}
	newState, _, _ := sa.step(context.Background(), sample, pumpRunning, time.Time{}, time.Time{})

	if newState != pumpIdle {
		t.Errorf("expected hard ceiling to stop the pump; state=%v", newState)
	}
	on, ok := pump.lastCall()
	if !ok || on {
		t.Errorf("expected pump OFF on hard ceiling; calls=%v", pump.calls)
	}
}

// TestSolarAutomation_CannotStartPastHardCeiling verifies that the solar trigger
// refuses to start the pump once the hard ceiling has already been reached.
func TestSolarAutomation_CannotStartPastHardCeiling(t *testing.T) {
	s := newTestEventsStorage(t)
	seedDailyRuntime(t, s, "pool-device", 8000) // past the hard ceiling
	tracker := NewPoolRuntimeTracker(logr.Discard(), s, "pool-device")

	cfg := defaultCfg()
	cfg.DailyTargetSec = 3600
	cfg.MaxRotationSec = 7200

	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, tracker, pump, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 900) // well above the start threshold, but the ceiling is already reached
	if len(pump.calls) != 0 {
		t.Errorf("expected no solar start once the hard ceiling is reached; calls=%v", pump.calls)
	}
}

// TestSolarAutomation_NoPumpStartWhenIdle verifies that the pump is not started
// when context is cancelled while in idle state.
func TestSolarAutomation_NoPumpStartWhenIdle(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, defaultCfg())

	ctx, cancel := context.WithCancel(context.Background())
	sa.Start(ctx)

	send(ch, 100) // below threshold — stays idle
	cancel()
	time.Sleep(50 * time.Millisecond)

	if len(pump.calls) != 0 {
		t.Errorf("no pump calls expected when cancelling from idle; calls=%v", pump.calls)
	}
}
