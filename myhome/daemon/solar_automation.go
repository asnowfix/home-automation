package daemon

import (
	"context"
	"fmt"
	"time"

	beem "github.com/asnowfix/home-automation/pkg/beem"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
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
	DailyTargetSec  int64         // soft-stop target in seconds; 0 = no soft-stop check
	MaxRotationSec  int64         // hard-ceiling in seconds; 0 = no ceiling
}

// PumpController abstracts switch control so the state machine can be tested without MQTT.
type PumpController interface {
	SetPump(ctx context.Context, on bool) error
}

// SolarAutomation subscribes to Beem power samples and controls the pool pump
// using a hysteresis state machine:
//
//	IDLE  →  (solar_w ≥ StartThresholdW  for  StartDelay
//	          AND  runtime < MaxRotationSec)                         →  RUNNING
//	RUNNING  →  (runtime ≥ MaxRotationSec)                           →  IDLE  [hard ceiling]
//	RUNNING  →  (runtime ≥ DailyTargetSec  AND  solar_w < StartThresholdW)
//	                                                                  →  IDLE  [soft stop]
//	RUNNING  →  (solar_w < StopThresholdW  for  StopDelay)           →  IDLE  [solar loss]
//
// See docs/beem-energy.md "Soft stop vs. hard ceiling" for the rationale:
// DailyTargetSec only stops the pump once solar has also dropped, so free
// solar energy keeps over-filtering rather than going to waste; MaxRotationSec
// is an absolute ceiling that always stops (and blocks new solar starts).
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
					sa.log.Info("Hard ceiling already reached; not starting pump via solar")
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
		runtime, haveRuntime := sa.dailyRuntimeSec(ctx)

		// Hard ceiling: always stop once max_rotation_sec is reached, regardless of solar.
		if haveRuntime && sa.cfg.MaxRotationSec > 0 && runtime >= sa.cfg.MaxRotationSec {
			sa.log.Info("Hard ceiling reached, stopping pump",
				"runtime_sec", runtime, "max_rotation_sec", sa.cfg.MaxRotationSec)
			if err := sa.pump.SetPump(ctx, false); err != nil {
				sa.log.Error(err, "Failed to stop pump")
			}
			return pumpIdle, zero, zero
		}

		// Soft stop: the normal filtration goal stops the pump only once solar
		// has also dropped below the start threshold — while solar still
		// produces, free energy keeps over-filtering past the daily target.
		if haveRuntime && sa.cfg.DailyTargetSec > 0 && runtime >= sa.cfg.DailyTargetSec &&
			sample.SolarW < sa.cfg.StartThresholdW {
			sa.log.Info("Daily target reached and solar gone, stopping pump (soft stop)",
				"runtime_sec", runtime, "daily_target_sec", sa.cfg.DailyTargetSec, "solar_w", sample.SolarW)
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

// canStart returns true when a solar-driven start is permitted: the pump must
// not already have reached the hard ceiling for today (see docs/beem-energy.md
// "Interaction with existing schedule"). Reaching the soft daily target alone
// does not block a start — free solar energy is used to over-filter.
func (sa *SolarAutomation) canStart(ctx context.Context) bool {
	if sa.cfg.MaxRotationSec <= 0 {
		return true
	}
	runtime, ok := sa.dailyRuntimeSec(ctx)
	if !ok {
		return true
	}
	return runtime < sa.cfg.MaxRotationSec
}

// dailyRuntimeSec returns today's pump runtime in seconds and whether it could
// be determined. It returns (0, false) when there is no tracker or the query fails.
func (sa *SolarAutomation) dailyRuntimeSec(ctx context.Context) (int64, bool) {
	if sa.tracker == nil {
		return 0, false
	}
	runtime, err := sa.tracker.DailyRuntimeSec(ctx)
	if err != nil {
		sa.log.Error(err, "Failed to read daily pump runtime")
		return 0, false
	}
	return runtime, true
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

// poolRuntimeKVSKeys are the pool device KVS keys read at solar-automation
// startup to derive daily_target_sec / max_rotation_sec from the configured
// turnover multipliers — see docs/beem-energy.md "Runtime target computation".
// pool-pump.js already populates these for its own scheduling; the daemon only
// reads them, never writes them (KVS remains exclusively the JS script's domain).
const (
	kvsKeyPoolVolume  = "script/pool-pump/pool-volume"
	kvsKeyMaxFlowRate = "script/pool-pump/max-flow-rate"
	kvsKeyMaxRpm      = "script/pool-pump/max-rpm"
	kvsKeySpeed       = "script/pool-pump/speed"
)

// computeRuntimeTargets derives daily_target_sec (soft stop) and max_rotation_sec
// (hard ceiling) from the turnover multipliers and the pool device KVS:
//
//	flow_rate        = max_flow_rate × (speed / max_rpm)            [m³/h]
//	daily_target_sec = pool_volume × min_turnover / flow_rate × 3600 [s]
//	max_rotation_sec = pool_volume × max_turnover / flow_rate × 3600 [s]
func computeRuntimeTargets(ctx context.Context, log logr.Logger, device *shellyapi.Device, minTurnover, maxTurnover float64) (dailyTargetSec, maxRotationSec int64, err error) {
	via := types.ChannelMqtt

	poolVolume, err := readPoolKVSFloat(ctx, log, device, via, kvsKeyPoolVolume)
	if err != nil {
		return 0, 0, err
	}
	maxFlowRate, err := readPoolKVSFloat(ctx, log, device, via, kvsKeyMaxFlowRate)
	if err != nil {
		return 0, 0, err
	}
	maxRpm, err := readPoolKVSFloat(ctx, log, device, via, kvsKeyMaxRpm)
	if err != nil {
		return 0, 0, err
	}
	speed, err := readPoolKVSFloat(ctx, log, device, via, kvsKeySpeed)
	if err != nil {
		return 0, 0, err
	}

	if poolVolume <= 0 || maxFlowRate <= 0 || maxRpm <= 0 || speed <= 0 {
		return 0, 0, fmt.Errorf(
			"invalid pool KVS values: pool_volume=%v max_flow_rate=%v max_rpm=%v speed=%v",
			poolVolume, maxFlowRate, maxRpm, speed,
		)
	}

	flowRate := maxFlowRate * (speed / maxRpm)
	dailyTargetSec = int64(poolVolume * minTurnover / flowRate * 3600)
	maxRotationSec = int64(poolVolume * maxTurnover / flowRate * 3600)
	return dailyTargetSec, maxRotationSec, nil
}

// readPoolKVSFloat reads a single numeric KVS value from the pool device.
// KVS values are always stored as plain strings on Shelly devices.
func readPoolKVSFloat(ctx context.Context, log logr.Logger, device *shellyapi.Device, via types.Channel, key string) (float64, error) {
	resp, err := kvs.GetValue(ctx, log, via, device, key)
	if err != nil {
		return 0, fmt.Errorf("KVS.Get %s: %w", key, err)
	}
	var v float64
	if _, err := fmt.Sscanf(resp.Value, "%f", &v); err != nil {
		return 0, fmt.Errorf("parse KVS %s=%q: %w", key, resp.Value, err)
	}
	return v, nil
}
