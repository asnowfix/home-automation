package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
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

// RuntimeTracker reports how long the pool pump has run today.
type RuntimeTracker interface {
	DailyRuntimeSec(ctx context.Context) (int64, error)
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
	tracker RuntimeTracker // nil ⇒ no daily-target check
	pump    PumpController
	cfg     SolarConfig

	// events/deviceID are optional (set via WithEvents); when nil/empty,
	// recordNotice is a no-op, so existing callers and tests that never
	// call WithEvents are unaffected.
	events   *events.Service
	deviceID string
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
	tracker RuntimeTracker,
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

// WithEvents enables recording "notice"-severity pool.solar_start /
// pool.solar_stop events to eventsSvc, attributed to deviceID. Without this,
// the solar pump still operates identically — only the notice trail is
// skipped (degraded mode: daemon-down or events-disabled never blocks pump
// control, see CLAUDE.md "daemon-optional per device").
func (sa *SolarAutomation) WithEvents(eventsSvc *events.Service, deviceID string) *SolarAutomation {
	sa.events = eventsSvc
	sa.deviceID = deviceID
	return sa
}

// recordNotice emits a "notice"-severity event for a solar pump decision. A
// nil events service (WithEvents never called) makes this a silent no-op.
func (sa *SolarAutomation) recordNotice(ctx context.Context, name string, data map[string]any) {
	if sa.events == nil || sa.deviceID == "" {
		return
	}
	payload, err := json.Marshal(data)
	if err != nil {
		sa.log.Error(err, "Failed to marshal solar notice data", "event", name)
		return
	}
	str := string(payload)
	e := events.Event{
		Ts:        float64(time.Now().Unix()),
		DeviceID:  sa.deviceID,
		Component: "solar",
		Event:     name,
		Severity:  "notice",
		Data:      &str,
	}
	if err := sa.events.Record(ctx, e); err != nil {
		sa.log.Error(err, "Failed to record solar notice", "event", name)
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
				sa.recordNotice(ctx, "pool.solar_start", map[string]any{
					"solar_w":     sample.SolarW,
					"threshold_w": sa.cfg.StartThresholdW,
					"held_for_s":  time.Since(aboveStart).Seconds(),
				})
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
		// Stay in pumpRunning on failure — like the solar-loss stop below — so the
		// ceiling check fires again on the next sample and retries the stop. Moving
		// to pumpIdle here would silently defeat the "absolute" ceiling guarantee
		// if the (already retried) Switch.Set still couldn't be confirmed.
		if haveRuntime && sa.cfg.MaxRotationSec > 0 && runtime >= sa.cfg.MaxRotationSec {
			sa.log.Info("Hard ceiling reached, stopping pump",
				"runtime_sec", runtime, "max_rotation_sec", sa.cfg.MaxRotationSec)
			if err := sa.pump.SetPump(ctx, false); err != nil {
				sa.log.Error(err, "Failed to stop pump (hard ceiling); will retry next sample")
				break
			}
			sa.recordNotice(ctx, "pool.solar_stop", map[string]any{
				"reason":           "hard_ceiling",
				"runtime_sec":      runtime,
				"max_rotation_sec": sa.cfg.MaxRotationSec,
			})
			return pumpIdle, zero, zero
		}

		// Soft stop: the normal filtration goal stops the pump only once solar
		// has also dropped below the start threshold — while solar still
		// produces, free energy keeps over-filtering past the daily target.
		// Same retry-on-failure rationale as the hard ceiling above.
		if haveRuntime && sa.cfg.DailyTargetSec > 0 && runtime >= sa.cfg.DailyTargetSec &&
			sample.SolarW < sa.cfg.StartThresholdW {
			sa.log.Info("Daily target reached and solar gone, stopping pump (soft stop)",
				"runtime_sec", runtime, "daily_target_sec", sa.cfg.DailyTargetSec, "solar_w", sample.SolarW)
			if err := sa.pump.SetPump(ctx, false); err != nil {
				sa.log.Error(err, "Failed to stop pump (soft stop); will retry next sample")
				break
			}
			sa.recordNotice(ctx, "pool.solar_stop", map[string]any{
				"reason":           "soft_stop",
				"runtime_sec":      runtime,
				"daily_target_sec": sa.cfg.DailyTargetSec,
				"solar_w":          sample.SolarW,
			})
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
				sa.recordNotice(ctx, "pool.solar_stop", map[string]any{
					"reason":      "solar_loss",
					"solar_w":     sample.SolarW,
					"threshold_w": sa.cfg.StopThresholdW,
					"held_for_s":  time.Since(belowStop).Seconds(),
				})
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

// shellyPumpController implements PumpController using the Shelly Switch.Set
// RPC over MQTT. Because Switch.Set is fire-and-forget — the MQTT broker
// neither retains nor redelivers it, so a message dropped by a flaky link is
// gone for good — it confirms the resulting state by tracking the device's own
// NotifyStatus push notifications (subscribed for the daemon's lifetime, see
// newShellyPumpController) rather than polling Switch.GetStatus synchronously.
type shellyPumpController struct {
	log    logr.Logger
	device *shellyapi.Device

	mu     sync.Mutex
	output *bool         // last known switch:0 output reported via MQTT; nil = unknown
	notify chan struct{} // closed and replaced whenever output changes, to wake waiters
}

// newShellyPumpController creates and MQTT-initializes a shellyPumpController
// for the given device ID, and subscribes to its status notifications for the
// lifetime of ctx (the daemon context). Must be called after shelly.Init() and
// the MQTT client have been set up.
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

	mc, err := mqttclient.GetClientE(ctx)
	if err != nil {
		return nil, fmt.Errorf("get MQTT client: %w", err)
	}

	c := &shellyPumpController{
		log:    log.WithName("PumpController"),
		device: sd,
		notify: make(chan struct{}),
	}

	// Shelly Gen2 devices push NotifyStatus on <device_id>/events/rpc whenever
	// a component's status changes (rpc_ntf, enabled by default) — the same
	// mechanism gen2.Listener uses to record switch.on/switch.off events.
	topic := sd.Id() + "/events/rpc"
	if err := mc.SubscribeWithHandler(ctx, topic, 8, "solar.pump.status", func(_ string, payload []byte, _ string) error {
		c.handleStatusNotification(payload)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("subscribe to %s: %w", topic, err)
	}

	return c, nil
}

// handleStatusNotification parses a NotifyStatus message from the pump device
// and records the switch:0 output state, waking any goroutines waiting in
// waitForOutput.
func (c *shellyPumpController) handleStatusNotification(payload []byte) {
	var msg struct {
		Method string                     `json:"method"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil || msg.Method != "NotifyStatus" {
		return
	}
	raw, ok := msg.Params["switch:0"]
	if !ok {
		return
	}
	var sw struct {
		Output *bool `json:"output"`
	}
	if err := json.Unmarshal(raw, &sw); err != nil || sw.Output == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.output != nil && *c.output == *sw.Output {
		return
	}
	c.output = sw.Output
	close(c.notify)
	c.notify = make(chan struct{})
	c.log.V(1).Info("Pump switch status update", "device_id", c.device.Id(), "output", *sw.Output)
}

// waitForOutput blocks until the tracked switch:0 output matches want, ctx is
// cancelled, or timeout elapses — whichever comes first. It returns true only
// on a confirmed match.
func (c *shellyPumpController) waitForOutput(ctx context.Context, want bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		output := c.output
		ch := c.notify
		c.mu.Unlock()

		if output != nil && *output == want {
			return true
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-ch:
			timer.Stop()
		case <-timer.C:
			return false
		}
	}
}

// pumpSetMaxAttempts/pumpSetRetryDelay/pumpVerifyTimeout bound the
// retry-and-confirm loop in SetPump: Switch.Set only acknowledges receipt, not
// the resulting state, so each attempt waits for the device's own status
// notification to confirm the change landed before declaring success.
const (
	pumpSetMaxAttempts = 3
	pumpSetRetryDelay  = 2 * time.Second
	pumpVerifyTimeout  = 5 * time.Second
)

func (c *shellyPumpController) SetPump(ctx context.Context, on bool) error {
	var lastErr error
	for attempt := 1; attempt <= pumpSetMaxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepCtx(ctx, pumpSetRetryDelay); err != nil {
				return err
			}
		}

		if _, err := sswitch.Set(ctx, c.device, types.ChannelMqtt, 0, on); err != nil {
			lastErr = fmt.Errorf("Switch.Set on=%v: %w", on, err)
			c.log.Error(err, "Switch.Set failed, will retry", "device_id", c.device.Id(), "on", on, "attempt", attempt)
			continue
		}

		if c.waitForOutput(ctx, on, pumpVerifyTimeout) {
			c.log.Info("Pump state confirmed via status notification", "device_id", c.device.Id(), "on", on, "attempt", attempt)
			return nil
		}

		lastErr = fmt.Errorf("no status notification confirming pump on=%v", on)
		c.log.Error(lastErr, "Pump did not confirm desired state, will retry", "device_id", c.device.Id(), "on", on, "attempt", attempt)
	}
	return fmt.Errorf("failed to set pump on=%v after %d attempts: %w", on, pumpSetMaxAttempts, lastErr)
}

// sleepCtx waits for d or returns ctx.Err() if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
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
