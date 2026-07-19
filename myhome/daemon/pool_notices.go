package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// kvsKeyTurnover is the configured daily turnover target (pool volumes per
// day), read alongside the runtime-target KVS keys already defined in
// solar_automation.go (kvsKeyPoolVolume, kvsKeyMaxFlowRate, kvsKeyMaxRpm,
// kvsKeySpeed) — pool-pump.js populates all of these for its own scheduling.
const kvsKeyTurnover = "script/pool-pump/turnover"

// PoolNotices records a companion "pool.turnover_today" notice whenever the
// pool pump stops — either via the device's own pool.pump_stop (schedule or
// manual) or the daemon's pool.solar_stop — reporting the water-volume
// turnovers achieved today against the configured daily target. Neither
// pool-pump.js nor SolarAutomation track this number: it combines runtime
// accrued so far (PoolRuntimeTracker, events DB) with KVS configuration that
// only the daemon reads.
type PoolNotices struct {
	log      logr.Logger
	events   *events.Service
	tracker  *PoolRuntimeTracker
	device   *shellyapi.Device
	deviceID string
}

// NewPoolNotices builds a PoolNotices, or returns nil if any dependency is
// unavailable (events/tracker disabled, or the pool device can't be reached
// over MQTT right now). OnEvent on a nil *PoolNotices is a safe no-op, so
// daemon.go can wire it into the broadcast hook unconditionally.
func NewPoolNotices(ctx context.Context, log logr.Logger, eventsSvc *events.Service, tracker *PoolRuntimeTracker, deviceID string) *PoolNotices {
	if eventsSvc == nil || tracker == nil || deviceID == "" {
		return nil
	}

	log = log.WithName("PoolNotices")

	sd, err := shellyapi.NewDeviceFromMqttId(ctx, log, deviceID)
	if err != nil {
		log.Error(err, "Failed to create device handle, turnover notices disabled", "device_id", deviceID)
		return nil
	}
	if err := sd.Init(ctx); err != nil {
		log.Error(err, "Failed to init device handle, turnover notices disabled", "device_id", deviceID)
		return nil
	}

	return &PoolNotices{
		log:      log,
		events:   eventsSvc,
		tracker:  tracker,
		device:   sd,
		deviceID: deviceID,
	}
}

// OnEvent is wired into the daemon's event broadcast hook (see daemon.go
// broadcastFn) alongside notice.Service.OnEvent. It reacts only to
// pool.pump_stop (device-emitted, schedule/manual) and pool.solar_stop
// (daemon-emitted) — every other event is a no-op.
func (p *PoolNotices) OnEvent(ctx context.Context, e events.Event) {
	if p == nil {
		return
	}
	if e.Event != "pool.pump_stop" && e.Event != "pool.solar_stop" {
		return
	}
	p.recordTurnoverToday(ctx)
}

func (p *PoolNotices) recordTurnoverToday(ctx context.Context) {
	achieved, target, runtimeSec, err := p.ComputeTurnover(ctx)
	if err != nil {
		p.log.Error(err, "Failed to compute turnover for notice")
		return
	}

	payload, err := json.Marshal(map[string]any{
		"turnover_achieved": achieved,
		"turnover_target":   target,
		"runtime_sec":       runtimeSec,
	})
	if err != nil {
		p.log.Error(err, "Failed to marshal turnover notice data")
		return
	}
	str := string(payload)
	ev := events.Event{
		Ts:        float64(time.Now().Unix()),
		DeviceID:  p.deviceID,
		Component: "pool",
		Event:     "pool.turnover_today",
		Severity:  "notice",
		Data:      &str,
	}
	if err := p.events.Record(ctx, ev); err != nil {
		p.log.Error(err, "Failed to record turnover_today notice")
	}
}

// ComputeTurnover returns today's achieved water-volume turnovers (pool
// volumes filtered so far today) against the configured daily target, plus
// the runtime in seconds they were derived from. Shared by the
// pool.turnover_today notice (recordTurnoverToday) and the pool.getstatus
// RPC handler (PoolRPCHandler) so both read the same KVS values the same way.
func (p *PoolNotices) ComputeTurnover(ctx context.Context) (achieved, target float64, runtimeSec int64, err error) {
	runtimeSec, err = p.tracker.DailyRuntimeSec(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read daily pump runtime: %w", err)
	}

	via := types.ChannelMqtt
	poolVolume, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyPoolVolume)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read pool volume: %w", err)
	}
	maxFlowRate, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyMaxFlowRate)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read max flow rate: %w", err)
	}
	maxRpm, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyMaxRpm)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read max rpm: %w", err)
	}
	speed, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeySpeed)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read active speed: %w", err)
	}
	target, err = readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyTurnover)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read turnover target: %w", err)
	}
	if poolVolume <= 0 || maxFlowRate <= 0 || maxRpm <= 0 || speed <= 0 {
		return 0, 0, 0, fmt.Errorf(
			"invalid pool KVS values: pool_volume=%v max_flow_rate=%v max_rpm=%v speed=%v",
			poolVolume, maxFlowRate, maxRpm, speed,
		)
	}

	flowRate := maxFlowRate * (speed / maxRpm) // m3/h
	achieved = roundTo(float64(runtimeSec)/3600*flowRate/poolVolume, 2)
	return achieved, target, runtimeSec, nil
}

// WaterSupplyActive reports whether the pool device's water-supply
// protection input is currently engaged (true = active, pump forced off by
// pool-pump.js's handleWaterSupply; false = normal operation).
func (p *PoolNotices) WaterSupplyActive(ctx context.Context) (bool, error) {
	result, err := p.device.CallE(ctx, types.ChannelMqtt, "Input.GetStatus", map[string]any{"id": 0})
	if err != nil {
		return false, fmt.Errorf("Input.GetStatus: %w", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		return false, fmt.Errorf("unexpected Input.GetStatus response type %T", result)
	}
	active, _ := m["state"].(bool)
	return active, nil
}

// roundTo rounds v to the given number of decimal places.
func roundTo(v float64, places int) float64 {
	pow := 1.0
	for i := 0; i < places; i++ {
		pow *= 10
	}
	if v >= 0 {
		return float64(int64(v*pow+0.5)) / pow
	}
	return float64(int64(v*pow-0.5)) / pow
}
