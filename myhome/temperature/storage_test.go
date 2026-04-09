package temperature

import (
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
)

func TestNewStorage_CreatesSchema(t *testing.T) {
	s := newTestStorage(t)
	// Tables should exist — verify by doing a query that would fail if absent.
	rooms, err := s.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms on fresh schema: %v", err)
	}
	if len(rooms) != 0 {
		t.Errorf("expected empty rooms on fresh DB, got %d", len(rooms))
	}
}

func TestSaveRoom_Insert(t *testing.T) {
	s := newTestStorage(t)
	config := &RoomConfig{
		ID:     "r1",
		Name:   "Living Room",
		Kinds:  []myhome.RoomKind{myhome.RoomKindLivingRoom},
		Levels: map[string]float64{"eco": 17.0, "comfort": 21.0},
	}
	modified, err := s.SaveRoom(config)
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	if !modified {
		t.Error("expected modified=true on first insert")
	}
}

func TestSaveRoom_SameData(t *testing.T) {
	s := newTestStorage(t)
	config := &RoomConfig{
		ID:     "r1",
		Name:   "Living Room",
		Kinds:  []myhome.RoomKind{myhome.RoomKindLivingRoom},
		Levels: map[string]float64{"eco": 17.0},
	}
	if _, err := s.SaveRoom(config); err != nil {
		t.Fatalf("first SaveRoom: %v", err)
	}
	modified, err := s.SaveRoom(config)
	if err != nil {
		t.Fatalf("second SaveRoom: %v", err)
	}
	if modified {
		t.Error("expected modified=false when data unchanged")
	}
}

func TestSaveRoom_UpdatedField(t *testing.T) {
	s := newTestStorage(t)
	config := &RoomConfig{
		ID:     "r1",
		Name:   "Old Name",
		Kinds:  []myhome.RoomKind{myhome.RoomKindLivingRoom},
		Levels: map[string]float64{"eco": 17.0},
	}
	if _, err := s.SaveRoom(config); err != nil {
		t.Fatalf("first SaveRoom: %v", err)
	}
	config.Name = "New Name"
	modified, err := s.SaveRoom(config)
	if err != nil {
		t.Fatalf("second SaveRoom: %v", err)
	}
	if !modified {
		t.Error("expected modified=true when name changed")
	}
}

func TestListRooms_Empty(t *testing.T) {
	s := newTestStorage(t)
	rooms, err := s.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 0 {
		t.Errorf("expected 0 rooms, got %d", len(rooms))
	}
}

func TestListRooms_Populated(t *testing.T) {
	s := newTestStorage(t)
	for _, id := range []string{"r1", "r2"} {
		_, err := s.SaveRoom(&RoomConfig{
			ID:     id,
			Name:   "Room " + id,
			Kinds:  []myhome.RoomKind{myhome.RoomKindOther},
			Levels: map[string]float64{"eco": 17.0},
		})
		if err != nil {
			t.Fatalf("SaveRoom %s: %v", id, err)
		}
	}
	rooms, err := s.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Errorf("expected 2 rooms, got %d", len(rooms))
	}
}

func TestSaveKindSchedule_InsertAndRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	ranges := []myhome.TemperatureTimeRange{{Start: 360, End: 1380}}
	modified, err := s.SetKindSchedule(myhome.RoomKindBedroom, myhome.DayTypeWorkDay, ranges)
	if err != nil {
		t.Fatalf("SetKindSchedule: %v", err)
	}
	if !modified {
		t.Error("expected modified=true on first insert")
	}

	got, err := s.GetKindSchedules(nil, nil)
	if err != nil {
		t.Fatalf("GetKindSchedules: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(got))
	}
	if got[0].Kind != myhome.RoomKindBedroom || got[0].DayType != myhome.DayTypeWorkDay {
		t.Errorf("unexpected schedule metadata: %+v", got[0])
	}
	if len(got[0].Ranges) != 1 || got[0].Ranges[0].Start != 360 || got[0].Ranges[0].End != 1380 {
		t.Errorf("unexpected ranges: %v", got[0].Ranges)
	}
}

func TestSaveKindSchedule_Unchanged(t *testing.T) {
	s := newTestStorage(t)
	ranges := []myhome.TemperatureTimeRange{{Start: 360, End: 1380}}
	if _, err := s.SetKindSchedule(myhome.RoomKindBedroom, myhome.DayTypeWorkDay, ranges); err != nil {
		t.Fatalf("first SetKindSchedule: %v", err)
	}
	modified, err := s.SetKindSchedule(myhome.RoomKindBedroom, myhome.DayTypeWorkDay, ranges)
	if err != nil {
		t.Fatalf("second SetKindSchedule: %v", err)
	}
	if modified {
		t.Error("expected modified=false when data unchanged")
	}
}

func TestGetKindSchedules_Filter(t *testing.T) {
	s := newTestStorage(t)
	s.SetKindSchedule(myhome.RoomKindBedroom, myhome.DayTypeWorkDay, []myhome.TemperatureTimeRange{{Start: 360, End: 1380}})
	s.SetKindSchedule(myhome.RoomKindOffice, myhome.DayTypeDayOff, []myhome.TemperatureTimeRange{{Start: 540, End: 1080}})

	// Filter by kind.
	kind := myhome.RoomKindBedroom
	got, err := s.GetKindSchedules(&kind, nil)
	if err != nil {
		t.Fatalf("GetKindSchedules by kind: %v", err)
	}
	if len(got) != 1 || got[0].Kind != myhome.RoomKindBedroom {
		t.Errorf("expected bedroom schedule, got %v", got)
	}

	// Filter by day type.
	dt := myhome.DayTypeDayOff
	got, err = s.GetKindSchedules(nil, &dt)
	if err != nil {
		t.Fatalf("GetKindSchedules by day_type: %v", err)
	}
	if len(got) != 1 || got[0].DayType != myhome.DayTypeDayOff {
		t.Errorf("expected day-off schedule, got %v", got)
	}
}

func TestSaveWeekdayDefault(t *testing.T) {
	s := newTestStorage(t)
	modified, err := s.SetWeekdayDefault(1 /*Monday*/, myhome.DayTypeWorkDay)
	if err != nil {
		t.Fatalf("SetWeekdayDefault: %v", err)
	}
	if !modified {
		t.Error("expected modified=true on insert")
	}
}

func TestGetWeekdayDefaults(t *testing.T) {
	s := newTestStorage(t)
	for weekday := 0; weekday <= 6; weekday++ {
		dt := myhome.DayTypeWorkDay
		if weekday == 0 || weekday == 6 { // Sun or Sat
			dt = myhome.DayTypeDayOff
		}
		if _, err := s.SetWeekdayDefault(weekday, dt); err != nil {
			t.Fatalf("SetWeekdayDefault(%d): %v", weekday, err)
		}
	}

	defaults, err := s.GetWeekdayDefaults()
	if err != nil {
		t.Fatalf("GetWeekdayDefaults: %v", err)
	}
	if len(defaults) != 7 {
		t.Errorf("expected 7 defaults, got %d", len(defaults))
	}
	if defaults[0] != myhome.DayTypeDayOff {
		t.Errorf("Sunday (0): got %v, want day-off", defaults[0])
	}
	if defaults[1] != myhome.DayTypeWorkDay {
		t.Errorf("Monday (1): got %v, want work-day", defaults[1])
	}
	if defaults[6] != myhome.DayTypeDayOff {
		t.Errorf("Saturday (6): got %v, want day-off", defaults[6])
	}
}
