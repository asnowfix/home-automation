package daemon

import (
	"context"
	"fmt"

	"github.com/asnowfix/home-automation/internal/myhome"
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
}

// NewPoolRPCHandler builds a PoolRPCHandler. pool may be nil (pool tracking
// disabled or the device unreachable at startup) — handleGetStatus then
// returns a clear error instead of panicking.
func NewPoolRPCHandler(log logr.Logger, pool *PoolNotices) *PoolRPCHandler {
	return &PoolRPCHandler{log: log.WithName("PoolRPCHandler"), pool: pool}
}

// RegisterHandlers registers the pool.getstatus RPC method.
func (h *PoolRPCHandler) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.PoolGetStatus, h.handleGetStatus)
	h.log.Info("Pool RPC handler registered")
}

func (h *PoolRPCHandler) handleGetStatus(ctx context.Context, _ any) (any, error) {
	if h.pool == nil {
		return nil, fmt.Errorf("pool status unavailable: pool device not configured or unreachable")
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
