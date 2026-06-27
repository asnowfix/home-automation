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

func TestNewMultiIntervalPricerFromString_Invalid(t *testing.T) {
	cases := []string{
		"",
		"23:15",        // missing end
		"bad-07:15",    // bad start
		"23:15-bad",    // bad end
		"23:15-07:15,", // trailing comma → empty segment
	}
	for _, s := range cases {
		if _, err := NewMultiIntervalPricerFromString(s); err == nil {
			t.Errorf("NewMultiIntervalPricerFromString(%q): expected error, got nil", s)
		}
	}
}

func TestWindowInterval_InWindow_MidnightCrossing(t *testing.T) {
	// Cheap: 23:15 – 07:15 (crosses midnight)
	iv := windowInterval{startMin: 23*60 + 15, endMin: 7*60 + 15}

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
		got := iv.inWindow(time.Date(2024, 1, 1, tc.hour, tc.min, 0, 0, time.UTC))
		if got != tc.want {
			t.Errorf("inWindow(%02d:%02d) [%s] = %v, want %v", tc.hour, tc.min, tc.label, got, tc.want)
		}
	}
}

func TestWindowInterval_InWindow_Normal(t *testing.T) {
	// Cheap: 09:00 – 17:00 (no midnight crossing)
	iv := windowInterval{startMin: 9 * 60, endMin: 17 * 60}

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
		got := iv.inWindow(time.Date(2024, 1, 1, tc.hour, tc.min, 0, 0, time.UTC))
		if got != tc.want {
			t.Errorf("inWindow(%02d:%02d) [%s] = %v, want %v", tc.hour, tc.min, tc.label, got, tc.want)
		}
	}
}

func TestMultiIntervalPricer_TwoIntervals(t *testing.T) {
	// Night window (23:15–07:15) plus midday window (12:00–14:00)
	p, err := NewMultiIntervalPricerFromString("23:15-07:15,12:00-14:00")
	if err != nil {
		t.Fatalf("NewMultiIntervalPricerFromString: %v", err)
	}

	cases := []struct {
		hour, min int
		want      bool
		label     string
	}{
		{23, 15, true, "night window start"},
		{1, 0, true, "in night window"},
		{7, 14, true, "night window near end"},
		{7, 15, false, "after night window"},
		{12, 0, true, "midday window start"},
		{13, 0, true, "in midday window"},
		{14, 0, false, "after midday window"},
		{10, 0, false, "between windows"},
		{22, 0, false, "before night window"},
	}

	ctx := context.Background()
	for _, tc := range cases {
		// Inject a known time by checking via IsCheapNow indirectly through inWindow logic.
		// Since IsCheapNow uses time.Now(), we test inWindow on each interval directly.
		inAny := false
		for _, iv := range p.intervals {
			if iv.inWindow(time.Date(2024, 1, 1, tc.hour, tc.min, 0, 0, time.UTC)) {
				inAny = true
				break
			}
		}
		if inAny != tc.want {
			t.Errorf("multi-interval %02d:%02d [%s] = %v, want %v", tc.hour, tc.min, tc.label, inAny, tc.want)
		}
	}

	// Sanity: IsCheapNow doesn't panic with two intervals
	_ = p.IsCheapNow(ctx, 0)
	_ = p.IsCheapNow(ctx, 2)
}

func TestMultiIntervalPricer_UntilEpoch_ReturnsEarliest(t *testing.T) {
	// Two overlapping windows; if both are active, UntilEpoch should return the earliest end.
	// Window 1: 09:00–17:00 ; Window 2: 10:00–12:00
	// At 11:00 both are active; earliest end is 12:00.
	p, err := NewMultiIntervalPricerFromString("09:00-17:00,10:00-12:00")
	if err != nil {
		t.Fatalf("NewMultiIntervalPricerFromString: %v", err)
	}
	loc := time.UTC
	now := time.Date(2024, 6, 1, 11, 0, 0, 0, loc)
	got := p.UntilEpoch(now)
	want := time.Date(2024, 6, 1, 12, 0, 0, 0, loc).Unix()
	if got != want {
		t.Errorf("UntilEpoch at 11:00 = %d, want %d", got, want)
	}
}
