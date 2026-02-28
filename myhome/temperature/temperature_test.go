package temperature

import (
	"context"
	"errors"
	"testing"
	"time"

	"myhome"
)

// TestIsInTimeRange verifies normal and midnight-crossing ranges.
func TestIsInTimeRange(t *testing.T) {
	cases := []struct {
		name    string
		current int
		tr      TimeRange
		want    bool
	}{
		{"normal range inside", 720, TimeRange{360, 1380}, true},
		{"normal range before start", 300, TimeRange{360, 1380}, false},
		{"normal range at end exclusive", 1380, TimeRange{360, 1380}, false},
		{"normal range at start inclusive", 360, TimeRange{360, 1380}, true},
		{"midnight crossing before midnight", 1400, TimeRange{1380, 360}, true},
		{"midnight crossing after midnight", 200, TimeRange{1380, 360}, true},
		{"midnight crossing at start", 1380, TimeRange{1380, 360}, true},
		{"midnight crossing outside", 720, TimeRange{1380, 360}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isInTimeRange(tc.current, tc.tr)
			if got != tc.want {
				t.Errorf("isInTimeRange(%d, %+v) = %v, want %v", tc.current, tc.tr, got, tc.want)
			}
		})
	}
}

// TestParseTime verifies "HH:MM" → minutes conversion and error cases.
func TestParseTime(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"valid 06:00", "06:00", 360, false},
		{"valid 12:30", "12:30", 750, false},
		{"boundary 23:59", "23:59", 1439, false},
		{"midnight 00:00", "00:00", 0, false},
		{"invalid format no colon", "600", 0, true},
		{"invalid format dash", "6-00", 0, true},
		{"invalid hour 24", "24:00", 0, true},
		{"invalid minute 60", "00:60", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTime(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseTime(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("parseTime(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// TestGetDayType_BuiltInDefaults verifies the built-in weekday/weekend defaults.
func TestGetDayType_BuiltInDefaults(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // known Monday
	if got := svc.getDayType(ctx, "r1", monday); got != myhome.DayTypeWorkDay {
		t.Errorf("Monday: got %v, want %v", got, myhome.DayTypeWorkDay)
	}

	saturday := time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC) // known Saturday
	if got := svc.getDayType(ctx, "r1", saturday); got != myhome.DayTypeDayOff {
		t.Errorf("Saturday: got %v, want %v", got, myhome.DayTypeDayOff)
	}

	sunday := time.Date(2024, 1, 7, 0, 0, 0, 0, time.UTC) // known Sunday
	if got := svc.getDayType(ctx, "r1", sunday); got != myhome.DayTypeDayOff {
		t.Errorf("Sunday: got %v, want %v", got, myhome.DayTypeDayOff)
	}
}

// TestGetDayType_WeekdayDefaultOverride verifies that per-room weekday overrides
// take precedence over built-in defaults.
func TestGetDayType_WeekdayDefaultOverride(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Override Monday (weekday 1) → day-off for room r1.
	svc.weekdayDefaults["r1"] = map[int]myhome.DayType{
		1: myhome.DayTypeDayOff,
	}

	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := svc.getDayType(ctx, "r1", monday); got != myhome.DayTypeDayOff {
		t.Errorf("Monday with override: got %v, want %v", got, myhome.DayTypeDayOff)
	}
}

// TestGetDayType_ExternalAPI verifies that a configured external API takes
// priority over both weekday defaults and built-in defaults.
func TestGetDayType_ExternalAPI(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	svc.externalDayTypeAPI = func(_ context.Context, _ string, _ time.Time) (myhome.DayType, error) {
		return myhome.DayTypeDayOff, nil
	}

	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := svc.getDayType(ctx, "r1", monday); got != myhome.DayTypeDayOff {
		t.Errorf("Monday with external API: got %v, want %v", got, myhome.DayTypeDayOff)
	}
}

// TestGetDayType_ExternalAPIFallback verifies that when the external API returns
// an error, the service falls back to weekday defaults.
func TestGetDayType_ExternalAPIFallback(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	svc.externalDayTypeAPI = func(_ context.Context, _ string, _ time.Time) (myhome.DayType, error) {
		return "", errors.New("api unavailable")
	}

	// Monday (weekday 1) falls back to built-in work-day.
	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := svc.getDayType(ctx, "r1", monday); got != myhome.DayTypeWorkDay {
		t.Errorf("Monday with failing API: got %v, want %v", got, myhome.DayTypeWorkDay)
	}
}

// TestGetComfortRanges exercises the main comfort-range computation.
func TestGetComfortRanges(t *testing.T) {
	ctx := context.Background()

	t.Run("room with single kind and schedule", func(t *testing.T) {
		svc, _ := newTestService(t)
		svc.rooms["r1"] = &RoomConfig{ID: "r1", Name: "R1", Kinds: []myhome.RoomKind{myhome.RoomKindBedroom}}
		svc.kindSchedules[myhome.RoomKindBedroom] = map[myhome.DayType][]TimeRange{
			myhome.DayTypeWorkDay: {{Start: 360, End: 1380}},
		}

		monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		ranges, dayType, err := svc.GetComfortRanges(ctx, "r1", monday)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dayType != myhome.DayTypeWorkDay {
			t.Errorf("dayType: got %v, want %v", dayType, myhome.DayTypeWorkDay)
		}
		if len(ranges) != 1 || ranges[0].Start != 360 || ranges[0].End != 1380 {
			t.Errorf("unexpected ranges: %v", ranges)
		}
	})

	t.Run("room with two kinds returns union", func(t *testing.T) {
		svc, _ := newTestService(t)
		svc.rooms["r1"] = &RoomConfig{
			ID:    "r1",
			Name:  "R1",
			Kinds: []myhome.RoomKind{myhome.RoomKindBedroom, myhome.RoomKindOffice},
		}
		svc.kindSchedules[myhome.RoomKindBedroom] = map[myhome.DayType][]TimeRange{
			myhome.DayTypeWorkDay: {{Start: 360, End: 720}},
		}
		svc.kindSchedules[myhome.RoomKindOffice] = map[myhome.DayType][]TimeRange{
			myhome.DayTypeWorkDay: {{Start: 540, End: 1080}},
		}

		monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		ranges, _, err := svc.GetComfortRanges(ctx, "r1", monday)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ranges) != 2 {
			t.Errorf("expected 2 ranges (union of both kinds), got %d: %v", len(ranges), ranges)
		}
	})

	t.Run("room with kind but no schedule for day type", func(t *testing.T) {
		svc, _ := newTestService(t)
		svc.rooms["r1"] = &RoomConfig{ID: "r1", Name: "R1", Kinds: []myhome.RoomKind{myhome.RoomKindBedroom}}
		svc.kindSchedules[myhome.RoomKindBedroom] = map[myhome.DayType][]TimeRange{
			myhome.DayTypeWorkDay: {{Start: 360, End: 1380}},
		}

		// Saturday → day-off; only work-day schedule exists → empty.
		saturday := time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC)
		ranges, _, err := svc.GetComfortRanges(ctx, "r1", saturday)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ranges) != 0 {
			t.Errorf("expected empty ranges for day-off, got %v", ranges)
		}
	})

	t.Run("room not found returns error", func(t *testing.T) {
		svc, _ := newTestService(t)
		_, _, err := svc.GetComfortRanges(ctx, "nonexistent", time.Now())
		if err == nil {
			t.Error("expected error for unknown room, got nil")
		}
	})

	t.Run("deduplication: identical ranges from two kinds appear once", func(t *testing.T) {
		svc, _ := newTestService(t)
		svc.rooms["r1"] = &RoomConfig{
			ID:    "r1",
			Name:  "R1",
			Kinds: []myhome.RoomKind{myhome.RoomKindBedroom, myhome.RoomKindOffice},
		}
		// Both kinds share the same range.
		same := []TimeRange{{Start: 360, End: 1380}}
		svc.kindSchedules[myhome.RoomKindBedroom] = map[myhome.DayType][]TimeRange{
			myhome.DayTypeWorkDay: same,
		}
		svc.kindSchedules[myhome.RoomKindOffice] = map[myhome.DayType][]TimeRange{
			myhome.DayTypeWorkDay: same,
		}

		monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		ranges, _, err := svc.GetComfortRanges(ctx, "r1", monday)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ranges) != 1 {
			t.Errorf("expected 1 (deduplicated) range, got %d: %v", len(ranges), ranges)
		}
	})
}

// TestIsComfortTime checks normal and midnight-crossing comfort windows.
func TestIsComfortTime(t *testing.T) {
	svc, _ := newTestService(t)
	kinds := []myhome.RoomKind{myhome.RoomKindBedroom}
	svc.kindSchedules[myhome.RoomKindBedroom] = map[myhome.DayType][]TimeRange{
		myhome.DayTypeWorkDay: {{Start: 360, End: 1380}},   // 06:00-23:00
		myhome.DayTypeDayOff:  {{Start: 1380, End: 360}},   // 23:00-06:00 (crosses midnight)
	}

	if !svc.isComfortTime(kinds, myhome.DayTypeWorkDay, tod(12, 0)) {
		t.Error("12:00 work-day should be comfort time")
	}
	if svc.isComfortTime(kinds, myhome.DayTypeWorkDay, tod(5, 0)) {
		t.Error("05:00 work-day should be eco time")
	}
	// Midnight-crossing range: 23:30 and 04:00 should both be inside.
	if !svc.isComfortTime(kinds, myhome.DayTypeDayOff, tod(23, 30)) {
		t.Error("23:30 day-off should be comfort time (midnight-crossing range)")
	}
	if !svc.isComfortTime(kinds, myhome.DayTypeDayOff, tod(4, 0)) {
		t.Error("04:00 day-off should be comfort time (midnight-crossing range)")
	}
	// 12:00 is outside the midnight-crossing range.
	if svc.isComfortTime(kinds, myhome.DayTypeDayOff, tod(12, 0)) {
		t.Error("12:00 day-off should be eco time (outside midnight-crossing range)")
	}
}

// tod returns a time.Time set to hour:minute on an arbitrary fixed date (UTC).
func tod(hour, minute int) time.Time {
	return time.Date(2024, 1, 1, hour, minute, 0, 0, time.UTC)
}
