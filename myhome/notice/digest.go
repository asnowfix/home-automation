package notice

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
)

// formatDigest renders the subject and plain-text body of the daily digest
// email from a set of notice-severity events. Pure function for testability;
// notices are expected pre-sorted newest-first (events.Storage.Query orders
// by ts DESC).
func formatDigest(notices []events.Event, now time.Time) (subject, body string) {
	count := len(notices)
	plural := "s"
	if count == 1 {
		plural = ""
	}
	subject = fmt.Sprintf("MyHome notice digest — %s (%d notice%s)", now.Format("2006-01-02"), count, plural)

	var b strings.Builder
	if count == 0 {
		b.WriteString("No notices in the last 24 hours.\n")
		return subject, b.String()
	}

	for _, n := range notices {
		ts := time.Unix(int64(n.Ts), 0).Format("15:04")
		fmt.Fprintf(&b, "%s  %-20s %-10s %s", ts, n.DeviceID, n.Component, n.Event)
		if tail := humanizePoolData(n.Event, n.Data); tail != "" {
			fmt.Fprintf(&b, "  %s", tail)
		}
		b.WriteString("\n")
	}
	return subject, b.String()
}

// humanizePoolData renders the Data payload of a pool.* notice as a short
// human-readable phrase instead of raw JSON — e.g. "speed eco, reason:
// schedule" rather than {"speed":"eco","reason":"schedule","switch_id":0}.
// Falls back to the raw JSON string for any event this function doesn't
// recognize, or whose Data doesn't parse as expected, so no notice ever goes
// unrendered.
func humanizePoolData(event string, data *string) string {
	if data == nil || *data == "" {
		return ""
	}
	raw := *data

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw
	}

	switch event {
	case "pool.run_window":
		mode, _ := m["mode"].(string)
		maxTemp, _ := m["max_temp_c"].(float64)
		if mode == "summer" {
			runHours, _ := m["run_hours"].(float64)
			startH, _ := m["start_h"].(float64)
			stopH, _ := m["stop_h"].(float64)
			return fmt.Sprintf("summer mode, planned run %.1fh %s–%s (forecast max %.0f°C)",
				runHours, hoursToClock(startH), hoursToClock(stopH), maxTemp)
		}
		return fmt.Sprintf("winter mode (forecast max %.0f°C), night schedule only", maxTemp)

	case "pool.pump_start":
		speed, _ := m["speed"].(string)
		reason, _ := m["reason"].(string)
		if reason == "" {
			reason = "unknown"
		}
		return fmt.Sprintf("speed %s, reason: %s", speed, reason)

	case "pool.pump_stop":
		reason, _ := m["reason"].(string)
		if reason == "" {
			reason = "unknown"
		}
		return fmt.Sprintf("reason: %s", reason)

	case "pool.turnover_today":
		achieved, _ := m["turnover_achieved"].(float64)
		target, _ := m["turnover_target"].(float64)
		return fmt.Sprintf("turnover %.2f of %.1f x/day today", achieved, target)

	case "pool.water_supply_protected":
		return "water supply active, pump paused"

	case "pool.water_supply_restored":
		return "water supply cleared, pump resumed"

	case "pool.solar_start":
		solarW, _ := m["solar_w"].(float64)
		thresholdW, _ := m["threshold_w"].(float64)
		return fmt.Sprintf("solar %.0fW ≥ %.0fW threshold", solarW, thresholdW)

	case "pool.solar_stop":
		reason, _ := m["reason"].(string)
		if reason == "" {
			reason = "unknown"
		}
		return fmt.Sprintf("reason: %s", reason)
	}

	return raw
}

// hoursToClock renders a fractional hour (e.g. 11.9) as "HH:MM", wrapping at
// 24h back to 00:00.
func hoursToClock(h float64) string {
	if h < 0 {
		h = 0
	}
	hh := int(h)
	mm := int((h-float64(hh))*60 + 0.5)
	if mm == 60 {
		mm = 0
		hh++
	}
	hh %= 24
	return fmt.Sprintf("%02d:%02d", hh, mm)
}
