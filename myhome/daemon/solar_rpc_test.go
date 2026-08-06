package daemon

import (
	"context"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/myhome/energy"
	"github.com/go-logr/logr"
)

// TestSolarRPCHandler_ClaimersList_EnrichesPoolPump verifies the
// solar.claimerslist handler walks the registry and enriches the
// "pool-pump" entry with a live active/speed read from a (fake) device KVS,
// mapping the active switch id back to its configured speed name — using
// the same fakeKVSDevice stub as pool_notices_test.go's ComputeTurnover
// tests, so no live hardware or MQTT broker is touched.
func TestSolarRPCHandler_ClaimersList_EnrichesPoolPump(t *testing.T) {
	dev := &fakeKVSDevice{
		id: "pool-device-claimers",
		kvs: map[string]string{
			"script/pool-pump/active-output": "1",
			"script/pool-pump/eco-speed":     "0",
			"script/pool-pump/mid-speed":     "1",
			"script/pool-pump/high-speed":    "2",
		},
	}
	pool := &PoolNotices{log: logr.Discard(), device: dev, deviceID: dev.id}

	registry := energy.NewRegistry()
	registry.Register("pool-pump", dev.id)

	h := NewSolarRPCHandler(logr.Discard(), registry, pool)

	out, err := h.handleClaimersList(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleClaimersList: %v", err)
	}
	result, ok := out.(*myhome.SolarClaimersListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", out)
	}
	if len(result.Claimers) != 1 {
		t.Fatalf("expected 1 claimer, got %d", len(result.Claimers))
	}
	c := result.Claimers[0]
	if c.Name != "pool-pump" || c.DeviceID != dev.id {
		t.Fatalf("unexpected claimer identity: %+v", c)
	}
	if !c.Active {
		t.Fatalf("expected pool-pump to be reported active, got %+v", c)
	}
	if c.ActiveSpeed != "mid" {
		t.Fatalf("expected active_speed=mid (switch 1), got %q", c.ActiveSpeed)
	}
}

// TestSolarRPCHandler_ClaimersList_PoolInactive verifies an active-output of
// -1 (pump off) is reported as an inactive claimer with no speed, not an
// error.
func TestSolarRPCHandler_ClaimersList_PoolInactive(t *testing.T) {
	dev := &fakeKVSDevice{
		id: "pool-device-inactive",
		kvs: map[string]string{
			"script/pool-pump/active-output": "-1",
		},
	}
	pool := &PoolNotices{log: logr.Discard(), device: dev, deviceID: dev.id}

	registry := energy.NewRegistry()
	registry.Register("pool-pump", dev.id)

	h := NewSolarRPCHandler(logr.Discard(), registry, pool)

	out, err := h.handleClaimersList(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleClaimersList: %v", err)
	}
	result := out.(*myhome.SolarClaimersListResult)
	if len(result.Claimers) != 1 {
		t.Fatalf("expected 1 claimer, got %d", len(result.Claimers))
	}
	c := result.Claimers[0]
	if c.Active {
		t.Fatalf("expected pool-pump to be reported inactive, got %+v", c)
	}
	if c.ActiveSpeed != "" {
		t.Fatalf("expected empty active_speed when inactive, got %q", c.ActiveSpeed)
	}
}

// TestSolarRPCHandler_ClaimersList_NilPoolNotices verifies the RPC degrades
// gracefully — returning the claimer's static identity with Active=false —
// when PoolNotices is nil (pool tracking disabled or device unreachable at
// startup), rather than panicking or failing the whole request. Mirrors the
// nil-receiver safety already required of PoolNotices.OnEvent.
func TestSolarRPCHandler_ClaimersList_NilPoolNotices(t *testing.T) {
	registry := energy.NewRegistry()
	registry.Register("pool-pump", "some-device-id")

	h := NewSolarRPCHandler(logr.Discard(), registry, nil)

	out, err := h.handleClaimersList(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleClaimersList: %v", err)
	}
	result := out.(*myhome.SolarClaimersListResult)
	if len(result.Claimers) != 1 {
		t.Fatalf("expected 1 claimer, got %d", len(result.Claimers))
	}
	c := result.Claimers[0]
	if c.Name != "pool-pump" || c.DeviceID != "some-device-id" {
		t.Fatalf("unexpected claimer identity: %+v", c)
	}
	if c.Active {
		t.Fatalf("expected inactive when PoolNotices is nil, got %+v", c)
	}
}

// TestSolarRPCHandler_ClaimersList_LiveReadErrorDoesNotFailRPC verifies that
// a KVS read failure for the pool-pump claimer (e.g. device unreachable) is
// logged and the claimer reported with Active=false, rather than the whole
// solar.claimerslist RPC returning an error.
func TestSolarRPCHandler_ClaimersList_LiveReadErrorDoesNotFailRPC(t *testing.T) {
	dev := &fakeKVSDevice{
		id:  "pool-device-unreachable",
		kvs: map[string]string{ /* active-output deliberately absent */ },
	}
	pool := &PoolNotices{log: logr.Discard(), device: dev, deviceID: dev.id}

	registry := energy.NewRegistry()
	registry.Register("pool-pump", dev.id)

	h := NewSolarRPCHandler(logr.Discard(), registry, pool)

	out, err := h.handleClaimersList(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleClaimersList should not fail on a live-read error, got: %v", err)
	}
	result := out.(*myhome.SolarClaimersListResult)
	if len(result.Claimers) != 1 {
		t.Fatalf("expected 1 claimer, got %d", len(result.Claimers))
	}
	if result.Claimers[0].Active {
		t.Fatalf("expected inactive claimer on live-read error, got %+v", result.Claimers[0])
	}
}

// TestSolarRPCHandler_ClaimersList_EmptyRegistry verifies an empty registry
// (no pool device configured) yields an empty, non-nil claimers list rather
// than an error.
func TestSolarRPCHandler_ClaimersList_EmptyRegistry(t *testing.T) {
	registry := energy.NewRegistry()
	h := NewSolarRPCHandler(logr.Discard(), registry, nil)

	out, err := h.handleClaimersList(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleClaimersList: %v", err)
	}
	result := out.(*myhome.SolarClaimersListResult)
	if result.Claimers == nil {
		t.Fatalf("expected non-nil empty claimers slice")
	}
	if len(result.Claimers) != 0 {
		t.Fatalf("expected 0 claimers, got %d", len(result.Claimers))
	}
}
