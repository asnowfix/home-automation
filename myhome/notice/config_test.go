package notice

import (
	"testing"
	"time"
)

func TestConfig_IsNight_Wraparound(t *testing.T) {
	cfg := Config{NightStart: "22:00", NightEnd: "06:00"}

	cases := []struct {
		hhmm string
		want bool
	}{
		{"21:59", false},
		{"22:00", true},
		{"23:30", true},
		{"00:00", true},
		{"03:00", true},
		{"05:59", true},
		{"06:00", false}, // end is exclusive
		{"06:01", false},
		{"12:00", false},
	}

	for _, c := range cases {
		ts, err := time.Parse("15:04", c.hhmm)
		if err != nil {
			t.Fatalf("parse %q: %v", c.hhmm, err)
		}
		got := cfg.isNight(ts)
		if got != c.want {
			t.Errorf("isNight(%s) = %v, want %v", c.hhmm, got, c.want)
		}
	}
}

func TestConfig_IsNight_SameDayWindow(t *testing.T) {
	// A non-wrapping window (start < end) should also work, e.g. a
	// "quiet hours" daytime window.
	cfg := Config{NightStart: "09:00", NightEnd: "17:00"}

	for _, c := range []struct {
		hhmm string
		want bool
	}{
		{"08:59", false},
		{"09:00", true},
		{"12:00", true},
		{"16:59", true},
		{"17:00", false},
	} {
		ts, _ := time.Parse("15:04", c.hhmm)
		if got := cfg.isNight(ts); got != c.want {
			t.Errorf("isNight(%s) = %v, want %v", c.hhmm, got, c.want)
		}
	}
}

func TestConfig_IsNight_DegenerateWindowDisabled(t *testing.T) {
	cfg := Config{NightStart: "22:00", NightEnd: "22:00"}
	ts, _ := time.Parse("15:04", "23:00")
	if cfg.isNight(ts) {
		t.Error("isNight() with start == end should always be false (disabled), got true")
	}
}

func TestConfig_IsNight_UnparseableWindowDisabled(t *testing.T) {
	cfg := Config{NightStart: "not-a-time", NightEnd: "06:00"}
	ts, _ := time.Parse("15:04", "23:00")
	if cfg.isNight(ts) {
		t.Error("isNight() with an unparseable window should be false (disabled), got true")
	}
}

func TestConfig_WithDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	if got != DefaultConfig {
		t.Errorf("Config{}.withDefaults() = %+v, want %+v", got, DefaultConfig)
	}

	// Explicit values are preserved.
	got = Config{NightStart: "23:00", NightEnd: "05:00", DigestHour: 7}.withDefaults()
	want := Config{NightStart: "23:00", NightEnd: "05:00", DigestHour: 7}
	if got != want {
		t.Errorf("withDefaults() = %+v, want %+v", got, want)
	}
}
