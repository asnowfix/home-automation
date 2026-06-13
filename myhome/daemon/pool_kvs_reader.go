package daemon

import (
	"context"
	"fmt"
	"strconv"

	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

const poolKVSPrefix = "script/pool-pump/"

// poolKVSParams holds the device parameters read from KVS needed to compute runtime targets.
type poolKVSParams struct {
	PoolVolume  float64 // m³
	MaxFlowRate float64 // m³/h at max RPM
	MaxRPM      float64 // rated max RPM
	SpeedRPM    float64 // current operating speed RPM
}

// readPoolKVS fetches pool pump operating parameters from the device KVS.
// Falls back to hardcoded defaults for any key that is missing or cannot be parsed,
// so the function always returns a usable result as long as the device is reachable.
func readPoolKVS(ctx context.Context, log logr.Logger, deviceID string) (*poolKVSParams, error) {
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

	getFloat := func(key string, def float64) float64 {
		resp, err := kvs.GetValue(ctx, log, types.ChannelMqtt, sd, poolKVSPrefix+key)
		if err != nil || resp == nil {
			return def
		}
		v, err := strconv.ParseFloat(resp.Value, 64)
		if err != nil || v <= 0 {
			return def
		}
		return v
	}

	getString := func(key, def string) string {
		resp, err := kvs.GetValue(ctx, log, types.ChannelMqtt, sd, poolKVSPrefix+key)
		if err != nil || resp == nil || resp.Value == "" {
			return def
		}
		return resp.Value
	}

	p := &poolKVSParams{
		PoolVolume:  getFloat("pool-volume", 46),
		MaxFlowRate: getFloat("max-flow-rate", 31),
		MaxRPM:      getFloat("max-rpm", 2900),
	}

	// speed is stored as a string ("eco", "mid", "high", "max"); map to the corresponding RPM key.
	speedRPMKey := map[string]string{
		"eco": "eco-rpm", "mid": "mid-rpm", "high": "high-rpm", "max": "high-rpm",
	}
	speedRPMDefault := map[string]float64{
		"eco-rpm": 2000, "mid-rpm": 2600, "high-rpm": 2900,
	}
	speed := getString("speed", "eco")
	rpmKey := speedRPMKey[speed]
	if rpmKey == "" {
		rpmKey = "eco-rpm"
	}
	p.SpeedRPM = getFloat(rpmKey, speedRPMDefault[rpmKey])

	log.Info("Pool KVS params read",
		"pool_volume_m3", p.PoolVolume,
		"max_flow_rate_m3h", p.MaxFlowRate,
		"max_rpm", p.MaxRPM,
		"speed", speed,
		"speed_rpm", p.SpeedRPM,
	)
	return p, nil
}

// runtimeSecs converts a pool volume turnover multiplier to pump runtime in seconds.
//
//	flow_rate = max_flow_rate × (speed_rpm / max_rpm)          [m³/h]
//	runtime   = pool_volume × turnover / flow_rate × 3600      [s]
func runtimeSecs(p *poolKVSParams, turnoverMultiplier float64) int64 {
	if p.MaxRPM <= 0 || p.MaxFlowRate <= 0 || turnoverMultiplier <= 0 {
		return 0
	}
	flowRate := p.MaxFlowRate * (p.SpeedRPM / p.MaxRPM)
	if flowRate <= 0 {
		return 0
	}
	return int64(p.PoolVolume * turnoverMultiplier / flowRate * 3600)
}
