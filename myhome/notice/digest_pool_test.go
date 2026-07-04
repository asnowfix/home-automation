package notice

import (
	"strings"
	"testing"
)

func TestHoursToClock(t *testing.T) {
	cases := []struct {
		h    float64
		want string
	}{
		{11.9, "11:54"},
		{0, "00:00"},
		{23.999, "00:00"}, // rounds up into the next hour
		{-1, "00:00"},
	}
	for _, c := range cases {
		if got := hoursToClock(c.h); got != c.want {
			t.Errorf("hoursToClock(%v) = %q, want %q", c.h, got, c.want)
		}
	}
}

func TestHumanizePoolData(t *testing.T) {
	cases := []struct {
		name  string
		event string
		data  string
		want  []string
	}{
		{
			name:  "run_window summer",
			event: "pool.run_window",
			data:  `{"mode":"summer","max_temp_c":31,"run_hours":4.2,"start_h":11.9,"stop_h":16.1}`,
			want:  []string{"summer mode", "4.2h", "11:54", "31°C"},
		},
		{
			name:  "run_window winter",
			event: "pool.run_window",
			data:  `{"mode":"winter","max_temp_c":12}`,
			want:  []string{"winter mode", "12°C"},
		},
		{
			name:  "pump_start carries reason",
			event: "pool.pump_start",
			data:  `{"speed":"eco","switch_id":0,"reason":"Morning start event"}`,
			want:  []string{"speed eco", "Morning start event"},
		},
		{
			name:  "pump_stop carries reason",
			event: "pool.pump_stop",
			data:  `{"reason":"Evening stop event"}`,
			want:  []string{"Evening stop event"},
		},
		{
			name:  "turnover_today",
			event: "pool.turnover_today",
			data:  `{"turnover_achieved":3.2,"turnover_target":5,"runtime_sec":9000}`,
			want:  []string{"3.20", "5.0"},
		},
		{
			name:  "water_supply_protected",
			event: "pool.water_supply_protected",
			data:  `{"saved_output":1}`,
			want:  []string{"paused"},
		},
		{
			name:  "water_supply_restored",
			event: "pool.water_supply_restored",
			data:  `{"restored_output":1}`,
			want:  []string{"resumed"},
		},
		{
			name:  "solar_start",
			event: "pool.solar_start",
			data:  `{"solar_w":650,"threshold_w":500,"held_for_s":120}`,
			want:  []string{"650W", "500W"},
		},
		{
			name:  "solar_stop",
			event: "pool.solar_stop",
			data:  `{"reason":"hard_ceiling","runtime_sec":7200}`,
			want:  []string{"hard_ceiling"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := humanizePoolData(c.event, &c.data)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("humanizePoolData(%q, %q) = %q, missing %q", c.event, c.data, got, want)
				}
			}
		})
	}
}

func TestHumanizePoolDataFallsBackForUnrecognizedEvent(t *testing.T) {
	data := `{"foo":"bar"}`
	got := humanizePoolData("garden.plan", &data)
	if got != data {
		t.Errorf("expected raw JSON fallback for unrecognized event, got %q", got)
	}
}

func TestHumanizePoolDataNilOrEmpty(t *testing.T) {
	if got := humanizePoolData("pool.pump_start", nil); got != "" {
		t.Errorf("expected empty string for nil data, got %q", got)
	}
	empty := ""
	if got := humanizePoolData("pool.pump_start", &empty); got != "" {
		t.Errorf("expected empty string for empty data, got %q", got)
	}
}

func TestHumanizePoolDataFallsBackOnUnparseableJSON(t *testing.T) {
	bad := "not json"
	got := humanizePoolData("pool.pump_start", &bad)
	if got != bad {
		t.Errorf("expected raw fallback for unparseable JSON, got %q", got)
	}
}
