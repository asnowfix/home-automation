package rooms

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// seedRoom is a test helper that saves a room to storage.
func seedRoom(t *testing.T, svc *Service, id, name string) *RoomConfig {
	t.Helper()
	config := &RoomConfig{
		ID:     id,
		Name:   name,
		Kinds:  []myhome.RoomKind{myhome.RoomKindOther},
		Levels: map[string]float64{"eco": 17.0, "comfort": 21.0},
	}
	if _, err := svc.storage.SaveRoom(config); err != nil {
		t.Fatalf("seedRoom SaveRoom: %v", err)
	}
	return config
}

// --- HandleSet ---

func TestHandleSet_MissingRoomID(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.HandleSet(context.Background(), &myhome.TemperatureSetParams{
		Name:   "Test",
		Kinds:  []myhome.RoomKind{myhome.RoomKindOther},
		Levels: map[string]float64{"eco": 17.0},
	})
	if err == nil {
		t.Error("expected error for missing room_id")
	}
}

func TestHandleSet_MissingEcoLevel(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.HandleSet(context.Background(), &myhome.TemperatureSetParams{
		RoomID: "r1",
		Name:   "Test",
		Kinds:  []myhome.RoomKind{myhome.RoomKindOther},
		Levels: map[string]float64{"comfort": 21.0}, // no "eco"
	})
	if err == nil {
		t.Error("expected error for missing 'eco' level")
	}
}

func TestHandleSet_Valid(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.HandleSet(context.Background(), &myhome.TemperatureSetParams{
		RoomID: "r1",
		Name:   "Living Room",
		Kinds:  []myhome.RoomKind{myhome.RoomKindLivingRoom},
		Levels: map[string]float64{"eco": 17.0, "comfort": 21.0},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status: got %q, want \"ok\"", result.Status)
	}
	_, err = svc.storage.GetRoom("r1")
	if err != nil {
		t.Error("expected room to be stored in storage")
	}
}

// --- HandleGet ---

func TestHandleGet_Found(t *testing.T) {
	svc, _ := newTestService(t)
	seedRoom(t, svc, "r1", "Test Room")

	result, err := svc.HandleGet(context.Background(), &myhome.TemperatureGetParams{RoomID: "r1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoomID != "r1" {
		t.Errorf("room_id: got %q, want \"r1\"", result.RoomID)
	}
	if result.Name != "Test Room" {
		t.Errorf("name: got %q, want \"Test Room\"", result.Name)
	}
}

func TestHandleGet_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.HandleGet(context.Background(), &myhome.TemperatureGetParams{RoomID: "nope"})
	if err == nil {
		t.Error("expected error for unknown room, got nil")
	}
}

// --- HandleList ---

func TestHandleList_Empty(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.HandleList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*result) != 0 {
		t.Errorf("expected empty list, got %d rooms", len(*result))
	}
}

func TestHandleList_Populated(t *testing.T) {
	svc, _ := newTestService(t)
	seedRoom(t, svc, "r1", "Room 1")
	seedRoom(t, svc, "r2", "Room 2")

	result, err := svc.HandleList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*result) != 2 {
		t.Errorf("expected 2 rooms, got %d", len(*result))
	}
}

// --- HandleDelete ---

func TestHandleDelete_Found(t *testing.T) {
	svc, _ := newTestService(t)
	seedRoom(t, svc, "r1", "Room 1")

	result, err := svc.HandleDelete(context.Background(), &myhome.TemperatureDeleteParams{RoomID: "r1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status: got %q, want \"ok\"", result.Status)
	}
	_, err = svc.storage.GetRoom("r1")
	if err == nil {
		t.Error("expected room to be removed from storage")
	}
}

func TestHandleDelete_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.HandleDelete(context.Background(), &myhome.TemperatureDeleteParams{RoomID: "nope"})
	if err == nil {
		t.Error("expected error for unknown room, got nil")
	}
}

// --- HandleRoomCreate ---

func TestHandleRoomCreate(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.HandleRoomCreate(context.Background(), &myhome.RoomCreateParams{
		ID:   "r1",
		Name: "My Room",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got message: %q", result.Message)
	}
	stored, err := svc.storage.GetRoom("r1")
	if err != nil {
		t.Fatalf("room should be in storage: %v", err)
	}
	if stored.Name != "My Room" {
		t.Errorf("name: got %q, want \"My Room\"", stored.Name)
	}
}

func TestHandleRoomCreate_DefaultName(t *testing.T) {
	svc, _ := newTestService(t)
	// When Name is empty, ID is used as the name.
	result, err := svc.HandleRoomCreate(context.Background(), &myhome.RoomCreateParams{ID: "bedroom"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true")
	}
	room, err := svc.storage.GetRoom("bedroom")
	if err != nil {
		t.Fatalf("room not in storage: %v", err)
	}
	if room.Name != "bedroom" {
		t.Errorf("name: got %q, want \"bedroom\"", room.Name)
	}
}

func TestHandleRoomCreate_AlreadyExists(t *testing.T) {
	svc, _ := newTestService(t)
	seedRoom(t, svc, "r1", "Existing")

	result, err := svc.HandleRoomCreate(context.Background(), &myhome.RoomCreateParams{ID: "r1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected success=false for duplicate room")
	}
}

// --- HandleRoomEdit ---

func TestHandleRoomEdit(t *testing.T) {
	svc, _ := newTestService(t)
	seedRoom(t, svc, "r1", "Old Name")

	newName := "New Name"
	result, err := svc.HandleRoomEdit(context.Background(), &myhome.RoomEditParams{
		ID:   "r1",
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got message: %q", result.Message)
	}
	stored, err := svc.storage.GetRoom("r1")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if stored.Name != "New Name" {
		t.Errorf("stored name: got %q, want \"New Name\"", stored.Name)
	}
}

func TestHandleRoomEdit_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	newName := "Whatever"
	result, err := svc.HandleRoomEdit(context.Background(), &myhome.RoomEditParams{
		ID:   "nonexistent",
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected success=false for nonexistent room")
	}
}

// --- HandleRoomDelete ---

func TestHandleRoomDelete(t *testing.T) {
	svc, _ := newTestService(t)
	seedRoom(t, svc, "r1", "Room 1")

	result, err := svc.HandleRoomDelete(context.Background(), &myhome.RoomDeleteParams{ID: "r1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got: %q", result.Message)
	}
	_, err = svc.storage.GetRoom("r1")
	if err == nil {
		t.Error("room should be removed from storage")
	}
}

func TestHandleRoomDelete_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.HandleRoomDelete(context.Background(), &myhome.RoomDeleteParams{ID: "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected success=false for nonexistent room")
	}
}

// --- HandleSetKindSchedule ---

func TestHandleSetKindSchedule(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.HandleSetKindSchedule(context.Background(), &myhome.TemperatureSetKindScheduleParams{
		Kind:    myhome.RoomKindBedroom,
		DayType: myhome.DayTypeWorkDay,
		Ranges:  []string{"06:00-23:00"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status: got %q, want \"ok\"", result.Status)
	}
	// Verify schedule was persisted to storage.
	kind := myhome.RoomKindBedroom
	dayType := myhome.DayTypeWorkDay
	schedules, err := svc.storage.GetKindSchedules(&kind, &dayType)
	if err != nil || len(schedules) == 0 {
		t.Errorf("expected schedule in storage: err=%v schedules=%d", err, len(schedules))
	}
}

func TestHandleSetKindSchedule_InvalidKind(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.HandleSetKindSchedule(context.Background(), &myhome.TemperatureSetKindScheduleParams{
		Kind:    "invalid-kind",
		DayType: myhome.DayTypeWorkDay,
		Ranges:  []string{"06:00-23:00"},
	})
	if err == nil {
		t.Error("expected error for invalid kind")
	}
}

// --- HandleGetKindSchedules ---

func TestHandleGetKindSchedules(t *testing.T) {
	svc, _ := newTestService(t)
	// Seed directly via storage.
	svc.storage.SetKindSchedule(myhome.RoomKindBedroom, myhome.DayTypeWorkDay, []myhome.TemperatureTimeRange{{Start: 360, End: 1380}})

	result, err := svc.HandleGetKindSchedules(context.Background(), &myhome.TemperatureGetKindSchedulesParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*result) != 1 {
		t.Errorf("expected 1 schedule, got %d", len(*result))
	}
}

// --- HandleSetWeekdayDefault ---

func TestHandleSetWeekdayDefault(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.HandleSetWeekdayDefault(context.Background(), &myhome.TemperatureSetWeekdayDefaultParams{
		Weekday: 1, // Monday
		DayType: myhome.DayTypeDayOff,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Weekday != 1 || result.DayType != myhome.DayTypeDayOff {
		t.Errorf("unexpected result: %+v", result)
	}
	// Verify persistence.
	defaults, err := svc.storage.GetWeekdayDefaults()
	if err != nil {
		t.Fatalf("GetWeekdayDefaults: %v", err)
	}
	if defaults[1] != myhome.DayTypeDayOff {
		t.Errorf("storage weekday 1: got %v, want day-off", defaults[1])
	}
}

func TestHandleSetWeekdayDefault_InvalidWeekday(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.HandleSetWeekdayDefault(context.Background(), &myhome.TemperatureSetWeekdayDefaultParams{
		Weekday: 7, // out of range
		DayType: myhome.DayTypeWorkDay,
	})
	if err == nil {
		t.Error("expected error for weekday=7")
	}
}

// --- PublishRangesUpdate ---

func TestPublishRangesUpdate(t *testing.T) {
	svc, mc := newTestService(t)
	ctx := context.Background()

	// Set up room and kind schedule in storage.
	svc.storage.SaveRoom(&RoomConfig{
		ID:     "r1",
		Name:   "Room 1",
		Kinds:  []myhome.RoomKind{myhome.RoomKindBedroom},
		Levels: map[string]float64{"eco": 17.0, "comfort": 21.0},
	})
	svc.storage.SetKindSchedule(myhome.RoomKindBedroom, myhome.DayTypeWorkDay, []myhome.TemperatureTimeRange{{Start: 360, End: 1380}})

	if err := svc.PublishRangesUpdate(ctx, "r1"); err != nil {
		t.Fatalf("PublishRangesUpdate: %v", err)
	}

	topic := "myhome/rooms/r1/temperature/ranges"
	payloads := mc.Published(topic)
	if len(payloads) == 0 {
		t.Fatalf("no messages published to %q", topic)
	}

	// Payload should be valid JSON with expected room_id.
	var payload map[string]interface{}
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v\npayload: %s", err, payloads[0])
	}
	if payload["room_id"] != "r1" {
		t.Errorf("room_id: got %v, want \"r1\"", payload["room_id"])
	}
}

func TestPublishRangesUpdate_RoomNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	err := svc.PublishRangesUpdate(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown room, got nil")
	}
}
