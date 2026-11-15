package myhome

import (
	"testing"
	"time"
)

func TestHeatingTarget(t *testing.T) {
	levels := map[string]float64{"away": 14, "eco": 17, "comfort": 20}
	day := []TemperatureTimeRange{{Start: 6*60 + 30, End: 8 * 60}, {Start: 18 * 60, End: 22 * 60}}
	night := []TemperatureTimeRange{{Start: 22 * 60, End: 6 * 60}}
	yes, no := true, false
	at := func(h, m int) time.Time { return time.Date(2026, 11, 8, h, m, 0, 0, time.UTC) }
	hm := func(d, h, m int) *time.Time { u := time.Date(2026, 11, d, h, m, 0, 0, time.UTC); return &u }

	tests := []struct {
		name      string
		levels    map[string]float64
		ranges    []TemperatureTimeRange
		occupied  *bool
		now       time.Time
		wantLevel string
		wantTemp  float64
		wantUntil *time.Time
		wantOK    bool
	}{
		{"occupied inside range", levels, day, &yes, at(19, 0), "comfort", 20, hm(8, 22, 0), true},
		{"occupied outside range, next range today", levels, day, &yes, at(12, 0), "eco", 17, hm(8, 18, 0), true},
		{"occupied after last range", levels, day, &yes, at(23, 0), "eco", 17, nil, true},
		{"range end is exclusive", levels, day, &yes, at(22, 0), "eco", 17, nil, true},
		{"range start is inclusive", levels, day, &yes, at(18, 0), "comfort", 20, hm(8, 22, 0), true},
		{"unoccupied inside range", levels, day, &no, at(19, 0), "away", 14, nil, true},
		{"nil occupancy is away", levels, day, nil, at(19, 0), "away", 14, nil, true},
		{"crossing midnight, evening side", levels, night, &yes, at(23, 30), "comfort", 20, hm(9, 6, 0), true},
		{"crossing midnight, morning side", levels, night, &yes, at(5, 59), "comfort", 20, hm(8, 6, 0), true},
		{"crossing midnight, outside", levels, night, &yes, at(12, 0), "eco", 17, hm(8, 22, 0), true},
		{"no ranges", levels, nil, &yes, at(12, 0), "eco", 17, nil, true},
		{"missing comfort level", map[string]float64{"eco": 17, "away": 14}, day, &yes, at(19, 0), "comfort", 0, nil, false},
		{"missing away level", map[string]float64{"eco": 17}, day, nil, at(19, 0), "away", 0, nil, false},
		{"nil levels", nil, day, &yes, at(12, 0), "eco", 0, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, celsius, until, ok := HeatingTarget(tt.levels, tt.ranges, tt.occupied, tt.now)
			if level != tt.wantLevel || celsius != tt.wantTemp || ok != tt.wantOK {
				t.Fatalf("got (%q, %v, ok=%v), want (%q, %v, ok=%v)", level, celsius, ok, tt.wantLevel, tt.wantTemp, tt.wantOK)
			}
			switch {
			case until == nil && tt.wantUntil == nil:
			case until == nil || tt.wantUntil == nil || !until.Equal(*tt.wantUntil):
				t.Fatalf("until = %v, want %v", until, tt.wantUntil)
			}
		})
	}
}
