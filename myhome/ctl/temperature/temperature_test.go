package temperature

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// withFakeClient installs a FakeClient as myhome.TheClient for the duration
// of the test and restores the previous value on cleanup. myhome.TheClient
// is a package-level global shared across the whole test binary, so tests
// using it must not run in parallel.
func withFakeClient(t *testing.T) *myhome.FakeClient {
	t.Helper()
	prev := myhome.TheClient
	fake := myhome.NewFakeClient()
	myhome.TheClient = fake
	t.Cleanup(func() {
		myhome.TheClient = prev
	})
	return fake
}

// --- CLI-level tests: client injection via myhome.TheClient / FakeClient ---

func TestGetCmd_HappyPath(t *testing.T) {
	fake := withFakeClient(t)
	fake.SetResult(myhome.TemperatureGet, &myhome.TemperatureRoomConfig{
		RoomID: "salon",
		Name:   "Salon",
		Kinds:  []myhome.RoomKind{myhome.RoomKindLivingRoom},
		Levels: map[string]float64{"eco": 17},
	})

	getCmd.SetContext(context.Background())
	err := getCmd.RunE(getCmd, []string{"salon"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fake.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fake.Calls))
	}
	if fake.Calls[0].Method != myhome.TemperatureGet {
		t.Errorf("expected verb %s, got %s", myhome.TemperatureGet, fake.Calls[0].Method)
	}
	params, ok := fake.Calls[0].Params.(*myhome.TemperatureGetParams)
	if !ok {
		t.Fatalf("expected *TemperatureGetParams, got %T", fake.Calls[0].Params)
	}
	if params.RoomID != "salon" {
		t.Errorf("expected room_id 'salon', got %q", params.RoomID)
	}
}

func TestGetCmd_PropagatesClientError(t *testing.T) {
	fake := withFakeClient(t)
	wantErr := errors.New("device unreachable")
	fake.SetError(myhome.TemperatureGet, wantErr)

	getCmd.SetContext(context.Background())
	err := getCmd.RunE(getCmd, []string{"salon"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
}

func TestGetCmd_UnexpectedResultType(t *testing.T) {
	fake := withFakeClient(t)
	fake.SetResult(myhome.TemperatureGet, "not-a-config")

	getCmd.SetContext(context.Background())
	err := getCmd.RunE(getCmd, []string{"salon"})
	if err == nil {
		t.Fatal("expected an error for unexpected result type, got nil")
	}
}

func TestListCmd_HappyPath(t *testing.T) {
	fake := withFakeClient(t)
	rooms := myhome.TemperatureRoomList{
		"salon": {RoomID: "salon", Name: "Salon", Levels: map[string]float64{"eco": 17}},
	}
	fake.SetResult(myhome.TemperatureList, &rooms)

	listCmd.SetContext(context.Background())
	err := listCmd.RunE(listCmd, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.Calls) != 1 || fake.Calls[0].Method != myhome.TemperatureList {
		t.Fatalf("expected a single TemperatureList call, got %+v", fake.Calls)
	}
}

func TestListCmd_Empty(t *testing.T) {
	fake := withFakeClient(t)
	empty := myhome.TemperatureRoomList{}
	fake.SetResult(myhome.TemperatureList, &empty)

	listCmd.SetContext(context.Background())
	if err := listCmd.RunE(listCmd, []string{}); err != nil {
		t.Fatalf("unexpected error on empty list: %v", err)
	}
}

func TestDeleteCmd_HappyPath(t *testing.T) {
	fake := withFakeClient(t)
	fake.SetResult(myhome.TemperatureDelete, &myhome.TemperatureDeleteResult{RoomID: "salon", Status: "deleted"})

	deleteCmd.SetContext(context.Background())
	err := deleteCmd.RunE(deleteCmd, []string{"salon"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	params, ok := fake.Calls[0].Params.(*myhome.TemperatureDeleteParams)
	if !ok {
		t.Fatalf("expected *TemperatureDeleteParams, got %T", fake.Calls[0].Params)
	}
	if params.RoomID != "salon" {
		t.Errorf("expected room_id 'salon', got %q", params.RoomID)
	}
}

// --- Pure function tests: extracted decision logic ---

func TestBuildNewRoomParams(t *testing.T) {
	tests := []struct {
		name       string
		nameSet    bool
		kindsStr   string
		kindsSet   bool
		eco        float64
		ecoSet     bool
		comfortSet bool
		comfort    float64
		awaySet    bool
		away       float64
		wantErr    bool
		wantLevels map[string]float64
	}{
		{
			name: "missing name", nameSet: false, kindsSet: true, ecoSet: true, wantErr: true,
		},
		{
			name: "missing kinds", nameSet: true, kindsSet: false, ecoSet: true, wantErr: true,
		},
		{
			name: "missing eco", nameSet: true, kindsSet: true, ecoSet: false, wantErr: true,
		},
		{
			name: "invalid kinds string", nameSet: true, kindsStr: "", kindsSet: true, ecoSet: true, eco: 17, wantErr: true,
		},
		{
			name: "eco only", nameSet: true, kindsStr: "bedroom", kindsSet: true, eco: 17, ecoSet: true,
			wantLevels: map[string]float64{"eco": 17},
		},
		{
			name: "eco+comfort+away", nameSet: true, kindsStr: "bedroom", kindsSet: true, eco: 17, ecoSet: true,
			comfortSet: true, comfort: 21, awaySet: true, away: 14,
			wantLevels: map[string]float64{"eco": 17, "comfort": 21, "away": 14},
		},
		{
			name: "comfort set but non-positive is dropped", nameSet: true, kindsStr: "bedroom", kindsSet: true, eco: 17, ecoSet: true,
			comfortSet: true, comfort: 0,
			wantLevels: map[string]float64{"eco": 17},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := buildNewRoomParams("living-room", "Living Room", tt.nameSet, tt.kindsStr, tt.kindsSet, tt.eco, tt.ecoSet, tt.comfortSet, tt.comfort, tt.awaySet, tt.away)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if params.RoomID != "living-room" || params.Name != "Living Room" {
				t.Errorf("unexpected room id/name: %+v", params)
			}
			if len(params.Levels) != len(tt.wantLevels) {
				t.Fatalf("expected levels %v, got %v", tt.wantLevels, params.Levels)
			}
			for k, v := range tt.wantLevels {
				if params.Levels[k] != v {
					t.Errorf("level %s: expected %v, got %v", k, v, params.Levels[k])
				}
			}
		})
	}
}

func TestBuildUpdatedRoomParams(t *testing.T) {
	existing := &myhome.TemperatureRoomConfig{
		RoomID: "salon",
		Name:   "Salon",
		Kinds:  []myhome.RoomKind{myhome.RoomKindLivingRoom},
		Levels: map[string]float64{"eco": 17, "comfort": 21},
	}

	t.Run("no flags set: unchanged", func(t *testing.T) {
		params, err := buildUpdatedRoomParams("salon", existing, "", false, "", false, 0, false, false, 0, false, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.Name != "Salon" || len(params.Levels) != 2 {
			t.Errorf("expected unchanged config, got %+v", params)
		}
	})

	t.Run("only away set: added", func(t *testing.T) {
		params, err := buildUpdatedRoomParams("salon", existing, "", false, "", false, 0, false, false, 0, true, 14)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.Levels["away"] != 14 || params.Levels["eco"] != 17 || params.Levels["comfort"] != 21 {
			t.Errorf("expected away added while eco/comfort preserved, got %+v", params.Levels)
		}
	})

	t.Run("comfort set to non-positive: removed", func(t *testing.T) {
		params, err := buildUpdatedRoomParams("salon", existing, "", false, "", false, 0, false, true, 0, false, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := params.Levels["comfort"]; ok {
			t.Errorf("expected comfort removed, got %+v", params.Levels)
		}
		if params.Levels["eco"] != 17 {
			t.Errorf("expected eco preserved, got %+v", params.Levels)
		}
	})

	t.Run("invalid kinds string errors", func(t *testing.T) {
		_, err := buildUpdatedRoomParams("salon", existing, "", false, "", true, 0, false, false, 0, false, 0)
		if err == nil {
			t.Fatal("expected an error for empty kinds string when kindsSet=true")
		}
	})

	t.Run("name and kinds overridden", func(t *testing.T) {
		params, err := buildUpdatedRoomParams("salon", existing, "New Name", true, "office,kitchen", true, 0, false, false, 0, false, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.Name != "New Name" {
			t.Errorf("expected name overridden, got %q", params.Name)
		}
		if len(params.Kinds) != 2 || params.Kinds[0] != myhome.RoomKindOffice {
			t.Errorf("expected kinds [office kitchen], got %v", params.Kinds)
		}
	})
}

// --- Pure function tests: pre-existing helpers ---

func TestMatchRoomPattern(t *testing.T) {
	rooms := myhome.TemperatureRoomList{
		"salon":         {RoomID: "salon"},
		"chambre-ado":   {RoomID: "chambre-ado"},
		"chambre-bebe":  {RoomID: "chambre-bebe"},
		"salle-de-bain": {RoomID: "salle-de-bain"},
	}

	tests := []struct {
		pattern string
		want    []string
	}{
		{"*", []string{"salon", "chambre-ado", "chambre-bebe", "salle-de-bain"}},
		{"chambre*", []string{"chambre-ado", "chambre-bebe"}},
		{"*bain", []string{"salle-de-bain"}},
		{"salon", []string{"salon"}},
		{"nope", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := matchRoomPattern(tt.pattern, rooms)
			if len(got) != len(tt.want) {
				t.Fatalf("pattern %q: expected %d matches, got %d (%v)", tt.pattern, len(tt.want), len(got), got)
			}
			for _, id := range tt.want {
				if _, ok := got[id]; !ok {
					t.Errorf("pattern %q: expected match for %q", tt.pattern, id)
				}
			}
		})
	}
}

func TestParseKinds(t *testing.T) {
	tests := []struct {
		in   string
		want []myhome.RoomKind
	}{
		{"", nil},
		{"bedroom", []myhome.RoomKind{myhome.RoomKindBedroom}},
		{"bedroom,office", []myhome.RoomKind{myhome.RoomKindBedroom, myhome.RoomKindOffice}},
		{" bedroom , office ", []myhome.RoomKind{myhome.RoomKindBedroom, myhome.RoomKindOffice}},
	}
	for _, tt := range tests {
		got := parseKinds(tt.in)
		if len(got) != len(tt.want) {
			t.Fatalf("parseKinds(%q): expected %v, got %v", tt.in, tt.want, got)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseKinds(%q)[%d]: expected %v, got %v", tt.in, i, tt.want[i], got[i])
			}
		}
	}
}

func TestFormatKinds(t *testing.T) {
	if got := formatKinds(nil); got != "(none)" {
		t.Errorf("expected '(none)', got %q", got)
	}
	if got := formatKinds([]myhome.RoomKind{myhome.RoomKindBedroom}); got != "bedroom" {
		t.Errorf("expected 'bedroom', got %q", got)
	}
	if got := formatKinds([]myhome.RoomKind{myhome.RoomKindBedroom, myhome.RoomKindOffice}); got != "bedroom, office" {
		t.Errorf("expected 'bedroom, office', got %q", got)
	}
}

func TestFormatLevels(t *testing.T) {
	if got := formatLevels(nil); got != "(none)" {
		t.Errorf("expected '(none)', got %q", got)
	}
	if got := formatLevels(map[string]float64{"eco": 17}); got != "eco:17.0" {
		t.Errorf("expected 'eco:17.0', got %q", got)
	}
	got := formatLevels(map[string]float64{"eco": 17, "comfort": 21})
	if !strings.Contains(got, "eco:17.0") || !strings.Contains(got, "comfort:21.0") {
		t.Errorf("expected both levels present, got %q", got)
	}
}

func TestParseScheduleString(t *testing.T) {
	if got := parseScheduleString(""); len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
	got := parseScheduleString("08:00-18:00, 20:00-22:00")
	want := []string{"08:00-18:00", "20:00-22:00"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestFormatTimeRanges(t *testing.T) {
	if got := formatTimeRanges(nil); got != "(always eco)" {
		t.Errorf("expected '(always eco)', got %q", got)
	}
	got := formatTimeRanges([]myhome.TemperatureTimeRange{{Start: 480, End: 1080}})
	if got != "08:00-18:00" {
		t.Errorf("expected '08:00-18:00', got %q", got)
	}
}

func TestFormatMinutes(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "00:00"},
		{60, "01:00"},
		{90, "01:30"},
		{1439, "23:59"},
	}
	for _, tt := range tests {
		if got := formatMinutes(tt.in); got != tt.want {
			t.Errorf("formatMinutes(%d): expected %q, got %q", tt.in, tt.want, got)
		}
	}
}

func TestFormatWeekday(t *testing.T) {
	if got := formatWeekday(0); got != "Sunday" {
		t.Errorf("expected 'Sunday', got %q", got)
	}
	if got := formatWeekday(6); got != "Saturday" {
		t.Errorf("expected 'Saturday', got %q", got)
	}
	if got := formatWeekday(7); !strings.Contains(got, "Invalid") {
		t.Errorf("expected an Invalid(...) marker, got %q", got)
	}
}

func TestGetDefaultDayType(t *testing.T) {
	weekend := map[int]bool{0: true, 6: true}
	for d := 0; d <= 6; d++ {
		got := getDefaultDayType(d)
		if weekend[d] && got != myhome.DayTypeDayOff {
			t.Errorf("day %d: expected day-off, got %v", d, got)
		}
		if !weekend[d] && got != myhome.DayTypeWorkDay {
			t.Errorf("day %d: expected work-day, got %v", d, got)
		}
	}
}

func TestContains(t *testing.T) {
	kinds := []myhome.RoomKind{myhome.RoomKindBedroom, myhome.RoomKindOffice}
	if !contains(kinds, myhome.RoomKindBedroom) {
		t.Error("expected bedroom to be found")
	}
	if contains(kinds, myhome.RoomKindKitchen) {
		t.Error("expected kitchen to be absent")
	}
}
