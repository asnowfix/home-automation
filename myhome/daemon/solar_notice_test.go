package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/myhome/events"
	beem "github.com/asnowfix/home-automation/pkg/beem"
	"github.com/go-logr/logr"
)

// newTestEventsService wraps newTestEventsStorage (pool_runtime_tracker_test.go)
// in an events.Service, the type SolarAutomation.WithEvents expects.
func newTestEventsService(t *testing.T) (*events.Service, *events.Storage) {
	t.Helper()
	store := newTestEventsStorage(t)
	return events.NewService(logr.Discard(), store, nil, nil, 0), store
}

func queryNoticeEvents(t *testing.T, store *events.Storage, eventName string) []events.Event {
	t.Helper()
	rows, err := store.Query(context.Background(), events.Query{EventType: eventName})
	if err != nil {
		t.Fatalf("Query(%q): %v", eventName, err)
	}
	return rows
}

// TestSolarAutomation_RecordsStartNotice verifies a "notice"-severity
// pool.solar_start event is recorded when the solar trigger starts the pump.
func TestSolarAutomation_RecordsStartNotice(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	evSvc, store := newTestEventsService(t)

	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, defaultCfg())
	sa.WithEvents(evSvc, "pool-device")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600) // above start threshold → pump on

	rows := queryNoticeEvents(t, store, "pool.solar_start")
	if len(rows) != 1 {
		t.Fatalf("pool.solar_start rows = %d, want 1", len(rows))
	}
	if rows[0].DeviceID != "pool-device" {
		t.Errorf("DeviceID = %q, want pool-device", rows[0].DeviceID)
	}
	if rows[0].Component != "solar" {
		t.Errorf("Component = %q, want solar", rows[0].Component)
	}
	if rows[0].Severity != "notice" {
		t.Errorf("Severity = %q, want notice", rows[0].Severity)
	}
	if rows[0].Data == nil || *rows[0].Data == "" {
		t.Error("Data is empty, want solar_w/threshold_w payload")
	}
}

// TestSolarAutomation_RecordsSolarLossStopNotice verifies a "notice"
// pool.solar_stop{reason:solar_loss} event when solar drops below the stop
// threshold while running.
func TestSolarAutomation_RecordsSolarLossStopNotice(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	evSvc, store := newTestEventsService(t)

	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, defaultCfg())
	sa.WithEvents(evSvc, "pool-device")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600) // start
	send(ch, 100) // below stop threshold → stop (solar loss)

	rows := queryNoticeEvents(t, store, "pool.solar_stop")
	if len(rows) != 1 {
		t.Fatalf("pool.solar_stop rows = %d, want 1", len(rows))
	}
	if rows[0].Data == nil {
		t.Fatal("Data is nil, want a reason payload")
	}
	if want := `"reason":"solar_loss"`; !strings.Contains(*rows[0].Data, want) {
		t.Errorf("Data = %q, want substring %q", *rows[0].Data, want)
	}
}

// TestSolarAutomation_RecordsHardCeilingStopNotice verifies the hard-ceiling
// stop branch records reason "hard_ceiling".
func TestSolarAutomation_RecordsHardCeilingStopNotice(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	evSvc, store := newTestEventsService(t)

	tracker := &mockRuntimeTracker{runtimeSec: 0} // not yet at ceiling, so the pump can start
	cfg := defaultCfg()
	cfg.MaxRotationSec = 3600

	sa := NewSolarAutomation(logr.Discard(), ch, tracker, pump, cfg)
	sa.WithEvents(evSvc, "pool-device")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 800) // start pump
	tracker.runtimeSec = 3600
	send(ch, 800) // still high solar, but ceiling hit → must stop

	rows := queryNoticeEvents(t, store, "pool.solar_stop")
	if len(rows) != 1 {
		t.Fatalf("pool.solar_stop rows = %d, want 1; calls=%v", len(rows), pump.calls)
	}
	if want := `"reason":"hard_ceiling"`; !strings.Contains(*rows[0].Data, want) {
		t.Errorf("Data = %q, want substring %q", *rows[0].Data, want)
	}
}

// TestSolarAutomation_RecordsSoftStopNotice verifies the soft-stop branch
// records reason "soft_stop".
func TestSolarAutomation_RecordsSoftStopNotice(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	evSvc, store := newTestEventsService(t)

	tracker := &mockRuntimeTracker{runtimeSec: 4000} // already past the soft target
	cfg := defaultCfg()
	cfg.DailyTargetSec = 3600

	sa := NewSolarAutomation(logr.Discard(), ch, tracker, pump, cfg)
	sa.WithEvents(evSvc, "pool-device")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600) // start
	send(ch, 100) // solar gone + target already reached → soft stop

	rows := queryNoticeEvents(t, store, "pool.solar_stop")
	if len(rows) != 1 {
		t.Fatalf("pool.solar_stop rows = %d, want 1", len(rows))
	}
	if want := `"reason":"soft_stop"`; !strings.Contains(*rows[0].Data, want) {
		t.Errorf("Data = %q, want substring %q", *rows[0].Data, want)
	}
}

// TestSolarAutomation_NoNoticesWithoutWithEvents confirms recordNotice is a
// silent no-op (and never panics) when WithEvents was never called — the
// many pre-existing tests in solar_automation_test.go rely on this.
func TestSolarAutomation_NoNoticesWithoutWithEvents(t *testing.T) {
	pump := &mockPumpController{}
	ch := make(chan beem.PowerSample, 8)
	sa := NewSolarAutomation(logr.Discard(), ch, nil, pump, defaultCfg())
	// Deliberately not calling sa.WithEvents(...).

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa.Start(ctx)

	send(ch, 600)
	send(ch, 100)

	on, ok := pump.lastCall()
	if !ok || on {
		t.Fatalf("expected pump OFF; calls=%v", pump.calls)
	}
}
