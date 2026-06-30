package notice

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

// captureMailer is safe for concurrent use: Start runs the digest scheduler
// on its own goroutine, so tests that drive Start and then inspect call
// counts read/write this from two goroutines.
type captureMailer struct {
	mu      sync.Mutex
	calls   int
	subject string
	body    string
}

func (m *captureMailer) Send(_ context.Context, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.subject = subject
	m.body = body
	return nil
}

func (m *captureMailer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// failingMailer always returns an error, for testing SendDigest's
// mailer-failure branch.
type failingMailer struct{}

func (failingMailer) Send(context.Context, string, string) error {
	return fmt.Errorf("simulated SMTP failure")
}

// countingFailingMailer is failingMailer plus a thread-safe call counter, so
// Start-loop tests can confirm the scheduler keeps running (logs and
// continues) across repeated send failures instead of stopping.
type countingFailingMailer struct {
	mu    sync.Mutex
	calls int
}

func (m *countingFailingMailer) Send(context.Context, string, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return fmt.Errorf("simulated SMTP failure")
}

func (m *countingFailingMailer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func newTestEventsStorage(t *testing.T) *events.Storage {
	t.Helper()
	store, err := events.NewStorage(logr.Discard(), ":memory:")
	if err != nil {
		t.Fatalf("events.NewStorage: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func newTestEventsService(t *testing.T) *events.Service {
	t.Helper()
	return events.NewService(logr.Discard(), newTestEventsStorage(t), nil, nil, 0)
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

// TestSendDigest_QueryError confirms a storage failure is wrapped as
// "query notice events:" rather than panicking.
func TestSendDigest_QueryError(t *testing.T) {
	store := newTestEventsStorage(t)
	evSvc := events.NewService(logr.Discard(), store, nil, nil, 0)
	store.Close() // force the digest's Query call to fail

	svc := NewService(logr.Discard(), evSvc, &fakeOccupancy{}, &captureMailer{}, Config{})
	err := svc.SendDigest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "query notice events:") {
		t.Fatalf("SendDigest() = %v, want error wrapped with \"query notice events:\"", err)
	}
}

// TestSendDigest_MailerError confirms a Mailer.Send failure is wrapped as
// "send digest email:" — this is what Start logs and continues past rather
// than treating as fatal.
func TestSendDigest_MailerError(t *testing.T) {
	evSvc := newTestEventsService(t)
	svc := NewService(logr.Discard(), evSvc, &fakeOccupancy{}, failingMailer{}, Config{})

	err := svc.SendDigest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "send digest email:") {
		t.Fatalf("SendDigest() = %v, want error wrapped with \"send digest email:\"", err)
	}
}

// TestOnEvent_RecordFailureDoesNotPanic exercises recordDerived's
// events.Record error branch: with the underlying store closed, OnEvent
// must log and return rather than panic or propagate.
func TestOnEvent_RecordFailureDoesNotPanic(t *testing.T) {
	store := newTestEventsStorage(t)
	evSvc := events.NewService(logr.Discard(), store, nil, nil, 0)
	store.Close()

	svc := NewService(logr.Discard(), evSvc, &fakeOccupancy{occupied: false}, &captureMailer{}, Config{})
	src := events.Event{Ts: float64(time.Now().Unix()), DeviceID: "dev1", Event: "motion.detected"}
	svc.OnEvent(context.Background(), src) // must not panic
}

// TestStart_RunsDigestOnScheduleAndStopsOnCancel drives the real Start loop
// (not just SendDigest/untilNextDigest in isolation): it sets now() to land
// a few milliseconds before DigestHour so the loop fires quickly and
// repeatedly, confirms at least two digests are sent, then cancels ctx and
// confirms Start actually returns.
func TestStart_RunsDigestOnScheduleAndStopsOnCancel(t *testing.T) {
	evSvc := newTestEventsService(t)
	mailer := &captureMailer{}
	svc := NewService(logr.Discard(), evSvc, &fakeOccupancy{}, mailer, Config{DigestHour: 10})

	target := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	almostThere := target.Add(-5 * time.Millisecond)
	svc.now = func() time.Time { return almostThere }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for mailer.callCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("digest scheduler only fired %d time(s) within 2s, want >= 2", mailer.callCount())
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s of context cancellation")
	}
}

// TestStart_LogsAndContinuesPastDigestFailures covers Start's error-logging
// branch: a digest send failure (offline SMTP, bad creds) must not stop the
// scheduler — it keeps firing on the same cadence.
func TestStart_LogsAndContinuesPastDigestFailures(t *testing.T) {
	evSvc := newTestEventsService(t)
	mailer := &countingFailingMailer{}
	svc := NewService(logr.Discard(), evSvc, &fakeOccupancy{}, mailer, Config{DigestHour: 10})

	target := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	almostThere := target.Add(-5 * time.Millisecond)
	svc.now = func() time.Time { return almostThere }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for mailer.callCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("digest scheduler only attempted %d send(s) within 2s, want >= 2", mailer.callCount())
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s of context cancellation")
	}
}
