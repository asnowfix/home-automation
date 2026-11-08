package rooms

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/go-logr/logr"
)

// A storage error other than "not found" must fail temperature.set instead of
// saving the room with an empty (wiped) iCal URL.
func TestTemperatureSetStorageErrorDoesNotWipeICal(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	ical := "https://example.invalid/cal.ics"
	base := myhome.TemperatureSetParams{RoomID: "r", Name: "R", Kinds: []myhome.RoomKind{myhome.RoomKindOther}, Levels: map[string]float64{"eco": 17}}
	withURL := base
	withURL.ICalURL = &ical
	if _, err := svc.HandleSet(ctx, &withURL); err != nil {
		t.Fatal(err)
	}
	// Corrupt the stored row so that GetRoom fails with something other than not-found.
	if _, err := svc.storage.db.Exec(`UPDATE rooms SET kinds = 'not json' WHERE room_id = 'r'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.storage.GetRoom("r"); err == nil || errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("test setup: GetRoom error = %v, want a non-not-found error", err)
	}
	if _, err := svc.HandleSet(ctx, &base); err == nil {
		t.Fatal("set succeeded although the existing room could not be read")
	}
	var got string
	if err := svc.storage.db.Get(&got, `SELECT ical_url FROM rooms WHERE room_id = 'r'`); err != nil || got != ical {
		t.Errorf("stored iCal URL = %q (%v), want it untouched", got, err)
	}
}

// The data service backs the room/schedule RPCs of a daemon with the
// temperature service disabled: it imports the whole rooms section and
// publishes nothing, whereas the regular service publishes retained ranges
// that heaters consume (same import, so the comparison is meaningful).
func TestDataServiceImportsWithoutPublishing(t *testing.T) {
	ctx := context.Background()
	file := `{"rooms":{"kitchen":{"room_id":"kitchen","name":"Kitchen","kinds":["kitchen"],"levels":{"eco":17,"comfort":21},"ical_url":"https://example.invalid/c.ics"}},
	  "weekday_defaults":{"6":"day-off"},
	  "kind_schedules":[{"kind":"kitchen","day_type":"work-day","ranges":[{"start":390,"end":480}]}]}`
	in, err := myhome.ParseDatabaseImport([]byte(file))
	if err != nil {
		t.Fatal(err)
	}

	data := NewDataService(logr.Discard(), newTestStorage(t))
	n, err := myhome.ImportDatabase(ctx, serviceClient{data}, in, false, io.Discard)
	if err != nil || n.Rooms != 1 || n.KindSchedules != 1 || n.WeekdayDefaults != 1 {
		t.Fatalf("data service import: %+v, %v", n, err)
	}
	if r, err := data.storage.GetRoom("kitchen"); err != nil || r.ICalURL == "" {
		t.Errorf("room not stored with its iCal URL: %+v, %v", r, err)
	}
	// Room edits publish in the regular service; here they must just work.
	name := "Cuisine"
	if res, err := data.HandleRoomEdit(ctx, &myhome.RoomEditParams{ID: "kitchen", Name: &name}); err != nil || !res.Success {
		t.Errorf("room.edit on data service: %+v, %v", res, err)
	}

	// Control: the regular service, same import, does publish.
	full, mc := newTestService(t)
	if _, err := myhome.ImportDatabase(ctx, serviceClient{full}, in, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(mc.Published("myhome/rooms/kitchen/temperature/ranges")) == 0 {
		t.Error("control failed: the regular service published nothing, so this test proves nothing")
	}
}
