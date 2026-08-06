package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	poolscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/myhome/events"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// kvsKeyTurnover is the configured daily turnover target (pool volumes per
// day). kvsKeyRuntimeToday and kvsKeyTurnoverToday are the on-device
// runtime/turnover accumulators pool-pump.js maintains and mirrors to KVS
// (see #402) — the Go side just reads them back instead of re-deriving flow
// rate from the (non-numeric) preferred-speed KVS key. kvsKeyActiveOutput is
// the switch id (or -1 when off) the pump is currently driving, read back by
// ActiveSpeed for the solar.claimerslist RPC (see #404/solar_rpc.go).
const (
	kvsKeyTurnover      = "script/pool-pump/turnover"
	kvsKeyRuntimeToday  = "script/pool-pump/runtime-sec"
	kvsKeyTurnoverToday = "script/pool-pump/turnover-today"
	kvsKeyActiveOutput  = "script/pool-pump/active-output"
)

// defaultSpeedSwitchIDs are the eco/mid/high switch indices used when a
// controller has no speed mapping configured in KVS yet. Mirrors
// internal/myhome/shelly/script.defaultSpeedMappings (unexported there), so
// ActiveSpeed's best-effort read degrades to the same fallback.
var defaultSpeedSwitchIDs = map[string]int64{"eco": 0, "mid": 1, "high": 2}

// speedNameOrder is the display order ActiveSpeed checks switch-id matches
// in — only matters when a misconfiguration maps two speeds to the same
// switch id, in which case the first match wins.
var speedNameOrder = []string{"eco", "mid", "high"}

// PoolNotices records a companion "pool.turnover_today" notice whenever the
// pool pump stops — either via the device's own pool.pump_stop (schedule or
// manual) or the daemon's pool.solar_stop — reporting the water-volume
// turnovers achieved today against the configured daily target. Since #402,
// pool-pump.js itself computes and persists today's cumulative runtime and
// achieved turnover to KVS (it owns the string->RPM speed mapping); this
// type just reads those pre-computed values back rather than re-deriving
// flow rate here.
type PoolNotices struct {
	log      logr.Logger
	events   *events.Service
	device   types.Device // types.Device (not concrete *shellyapi.Device) so tests can inject a fake KVS responder
	deviceID string
}

// NewPoolNotices builds a PoolNotices, or returns nil if any dependency is
// unavailable (events service disabled, or the pool device can't be reached
// over MQTT right now). OnEvent on a nil *PoolNotices is a safe no-op, so
// daemon.go can wire it into the broadcast hook unconditionally.
func NewPoolNotices(ctx context.Context, log logr.Logger, eventsSvc *events.Service, deviceID string) *PoolNotices {
	if eventsSvc == nil || deviceID == "" {
		return nil
	}

	log = log.WithName("PoolNotices")

	d, err := shellyapi.NewDeviceFromMqttId(ctx, log, deviceID)
	if err != nil {
		log.Error(err, "Failed to create device handle, turnover notices disabled", "device_id", deviceID)
		return nil
	}
	sd, ok := d.(*shellyapi.Device)
	if !ok {
		log.Error(fmt.Errorf("unexpected device type %T", d), "Turnover notices disabled", "device_id", deviceID)
		return nil
	}
	if err := sd.Init(ctx); err != nil {
		log.Error(err, "Failed to init device handle, turnover notices disabled", "device_id", deviceID)
		return nil
	}

	return &PoolNotices{
		log:      log,
		events:   eventsSvc,
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
// RPC handler (PoolRPCHandler) so both read the same KVS values the same
// way. As of #402, both achieved turnover and runtime are computed on-device
// by pool-pump.js (which owns the preferred-speed -> RPM mapping) and
// mirrored to KVS — this just reads them back.
func (p *PoolNotices) ComputeTurnover(ctx context.Context) (achieved, target float64, runtimeSec int64, err error) {
	via := types.ChannelMqtt

	runtimeSec, err = readPoolKVSInt(ctx, p.log, p.device, via, kvsKeyRuntimeToday)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read runtime today: %w", err)
	}
	achieved, err = readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyTurnoverToday)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read turnover today: %w", err)
	}
	target, err = readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyTurnover)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read turnover target: %w", err)
	}

	return roundTo(achieved, 2), target, runtimeSec, nil
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

// ActiveSpeed reports whether the pool pump is currently running and, if so,
// which named speed (eco/mid/high) its active switch output maps to. It is a
// best-effort live read used to enrich the "pool-pump" entry in the
// solar.claimerslist RPC (see solar_rpc.go) — callers should apply their own
// timeout via ctx and treat a returned error as "unknown" rather than a
// reason to fail the whole request. ActiveSpeed on a nil *PoolNotices is a
// safe no-op (mirrors OnEvent), reporting inactive with no error.
func (p *PoolNotices) ActiveSpeed(ctx context.Context) (active bool, speedName string, err error) {
	if p == nil {
		return false, "", nil
	}

	via := types.ChannelMqtt

	activeOutput, err := readPoolKVSInt(ctx, p.log, p.device, via, kvsKeyActiveOutput)
	if err != nil {
		return false, "", fmt.Errorf("read active-output: %w", err)
	}
	if activeOutput < 0 {
		return false, "", nil
	}

	for _, name := range speedNameOrder {
		key := poolscript.PoolKVSKeys[name+"_speed"]
		switchID, err := readPoolKVSInt(ctx, p.log, p.device, via, key)
		if err != nil {
			// Best-effort: fall back to the same default mapping
			// internal/myhome/shelly/script uses when a controller has no
			// speed mapping configured in KVS yet, rather than failing the
			// whole lookup over one missing/unreachable key.
			switchID = defaultSpeedSwitchIDs[name]
		}
		if switchID == activeOutput {
			return true, name, nil
		}
	}

	return true, fmt.Sprintf("switch-%d", activeOutput), nil
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
