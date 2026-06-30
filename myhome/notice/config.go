package notice

import "time"

// Config holds the tunables for the notice service: the night window used
// by the motion rule and the hour at which the daily digest email fires.
// Zero values are replaced with sane defaults by NewService.
type Config struct {
	// NightStart/NightEnd are "HH:MM" 24h clock times. The window may wrap
	// past midnight (e.g. "22:00"–"06:00"). Deliberately a fixed,
	// configurable window rather than a computed sunrise/sunset — this
	// keeps the daemon fully offline-capable per the project's
	// internet-optional resilience rule, at the cost of not tracking
	// seasonal day-length changes.
	NightStart string
	NightEnd   string

	// DigestHour (0-23, local time) is the hour the daily digest email is
	// sent, covering the preceding 24h of notice-severity events.
	DigestHour int
}

// DefaultConfig mirrors the defaults documented in docs/configuration.md and
// myhome-example.yaml.
var DefaultConfig = Config{
	NightStart: "22:00",
	NightEnd:   "06:00",
	DigestHour: 8,
}

// withDefaults fills in zero-value fields from DefaultConfig.
func (c Config) withDefaults() Config {
	if c.NightStart == "" {
		c.NightStart = DefaultConfig.NightStart
	}
	if c.NightEnd == "" {
		c.NightEnd = DefaultConfig.NightEnd
	}
	if c.DigestHour == 0 {
		c.DigestHour = DefaultConfig.DigestHour
	}
	return c
}

// isNight reports whether t falls within the configured night window. An
// unparseable or degenerate (start == end) window disables the check rather
// than erroring, so a config typo never blocks the rest of the daemon.
func (c Config) isNight(t time.Time) bool {
	start, errStart := parseHHMM(c.NightStart)
	end, errEnd := parseHHMM(c.NightEnd)
	if errStart != nil || errEnd != nil || start == end {
		return false
	}

	cur := t.Hour()*60 + t.Minute()
	if start < end {
		// Same-day window, e.g. 09:00-17:00.
		return cur >= start && cur < end
	}
	// Wraps past midnight, e.g. 22:00-06:00.
	return cur >= start || cur < end
}

// parseHHMM parses a "HH:MM" string into minutes since midnight.
func parseHHMM(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}
