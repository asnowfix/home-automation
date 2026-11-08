package rooms

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/devices"
)

// serviceClient is a myhome.Client whose RPCs are answered by a rooms Service,
// with parameters and results sent through JSON like the real transport.
type serviceClient struct{ svc *Service }

func (c serviceClient) LookupDevices(context.Context, string) (*[]devices.Device, error) {
	return nil, nil
}
func (c serviceClient) ForgetDevices(context.Context, string) error { return nil }
func (c serviceClient) CallE(ctx context.Context, v myhome.Verb, in any) (any, error) {
	b, _ := json.Marshal(in)
	switch v {
	case myhome.TemperatureList:
		return c.svc.HandleList(ctx)
	case myhome.TemperatureSet:
		var p myhome.TemperatureSetParams
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		return c.svc.HandleSet(ctx, &p)
	case myhome.TemperatureGetWeekdayDefaults:
		return c.svc.HandleGetWeekdayDefaults(ctx, &myhome.TemperatureGetWeekdayDefaultsParams{})
	case myhome.TemperatureSetWeekdayDefault:
		var p myhome.TemperatureSetWeekdayDefaultParams
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		return c.svc.HandleSetWeekdayDefault(ctx, &p)
	case myhome.TemperatureGetKindSchedules:
		return c.svc.HandleGetKindSchedules(ctx, &myhome.TemperatureGetKindSchedulesParams{})
	case myhome.TemperatureSetKindSchedule:
		var p myhome.TemperatureSetKindScheduleParams
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		return c.svc.HandleSetKindSchedule(ctx, &p)
	}
	return nil, io.ErrUnexpectedEOF // vacuum.* and others: not served here
}

// Export from one populated service, import into an empty one: rooms (with
// their iCal URL), weekday defaults and kind schedules all arrive.
func TestRoomsExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	src, _ := newTestService(t)
	ical := "https://example.invalid/cal.ics"
	if _, err := src.HandleSet(ctx, &myhome.TemperatureSetParams{
		RoomID: "kitchen", Name: "Kitchen", Kinds: []myhome.RoomKind{myhome.RoomKindKitchen},
		Levels: map[string]float64{"eco": 17, "comfort": 21}, ICalURL: &ical,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := src.HandleSetWeekdayDefault(ctx, &myhome.TemperatureSetWeekdayDefaultParams{Weekday: 6, DayType: myhome.DayTypeDayOff}); err != nil {
		t.Fatal(err)
	}
	if _, err := src.HandleSetKindSchedule(ctx, &myhome.TemperatureSetKindScheduleParams{
		Kind: myhome.RoomKindKitchen, DayType: myhome.DayTypeWorkDay, Ranges: []string{"06:30-08:00", "18:00-22:00"},
	}); err != nil {
		t.Fatal(err)
	}

	exp, err := myhome.ExportDatabase(ctx, serviceClient{src}, "*", false)
	if err != nil {
		t.Fatal(err)
	}
	file, err := json.Marshal(exp)
	if err != nil {
		t.Fatal(err)
	}
	in, err := myhome.ParseDatabaseImport(file)
	if err != nil {
		t.Fatal(err)
	}

	dst, _ := newTestService(t)
	n, err := myhome.ImportDatabase(ctx, serviceClient{dst}, in, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if n.Rooms != 1 || n.WeekdayDefaults < 1 || n.KindSchedules < 1 {
		t.Errorf("counts = %+v", n)
	}
	room, err := dst.storage.GetRoom("kitchen")
	if err != nil {
		t.Fatal(err)
	}
	if room.ICalURL != ical || room.Name != "Kitchen" || room.Levels["comfort"] != 21 {
		t.Errorf("room after import = %+v (iCal URL must survive)", room)
	}
	if d, _ := dst.storage.GetWeekdayDefault(6); d != myhome.DayTypeDayOff {
		t.Errorf("weekday default 6 = %q", d)
	}
	kind, day := myhome.RoomKindKitchen, myhome.DayTypeWorkDay
	if ks, _ := dst.storage.GetKindSchedules(&kind, &day); len(ks) != 1 || len(ks[0].Ranges) != 2 {
		t.Errorf("kind schedules = %+v", ks)
	}
}

// A caller that does not know ical_url (nil) must not wipe the stored one.
func TestTemperatureSetWithoutICalKeepsStoredURL(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	ical := "https://example.invalid/cal.ics"
	base := myhome.TemperatureSetParams{RoomID: "r", Name: "R", Kinds: []myhome.RoomKind{myhome.RoomKindOther}, Levels: map[string]float64{"eco": 17}}
	withURL := base
	withURL.ICalURL = &ical
	if _, err := svc.HandleSet(ctx, &withURL); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.HandleSet(ctx, &base); err != nil {
		t.Fatal(err)
	}
	if r, _ := svc.storage.GetRoom("r"); r.ICalURL != ical {
		t.Errorf("iCal URL wiped by a set without it: %q", r.ICalURL)
	}
	empty := ""
	base.ICalURL = &empty
	if _, err := svc.HandleSet(ctx, &base); err != nil {
		t.Fatal(err)
	}
	if r, _ := svc.storage.GetRoom("r"); r.ICalURL != "" {
		t.Errorf("explicit empty did not clear: %q", r.ICalURL)
	}
}
