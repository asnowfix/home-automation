package daemon

import (
	"context"
	"fmt"

	"github.com/asnowfix/home-automation/internal/myhome"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/go-logr/logr"
)

// PoolRPCHandler exposes the configured pool device's turnover rate and
// water-supply status via the myhome.PoolGetStatus RPC verb, so both the web
// UI and `ctl pool status` read from the same source. It reuses PoolNotices'
// already-initialized device handle and KVS helpers rather than standing up
// a second connection to the same device.
type PoolRPCHandler struct {
	log  logr.Logger
	pool *PoolNotices
	// enabled is whether pool tracking was requested (--enable-pool with a device
	// id). pool == nil with enabled set means the device was unreachable at startup.
	enabled bool
	// deviceID and waterSupply feed pool.snapshot (WithSnapshotSource).
	deviceID    string
	waterSupply func(ctx context.Context) *bool
}

// WithSnapshotSource tells pool.snapshot which device it describes and how to
// read that device's water-supply input from memory (never from the device).
func (h *PoolRPCHandler) WithSnapshotSource(deviceID string, waterSupply func(ctx context.Context) *bool) *PoolRPCHandler {
	h.deviceID, h.waterSupply = deviceID, waterSupply
	return h
}

// NewPoolRPCHandler builds a PoolRPCHandler. pool may be nil (pool tracking
// disabled or the device unreachable at startup) — handleGetStatus then
// returns a clear error instead of panicking.
func NewPoolRPCHandler(log logr.Logger, pool *PoolNotices, enabled bool) *PoolRPCHandler {
	return &PoolRPCHandler{log: log.WithName("PoolRPCHandler"), pool: pool, enabled: enabled}
}

// RegisterHandlers registers the pool.getstatus RPC method.
func (h *PoolRPCHandler) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.PoolGetStatus, h.handleGetStatus)
	myhome.RegisterMethodHandler(myhome.PoolSnapshot, h.handleSnapshot)
	h.log.Info("Pool RPC handler registered")
}

func (h *PoolRPCHandler) handleGetStatus(ctx context.Context, _ any) (any, error) {
	if h.pool == nil {
		if !h.enabled {
			return nil, fmt.Errorf("pool status unavailable (enable pool tracking with a pool device id): %w", myhome.ErrPoolNotEnabled)
		}
		return nil, fmt.Errorf("pool status unavailable: pool device was unreachable at startup")
	}

	achieved, target, runtimeSec, err := h.pool.ComputeTurnover(ctx)
	if err != nil {
		return nil, fmt.Errorf("compute turnover: %w", err)
	}
	waterSupplyActive, err := h.pool.WaterSupplyActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("read water supply status: %w", err)
	}

	return &myhome.PoolGetStatusResult{
		DeviceID:          h.pool.deviceID,
		WaterSupplyActive: waterSupplyActive,
		TurnoverAchieved:  achieved,
		TurnoverTarget:    target,
		RuntimeSec:        runtimeSec,
	}, nil
}

// handleSnapshot answers pool.snapshot from memory only: the cached water-supply
// input and the last stored turnover reading. It makes no device call, so the UI
// can call it on every render (#731). Unknown values stay nil.
func (h *PoolRPCHandler) handleSnapshot(ctx context.Context, _ any) (any, error) {
	if !h.enabled || h.deviceID == "" {
		return nil, fmt.Errorf("pool status unavailable (enable pool tracking with a pool device id): %w", myhome.ErrPoolNotEnabled)
	}
	res := &myhome.PoolSnapshotResult{DeviceID: h.deviceID}
	if h.waterSupply != nil {
		res.WaterSupplyActive = h.waterSupply(ctx)
	}
	if s, ok := h.pool.Snapshot(); ok {
		res.TurnoverAchieved, res.TurnoverTarget, res.RuntimeSec = &s.achieved, &s.target, &s.runtimeSec
		res.TurnoverTs = s.ts.Unix()
		h.log.V(1).Info("pool.snapshot served", "turnover_ts", res.TurnoverTs)
	}
	return res, nil
}

// cachedWaterSupply returns a reader of the pool device's input:0 state taken
// from the status the daemon already holds in the device registry (kept current
// by refreshes and status notifications). nil means unknown. It only reads.
func cachedWaterSupply(reg interface {
	GetDeviceById(ctx context.Context, id string) (*myhome.Device, error)
}, deviceID string) func(ctx context.Context) *bool {
	return func(ctx context.Context) *bool {
		d, err := reg.GetDeviceById(ctx, deviceID)
		if err != nil || d == nil {
			return nil
		}
		sd, ok := d.Impl().(*shellyapi.Device)
		if !ok || sd == nil {
			return nil
		}
		st := sd.Status()
		if st == nil || st.Input0 == nil {
			return nil
		}
		active := st.Input0.State
		return &active
	}
}
