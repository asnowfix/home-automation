package daemon

import (
	"context"
	"fmt"
	"time"

	beem "github.com/asnowfix/home-automation/pkg/beem"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// SolarConfig holds the hysteresis parameters for solar-driven pump control.
type SolarConfig struct {
	StartThresholdW float64       // start pump when solar_w >= this
	StopThresholdW  float64       // stop pump when solar_w < this
	StartDelay      time.Duration // solar must hold above start threshold for this long
	StopDelay       time.Duration // solar must hold below stop threshold for this long
	DailyTargetSec  int64         // daily filtration target in seconds; 0 = no target check
}

// PumpController abstracts switch control so the state machine can be tested without MQTT.
type PumpController interface {
	SetPump(ctx context.Context, on bool) error
}

// SolarAutomation subscribes to Beem power samples and controls the pool pump
// using a hysteresis state machine:
//
//	IDLE  →  (solar_w ≥ StartThresholdW  for  StartDelay)  →  RUNNING
//	RUNNING  →  (solar_w < StopThresholdW  for  StopDelay)  →  IDLE
//	RUNNING  →  (DailyRemainingRuntimeSec ≤ 0)              →  IDLE
type SolarAutomation struct {
	log     logr.Logger
	powerCh <-chan beem.PowerSample
	tracker *PoolRuntimeTracker // nil ⇒ no daily-target check
	pump    PumpController
	cfg     SolarConfig
}

type pumpState int

const (
	pumpIdle    pumpState = iota
	pumpRunning pumpState = iota
)

func (s pumpState) String() string {
	if s == pumpRunning {
		return "running"
	}
	return "idle"
}

// NewSolarAutomation creates a SolarAutomation but does not start it.
func NewSolarAutomation(
	log logr.Logger,
	powerCh <-chan beem.PowerSample,
	tracker *PoolRuntimeTracker,
	pump PumpController,
	cfg SolarConfig,
) *SolarAutomation {
	return &SolarAutomation{
		log:     log.WithName("SolarAutomation"),
		powerCh: powerCh,
		tracker: tracker,
		pump:    pump,
		cfg:     cfg,
	}
}

// Start launches the state-machine goroutine. It returns immediately.
// The goroutine stops when ctx is cancelled.
func (sa *SolarAutomation) Start(ctx context.Context) {
	go sa.run(ctx)
}

func (sa *SolarAutomation) run(ctx context.Context) {
	var (
		state      = pumpIdle
		aboveStart time.Time
		belowStop  time.Time
	)
	sa.log.Info("Solar automation running",
		"start_threshold_w", sa.cfg.StartThresholdW,
		"stop_threshold_w", sa.cfg.StopThresholdW,
		"start_delay", sa.cfg.StartDelay,
		"stop_delay", sa.cfg.StopDelay,
		"daily_target_sec", sa.cfg.DailyTargetSec,
	)

	for {
		select {
		case <-ctx.Done():
			sa.log.Info("Solar automation stopping")
			if state == pumpRunning {
				if err := sa.pump.SetPump(context.Background(), false); err != nil {
					sa.log.Error(err, "Failed to stop pump on shutdown")
				}
			}
			return
		case sample, ok := <-sa.powerCh:
			if !ok {
				sa.log.Info("Power channel closed, stopping solar automation")
				if state == pumpRunning {
					if err := sa.pump.SetPump(context.Background(), false); err != nil {
						sa.log.Error(err, "Failed to stop pump on channel close")
					}
				}
				return
			}
			sa.log.V(1).Info("Solar sample", "solar_w", sample.SolarW, "state", state)
			state, aboveStart, belowStop = sa.step(ctx, sample, state, aboveStart, belowStop)
		}
	}
}

// step advances the state machine by one power sample.
// All time.Time zero values represent "timer not running".
func (sa *SolarAutomation) step(
	ctx context.Context,
	sample beem.PowerSample,
	state pumpState,
	aboveStart time.Time,
	belowStop time.Time,
) (pumpState, time.Time, time.Time) {
	var zero time.Time

	switch state {
	case pumpIdle:
		if sample.SolarW >= sa.cfg.StartThresholdW {
			if aboveStart.IsZero() {
				aboveStart = time.Now()
				sa.log.Info("Solar above start threshold, waiting for start delay",
					"solar_w", sample.SolarW,
					"threshold", sa.cfg.StartThresholdW,
					"start_delay", sa.cfg.StartDelay,
				)
			}
			if time.Since(aboveStart) >= sa.cfg.StartDelay {
				if !sa.canStart(ctx) {
					sa.log.Info("Daily target already reached; not starting pump")
					aboveStart = zero
					break
				}
				sa.log.Info("Starting pool pump (solar trigger)",
					"solar_w", sample.SolarW,
					"held_for", time.Since(aboveStart),
				)
				if err := sa.pump.SetPump(ctx, true); err != nil {
					sa.log.Error(err, "Failed to start pump")
					aboveStart = zero
					break
				}
				return pumpRunning, zero, zero
			}
		} else {
			if !aboveStart.IsZero() {
				sa.log.V(1).Info("Solar dropped below start threshold, resetting start timer",
					"solar_w", sample.SolarW)
				aboveStart = zero
			}
		}

	case pumpRunning:
		// Stop if daily filtration target is reached.
		if sa.targetReached(ctx) {
			sa.log.Info("Daily runtime target reached, stopping pump")
			if err := sa.pump.SetPump(ctx, false); err != nil {
				sa.log.Error(err, "Failed to stop pump")
			}
			return pumpIdle, zero, zero
		}

		if sample.SolarW < sa.cfg.StopThresholdW {
			if belowStop.IsZero() {
				belowStop = time.Now()
				sa.log.Info("Solar below stop threshold, waiting for stop delay",
					"solar_w", sample.SolarW,
					"threshold", sa.cfg.StopThresholdW,
					"stop_delay", sa.cfg.StopDelay,
				)
			}
			if time.Since(belowStop) >= sa.cfg.StopDelay {
				sa.log.Info("Stopping pool pump (solar too low)",
					"solar_w", sample.SolarW,
					"held_for", time.Since(belowStop),
				)
				if err := sa.pump.SetPump(ctx, false); err != nil {
					sa.log.Error(err, "Failed to stop pump")
					belowStop = zero
					break
				}
				return pumpIdle, zero, zero
			}
		} else {
			if !belowStop.IsZero() {
				sa.log.V(1).Info("Solar recovered above stop threshold, resetting stop timer",
					"solar_w", sample.SolarW)
				belowStop = zero
			}
		}
	}

	return state, aboveStart, belowStop
}

// canStart returns true when a solar-driven start is permitted.
// If no tracker or no target is configured, it always returns true.
func (sa *SolarAutomation) canStart(ctx context.Context) bool {
	if sa.tracker == nil || sa.cfg.DailyTargetSec <= 0 {
		return true
	}
	remaining, err := sa.tracker.RemainingRuntimeSec(ctx, sa.cfg.DailyTargetSec)
	if err != nil {
		sa.log.Error(err, "Failed to check remaining runtime; allowing start")
		return true
	}
	return remaining > 0
}

// targetReached returns true when the daily filtration target has been met.
func (sa *SolarAutomation) targetReached(ctx context.Context) bool {
	if sa.tracker == nil || sa.cfg.DailyTargetSec <= 0 {
		return false
	}
	remaining, err := sa.tracker.RemainingRuntimeSec(ctx, sa.cfg.DailyTargetSec)
	if err != nil {
		sa.log.Error(err, "Failed to check remaining runtime")
		return false
	}
	return remaining <= 0
}

// shellyPumpController implements PumpController using the Shelly Switch.Set RPC over MQTT.
type shellyPumpController struct {
	log    logr.Logger
	device *shellyapi.Device
}

// newShellyPumpController creates and MQTT-initializes a shellyPumpController for the given device ID.
// Must be called after shelly.Init() and the MQTT client have been set up.
func newShellyPumpController(ctx context.Context, log logr.Logger, deviceID string) (*shellyPumpController, error) {
	d, err := shellyapi.NewDeviceFromMqttId(ctx, log, deviceID)
	if err != nil {
		return nil, fmt.Errorf("create device %s: %w", deviceID, err)
	}
	sd, ok := d.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("unexpected device type %T", d)
	}
	if err := sd.Init(ctx); err != nil {
		return nil, fmt.Errorf("init device %s: %w", deviceID, err)
	}
	return &shellyPumpController{log: log.WithName("PumpController"), device: sd}, nil
}

func (c *shellyPumpController) SetPump(ctx context.Context, on bool) error {
	_, err := sswitch.Set(ctx, c.device, types.ChannelMqtt, 0, on)
	if err != nil {
		return fmt.Errorf("Switch.Set on=%v: %w", on, err)
	}
	c.log.Info("Switch.Set sent", "device_id", c.device.Id(), "on", on)
	return nil
}
