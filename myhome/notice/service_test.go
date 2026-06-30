package notice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/myhome/notify"
	"github.com/go-logr/logr"
)

type fakeOccupancy struct {
	occupied bool
}

func (f *fakeOccupancy) IsOccupied(_ context.Context) bool {
	return f.occupied
}

type captureMailer struct {
	calls   int
	subject string
	body    string
}

func (m *captureMailer) Send(_ context.Context, subject, body string) error {
	m.calls++
	m.subject = subject
	m.body = body
	return nil
}

func newTestEventsService(t *testing.T) *events.Service {
	t.Helper()
	store, err := events.NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("events.NewStorage: %v", err)
	}
	t.Cleanup(store.Close)
	return events.NewService(logr.Discard(), store, nil, nil, 0)
}

func newTestService(t *testing.T, occupied bool, cfg Config) (*Service, *events.Service) {
	t.Helper()
	evSvc := newTestEventsService(t)
	occ := &fakeOccupancy{occupied: occupied}
	mailer := notify.New(logr.Discard(), notify.Config{}) // no-op: From is empty
	return NewService(logr.Discard(), evSvc, occ, mailer, cfg), evSvc
}

func queryEvents(t *testing.T, evSvc *events.Service, eventName string) []events.Event {
	t.Helper()
	rows, err := evSvc.Store().Query(context.Background(), events.Query{EventType: eventName})
	if err != nil {
		t.Fatalf("Query(%q): %v", eventName, err)
	}
	return rows
}

func TestOnEvent_MotionWhileAbsent(t *testing.T) {
	svc, evSvc := newTestService(t, false /* occupied */, Config{NightStart: "22:00", NightEnd: "06:00"})

	noon := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC) // daytime: only "absent" should fire
	src := events.Event{Ts: float64(noon.Unix()), DeviceID: "shellyblumotion1-aabbcc", Event: "motion.detected", Component: "motion:0", Severity: "info"}
	svc.OnEvent(context.Background(), src)

	absent := queryEvents(t, evSvc, "motion.absent")
	if len(absent) != 1 {
		t.Fatalf("motion.absent rows = %d, want 1", len(absent))
	}
	if absent[0].DeviceID != src.DeviceID {
		t.Errorf("motion.absent device_id = %q, want %q", absent[0].DeviceID, src.DeviceID)
	}
	if absent[0].Severity != "notice" {
		t.Errorf("motion.absent severity = %q, want notice", absent[0].Severity)
	}

	if got := queryEvents(t, evSvc, "motion.night"); len(got) != 0 {
		t.Errorf("motion.night rows = %d, want 0 (daytime)", len(got))
	}
}

func TestOnEvent_MotionAtNightWhilePresent(t *testing.T) {
	svc, evSvc := newTestService(t, true /* occupied */, Config{NightStart: "22:00", NightEnd: "06:00"})

	lateNight := time.Date(2026, 6, 30, 23, 30, 0, 0, time.UTC)
	src := events.Event{Ts: float64(lateNight.Unix()), DeviceID: "shellyblumotion1-aabbcc", Event: "motion.detected", Component: "motion:0"}
	svc.OnEvent(context.Background(), src)

	if got := queryEvents(t, evSvc, "motion.night"); len(got) != 1 {
		t.Fatalf("motion.night rows = %d, want 1", len(got))
	}
	if got := queryEvents(t, evSvc, "motion.absent"); len(got) != 0 {
		t.Errorf("motion.absent rows = %d, want 0 (occupied)", len(got))
	}
}

func TestOnEvent_MotionAtNightWhileAbsent_EmitsBoth(t *testing.T) {
	svc, evSvc := newTestService(t, false, Config{NightStart: "22:00", NightEnd: "06:00"})

	lateNight := time.Date(2026, 6, 30, 23, 30, 0, 0, time.UTC)
	src := events.Event{Ts: float64(lateNight.Unix()), DeviceID: "dev1", Event: "motion.detected", Component: "motion:0"}
	svc.OnEvent(context.Background(), src)

	if got := queryEvents(t, evSvc, "motion.night"); len(got) != 1 {
		t.Errorf("motion.night rows = %d, want 1", len(got))
	}
	if got := queryEvents(t, evSvc, "motion.absent"); len(got) != 1 {
		t.Errorf("motion.absent rows = %d, want 1", len(got))
	}
}

func TestOnEvent_IgnoresNonMotionEvents(t *testing.T) {
	svc, evSvc := newTestService(t, false, Config{})
	src := events.Event{Ts: float64(time.Now().Unix()), DeviceID: "dev1", Event: "switch.on", Component: "switch:0"}
	svc.OnEvent(context.Background(), src)

	if got := queryEvents(t, evSvc, "motion.absent"); len(got) != 0 {
		t.Errorf("motion.absent rows = %d, want 0 for a non-motion event", len(got))
	}
}

// TestOnEvent_DerivedEventsDoNotRecurse confirms the rule cannot re-trigger
// itself on the events it just recorded: motion.absent/motion.night are not
// "motion.detected", so feeding one back through OnEvent must be a no-op.
func TestOnEvent_DerivedEventsDoNotRecurse(t *testing.T) {
	svc, evSvc := newTestService(t, false, Config{NightStart: "22:00", NightEnd: "06:00"})
	derived := events.Event{Ts: float64(time.Now().Unix()), DeviceID: "dev1", Event: "motion.absent", Component: "motion", Severity: "notice"}
	svc.OnEvent(context.Background(), derived)

	rows, err := evSvc.Store().Query(context.Background(), events.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows recorded after feeding a derived event back into OnEvent = %d, want 0", len(rows))
	}
}

func TestUntilNextDigest(t *testing.T) {
	svc, _ := newTestService(t, true, Config{DigestHour: 8})

	before := time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC)
	if got, want := svc.untilNextDigest(before), 2*time.Hour; got != want {
		t.Errorf("untilNextDigest(06:00, DigestHour=8) = %v, want %v", got, want)
	}

	after := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	if got, want := svc.untilNextDigest(after), 23*time.Hour; got != want {
		t.Errorf("untilNextDigest(09:00, DigestHour=8) = %v, want %v", got, want)
	}

	exact := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)
	if got, want := svc.untilNextDigest(exact), 24*time.Hour; got != want {
		t.Errorf("untilNextDigest(08:00, DigestHour=8) = %v, want %v (rolls to tomorrow)", got, want)
	}
}

func TestSendDigest(t *testing.T) {
	evSvc := newTestEventsService(t)
	occ := &fakeOccupancy{}
	mailer := &captureMailer{}
	svc := NewService(logr.Discard(), evSvc, occ, mailer, Config{})

	fixed := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }

	data := `{"mode":"summer"}`
	if err := evSvc.Record(context.Background(), events.Event{
		Ts:        float64(fixed.Add(-time.Hour).Unix()),
		DeviceID:  "pool-pump",
		Component: "pool",
		Event:     "pool.run_window",
		Severity:  "notice",
		Data:      &data,
	}); err != nil {
		t.Fatalf("seed Record: %v", err)
	}

	if err := svc.SendDigest(context.Background()); err != nil {
		t.Fatalf("SendDigest: %v", err)
	}
	if mailer.calls != 1 {
		t.Fatalf("mailer.calls = %d, want 1", mailer.calls)
	}
	if !strings.Contains(mailer.body, "pool.run_window") {
		t.Errorf("digest body missing pool.run_window:\n%s", mailer.body)
	}
}
