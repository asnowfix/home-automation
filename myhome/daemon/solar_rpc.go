package daemon

import (
	"context"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/myhome/energy"
	"github.com/go-logr/logr"
)

// solarClaimerLiveReadTimeout bounds the live device KVS reads
// handleClaimersList performs to enrich the "pool-pump" entry. Solar
// aggregation is daemon-side only (see #401); a slow or unreachable device
// must degrade the single claimer's status, not hang or fail the whole RPC.
const solarClaimerLiveReadTimeout = 3 * time.Second

// SolarRPCHandler exposes the daemon's static energy-claimers registry via
// the myhome.SolarClaimersList RPC verb (mirrors PoolRPCHandler). It is
// deliberately not a live-arbitration engine: no priority ordering, no
// partial allocation — see the follow-up "solar router" issue (#401) for
// that. It best-effort-enriches the "pool-pump" claimer with a live
// active/speed read via PoolNotices.ActiveSpeed.
type SolarRPCHandler struct {
	log      logr.Logger
	registry *energy.Registry
	pool     *PoolNotices
}

// NewSolarRPCHandler builds a SolarRPCHandler. pool may be nil (pool
// tracking disabled or the device unreachable at startup) — handleClaimersList
// then reports the pool-pump claimer's identity without live status,
// mirroring how PoolNotices.OnEvent on a nil receiver is already a no-op.
func NewSolarRPCHandler(log logr.Logger, registry *energy.Registry, pool *PoolNotices) *SolarRPCHandler {
	return &SolarRPCHandler{log: log.WithName("SolarRPCHandler"), registry: registry, pool: pool}
}

// RegisterHandlers registers the solar.claimerslist RPC method.
func (h *SolarRPCHandler) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.SolarClaimersList, h.handleClaimersList)
	h.log.Info("Solar RPC handler registered")
}

func (h *SolarRPCHandler) handleClaimersList(ctx context.Context, _ any) (any, error) {
	claimers := h.registry.Snapshot()

	out := make([]myhome.SolarClaimer, 0, len(claimers))
	for _, c := range claimers {
		sc := myhome.SolarClaimer{
			Name:     c.Name,
			DeviceID: c.DeviceID,
		}

		// Only the pool pump has a live-status source today. A future
		// multi-consumer solar router would generalize this per-claimer
		// lookup; out of scope here (see #401's follow-up).
		if c.Name == "pool-pump" {
			active, speed, err := h.readPoolActiveSpeed(ctx)
			if err != nil {
				h.log.Info("Live active/speed read failed, reporting claimer without live status",
					"device_id", c.DeviceID, "error", err.Error())
			} else {
				sc.Active = active
				sc.ActiveSpeed = speed
			}
		}

		out = append(out, sc)
	}

	return &myhome.SolarClaimersListResult{Claimers: out}, nil
}

// readPoolActiveSpeed wraps PoolNotices.ActiveSpeed with a bounded timeout,
// so an unreachable pool device degrades this one claimer's live status
// instead of hanging or failing the whole solar.claimerslist RPC.
func (h *SolarRPCHandler) readPoolActiveSpeed(ctx context.Context) (active bool, speed string, err error) {
	ctx, cancel := context.WithTimeout(ctx, solarClaimerLiveReadTimeout)
	defer cancel()
	return h.pool.ActiveSpeed(ctx)
}
