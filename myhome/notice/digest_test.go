package notice

import (
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
)

func TestFormatDigest_Empty(t *testing.T) {
	now := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)
	subject, body := formatDigest(nil, now)

	if !strings.Contains(subject, "2026-06-30") {
		t.Errorf("subject missing date: %q", subject)
	}
	if !strings.Contains(subject, "(0 notices)") {
		t.Errorf("subject missing zero-count plural: %q", subject)
	}
	if !strings.Contains(body, "No notices in the last 24 hours") {
		t.Errorf("body for zero notices = %q", body)
	}
}

func TestFormatDigest_Singular(t *testing.T) {
	now := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)
	eventTs := now.Add(-2 * time.Hour)
	data := `{"max_temp_c":28}`
	notices := []events.Event{
		{Ts: float64(eventTs.Unix()), DeviceID: "pool-pump", Component: "pool", Event: "pool.run_window", Data: &data},
	}
	subject, body := formatDigest(notices, now)

	if !strings.Contains(subject, "(1 notice)") {
		t.Errorf("subject for singular count = %q, want \"(1 notice)\"", subject)
	}
	// formatDigest renders Ts via time.Unix(...).Format("15:04"), which (like
	// the UI's formatTime) converts to the local machine timezone — so the
	// expected clock string must be derived the same way rather than
	// hardcoded, to keep this test timezone-independent.
	wantClock := time.Unix(eventTs.Unix(), 0).Format("15:04")
	for _, want := range []string{wantClock, "pool-pump", "pool", "pool.run_window", "max_temp_c"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest body missing %q:\n%s", want, body)
		}
	}
}

func TestFormatDigest_MultipleNotices(t *testing.T) {
	now := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)
	notices := []events.Event{
		{Ts: float64(now.Add(-1 * time.Hour).Unix()), DeviceID: "garden", Component: "garden", Event: "garden.plan"},
		{Ts: float64(now.Add(-2 * time.Hour).Unix()), DeviceID: "pool-pump", Component: "solar", Event: "pool.solar_start"},
		{Ts: float64(now.Add(-3 * time.Hour).Unix()), DeviceID: "motion-1", Component: "motion", Event: "motion.absent"},
	}
	subject, body := formatDigest(notices, now)

	if !strings.Contains(subject, "(3 notices)") {
		t.Errorf("subject = %q, want \"(3 notices)\"", subject)
	}
	lines := strings.Count(strings.TrimRight(body, "\n"), "\n") + 1
	if lines != 3 {
		t.Errorf("body line count = %d, want 3:\n%s", lines, body)
	}
	for _, want := range []string{"garden.plan", "pool.solar_start", "motion.absent"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest body missing %q:\n%s", want, body)
		}
	}
}

// TestFormatDigest_NilDataDoesNotPanic guards against a nil Data pointer
// (the common case — most events carry no extra payload).
func TestFormatDigest_NilDataDoesNotPanic(t *testing.T) {
	notices := []events.Event{
		{Ts: float64(time.Now().Unix()), DeviceID: "dev1", Component: "pool", Event: "pool.pump_start", Data: nil},
	}
	_, body := formatDigest(notices, time.Now())
	if !strings.Contains(body, "pool.pump_start") {
		t.Errorf("digest body missing event with nil Data:\n%s", body)
	}
}
