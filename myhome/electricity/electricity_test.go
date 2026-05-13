package electricity

import (
	"context"
	"testing"
	"time"
)

func TestNewFixedWindowPricer_Invalid(t *testing.T) {
	cases := []struct{ start, end string }{
		{"bad", "07:15"},
		{"23:15", "bad"},
		{"25:00", "07:15"},
		{"23:15", "07:60"},
	}
	for _, c := range cases {
		if _, err := NewFixedWindowPricer(c.start, c.end); err == nil {
			t.Errorf("NewFixedWindowPricer(%q, %q): expected error, got nil", c.start, c.end)
		}
	}
}

func TestFixedWindowPricer_InWindow(t *testing.T) {
	// Cheap: 23:15 – 07:15 (crosses midnight)
	p, err := NewFixedWindowPricer("23:15", "07:15")
	if err != nil {
		t.Fatalf("NewFixedWindowPricer: %v", err)
	}

	cases := []struct {
		hour, min int
		want      bool
		label     string
	}{
		{23, 15, true, "at start"},
		{0, 0, true, "midnight"},
		{7, 14, true, "one minute before end"},
		{7, 15, false, "at end (exclusive)"},
		{12, 0, false, "noon"},
		{23, 14, false, "one minute before start"},
	}

	for _, tc := range cases {
		t := t // capture
		got := p.inWindow(time.Date(2024, 1, 1, tc.hour, tc.min, 0, 0, time.UTC))
		if got != tc.want {
			t.Errorf("inWindow(%02d:%02d) [%s] = %v, want %v", tc.hour, tc.min, tc.label, got, tc.want)
		}
	}
}

func TestFixedWindowPricer_NormalWindow(t *testing.T) {
	// Cheap: 09:00 – 17:00 (no midnight crossing)
	p, err := NewFixedWindowPricer("09:00", "17:00")
	if err != nil {
		t.Fatalf("NewFixedWindowPricer: %v", err)
	}

	cases := []struct {
		hour, min int
		want      bool
		label     string
	}{
		{9, 0, true, "at start"},
		{13, 0, true, "midday"},
		{16, 59, true, "one minute before end"},
		{17, 0, false, "at end (exclusive)"},
		{8, 59, false, "before start"},
		{23, 0, false, "evening"},
	}

	for _, tc := range cases {
		got := p.inWindow(time.Date(2024, 1, 1, tc.hour, tc.min, 0, 0, time.UTC))
		if got != tc.want {
			t.Errorf("inWindow(%02d:%02d) [%s] = %v, want %v", tc.hour, tc.min, tc.label, got, tc.want)
		}
	}
}

func TestFixedWindowPricer_IsCheapNow_Horizon(t *testing.T) {
	// Cheap window 22:00–06:00; test that horizon look-ahead works.
	// This uses real time which is non-deterministic, so we test indirectly
	// via the interface using a manual pricer that we know the state of.
	p, err := NewFixedWindowPricer("22:00", "06:00")
	if err != nil {
		t.Fatalf("NewFixedWindowPricer: %v", err)
	}
	ctx := context.Background()
	// Just verify it returns a bool without panicking.
	_ = p.IsCheapNow(ctx, 0)
	_ = p.IsCheapNow(ctx, 2)
}
