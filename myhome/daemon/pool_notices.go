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
	runtimeSec, err := p.tracker.DailyRuntimeSec(ctx)
	if err != nil {
		p.log.Error(err, "Failed to read daily pump runtime for turnover notice")
		return
	}

	via := types.ChannelMqtt
	poolVolume, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyPoolVolume)
	if err != nil {
		p.log.Error(err, "Failed to read pool volume for turnover notice")
		return
	}
	maxFlowRate, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyMaxFlowRate)
	if err != nil {
		p.log.Error(err, "Failed to read max flow rate for turnover notice")
		return
	}
	maxRpm, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyMaxRpm)
	if err != nil {
		p.log.Error(err, "Failed to read max rpm for turnover notice")
		return
	}
	speed, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeySpeed)
	if err != nil {
		p.log.Error(err, "Failed to read active speed for turnover notice")
		return
	}
	target, err := readPoolKVSFloat(ctx, p.log, p.device, via, kvsKeyTurnover)
	if err != nil {
		p.log.Error(err, "Failed to read turnover target for turnover notice")
		return
	}
	if poolVolume <= 0 || maxFlowRate <= 0 || maxRpm <= 0 || speed <= 0 {
		p.log.Info("Skipping turnover notice: invalid pool KVS values",
			"pool_volume", poolVolume, "max_flow_rate", maxFlowRate, "max_rpm", maxRpm, "speed", speed)
		return
	}

	flowRate := maxFlowRate * (speed / maxRpm) // m3/h
	achieved := roundTo(float64(runtimeSec)/3600*flowRate/poolVolume, 2)

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
