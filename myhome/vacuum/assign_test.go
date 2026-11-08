package vacuum

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

// restoreRoomsVerb puts temperature.get back as the test found it (the method
// registry is package-level state: no t.Parallel in this file).
func restoreRoomsVerb(t *testing.T) {
	t.Helper()
	prev, _ := myhome.Methods(myhome.TemperatureGet)
	t.Cleanup(func() {
		if prev != nil {
			myhome.RegisterMethodHandler(myhome.TemperatureGet, prev.ActionE)
		} else {
			myhome.UnregisterMethodHandler(myhome.TemperatureGet)
		}
	})
}

// fakeRooms registers a temperature.get that knows only the given rooms.
func fakeRooms(t *testing.T, known ...string) {
	t.Helper()
	restoreRoomsVerb(t)
	myhome.RegisterMethodHandler(myhome.TemperatureGet, func(_ context.Context, in any) (any, error) {
		id := in.(*myhome.TemperatureGetParams).RoomID
		for _, k := range known {
			if k == id {
				return &myhome.TemperatureRoomConfig{RoomID: id}, nil
			}
		}
		return nil, fmt.Errorf("room not found: %s", id)
	})
}

// noRoomsService leaves temperature.get unregistered, as on a daemon without rooms.
func noRoomsService(t *testing.T) {
	t.Helper()
	restoreRoomsVerb(t)
	myhome.UnregisterMethodHandler(myhome.TemperatureGet)
}

func assignHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: "pw"}, &robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"})
	for _, r := range []regionRecord{
		{BLID: blid, MapID: "m1", MapVersion: "v1", RegionID: "11", RegionType: "rid"},
		{BLID: blid, MapID: "m1", MapVersion: "v1", RegionID: "4", RegionType: "rid"},
	} {
		if err := h.store.learnRegion(r); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func roomOf(t *testing.T, h *harness, mapID, id string) string {
	t.Helper()
	res, err := h.svc.Rooms("")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Regions {
		if r.ID == id && r.MapID == mapID {
			return r.RoomID
		}
	}
	t.Fatalf("region %s/%s not listed", mapID, id)
	return ""
}

func TestAssignMoveUnassign(t *testing.T) {
	fakeRooms(t, "salon", "cuisine")
	h := assignHarness(t)
	ctx := context.Background()

	res, err := h.svc.AssignRoom(ctx, "", "", "11", "salon")
	if err != nil || res.Previous != "" || res.RoomID != "salon" || res.MapID != "m1" {
		t.Fatalf("assign = %+v, %v", res, err)
	}
	// Many areas may share a room.
	if _, err := h.svc.AssignRoom(ctx, "", "", "4", "salon"); err != nil {
		t.Fatal(err)
	}
	// Moving reports the old room, and the area is in exactly one room.
	res, err = h.svc.AssignRoom(ctx, "", "", "11", "cuisine")
	if err != nil || res.Previous != "salon" {
		t.Fatalf("move = %+v, %v", res, err)
	}
	if got := roomOf(t, h, "m1", "11"); got != "cuisine" {
		t.Errorf("region 11 room = %q", got)
	}
	if got := roomOf(t, h, "m1", "4"); got != "salon" {
		t.Errorf("region 4 room = %q (moving 11 must not touch it)", got)
	}
	res, err = h.svc.AssignRoom(ctx, "", "", "11", "")
	if err != nil || res.Previous != "cuisine" || res.RoomID != "" {
		t.Fatalf("unassign = %+v, %v", res, err)
	}
	if got := roomOf(t, h, "m1", "11"); got != "" {
		t.Errorf("region 11 room after unassign = %q", got)
	}
}

func TestAssignRejections(t *testing.T) {
	fakeRooms(t, "salon")
	h := assignHarness(t)
	ctx := context.Background()

	if _, err := h.svc.AssignRoom(ctx, "", "", "11", "nowhere"); err == nil || !strings.Contains(err.Error(), `unknown room "nowhere"`) {
		t.Errorf("unknown room: %v", err)
	}
	if got := roomOf(t, h, "m1", "11"); got != "" {
		t.Errorf("a refused assignment wrote %q", got)
	}
	if _, err := h.svc.AssignRoom(ctx, "", "", "99", "salon"); err == nil || !strings.Contains(err.Error(), `no region "99"`) {
		t.Errorf("unknown region: %v", err)
	}
	if _, err := h.svc.AssignRoom(ctx, "", "m9", "11", "salon"); err == nil || !strings.Contains(err.Error(), `on map "m9"`) {
		t.Errorf("unknown map: %v", err)
	}
	if _, err := h.svc.AssignRoom(ctx, "nobody", "", "11", "salon"); err == nil {
		t.Error("unknown robot accepted")
	}

	noRoomsService(t)
	if _, err := h.svc.AssignRoom(ctx, "", "", "11", "salon"); err == nil || !strings.Contains(err.Error(), "rooms are not enabled") {
		t.Errorf("no rooms service: %v", err)
	}
	// Unassigning needs no rooms service.
	if _, err := h.svc.AssignRoom(ctx, "", "", "11", ""); err != nil {
		t.Errorf("unassign without rooms service: %v", err)
	}
}

// Region ids are unique per map, not across maps (the primary key is
// blid, pmap_id, region_id), so the same id may exist on two maps. Whether the
// robot really does that is an open question on #684; the mapping must work if so.
func TestAssignRegionIdOnTwoMaps(t *testing.T) {
	fakeRooms(t, "salon", "etage")
	h := assignHarness(t)
	ctx := context.Background()
	if err := h.store.learnRegion(regionRecord{BLID: blid, MapID: "m2", MapVersion: "v9", RegionID: "11", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}

	_, err := h.svc.AssignRoom(ctx, "", "", "11", "salon")
	if err == nil || !strings.Contains(err.Error(), "--map") || !strings.Contains(err.Error(), "2 robot maps") {
		t.Fatalf("ambiguous id: %v", err)
	}
	if roomOf(t, h, "m1", "11") != "" || roomOf(t, h, "m2", "11") != "" {
		t.Error("an ambiguous assignment wrote something")
	}
	if _, err := h.svc.AssignRoom(ctx, "", "m2", "11", "etage"); err != nil {
		t.Fatal(err)
	}
	if roomOf(t, h, "m1", "11") != "" || roomOf(t, h, "m2", "11") != "etage" {
		t.Errorf("--map m2 assigned m1=%q m2=%q", roomOf(t, h, "m1", "11"), roomOf(t, h, "m2", "11"))
	}

	// The same id on two cloud maps, one of which the cloud then drops: the
	// single non-stale region is chosen without --map.
	both := []regionRecord{{MapID: "m3", MapVersion: "c", RegionID: "30", RegionType: "rid"}, {MapID: "m4", MapVersion: "c", RegionID: "30", RegionType: "rid"}}
	if _, err := h.store.syncCloud(blid, both); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AssignRoom(ctx, "", "", "30", "salon"); err == nil {
		t.Error("two current regions with one id must stay ambiguous")
	}
	if _, err := h.store.syncCloud(blid, both[1:]); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.AssignRoom(ctx, "", "", "30", "salon")
	if err != nil || res.MapID != "m4" {
		t.Fatalf("single fresh candidate: %+v, %v", res, err)
	}
}

// A stale region may be assigned: the owner may be repairing a mapping.
func TestAssignStaleRegion(t *testing.T) {
	fakeRooms(t, "salon")
	h := assignHarness(t)
	if _, err := h.store.syncCloud(blid, []regionRecord{{MapID: "m5", MapVersion: "c", RegionID: "7", RegionType: "rid"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.syncCloud(blid, nil); err != nil { // 7 is no longer listed: stale
		t.Fatal(err)
	}
	if !regionByID(t, h, "7").Stale {
		t.Fatal("region 7 should be stale")
	}
	if _, err := h.svc.AssignRoom(context.Background(), "", "", "7", "salon"); err != nil {
		t.Errorf("assigning a stale region: %v", err)
	}
}

// Neither a cloud sync (rename, removal) nor learning a region again may
// forget a mapping.
func TestRoomIDSurvivesSyncAndLearn(t *testing.T) {
	s := newDumpStorage(t)
	seed(t, s) // learned 3 and cloud 7 on pm1, robot BLID1
	for _, id := range []string{"3", "7"} {
		if _, err := s.assignRegion("BLID1", "pm1", id, "salon"); err != nil {
			t.Fatal(err)
		}
	}
	want := func(step string) {
		t.Helper()
		recs, err := s.regions("BLID1")
		if err != nil || len(recs) != 2 {
			t.Fatalf("%s: %+v, %v", step, recs, err)
		}
		for _, r := range recs {
			if r.RoomID != "salon" {
				t.Errorf("%s: region %s room = %q, want salon", step, r.RegionID, r.RoomID)
			}
		}
	}
	// A cloud rename of 7, and 3 (learned) taken over by the cloud.
	if _, err := s.syncCloud("BLID1", []regionRecord{
		{MapID: "pm1", MapVersion: "v3", RegionID: "7", RegionType: "rid", Name: "Cuisine"},
		{MapID: "pm1", MapVersion: "v3", RegionID: "3", RegionType: "rid", Name: "Salon"},
	}); err != nil {
		t.Fatal(err)
	}
	want("rename")
	// 7 disappears from the cloud: stale, mapping kept.
	if _, err := s.syncCloud("BLID1", []regionRecord{{MapID: "pm1", MapVersion: "v3", RegionID: "3", RegionType: "rid", Name: "Salon"}}); err != nil {
		t.Fatal(err)
	}
	want("removal")
	if err := s.learnRegion(regionRecord{BLID: "BLID1", MapID: "pm1", MapVersion: "v4", RegionID: "3", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}
	want("learn")
}

func TestMigrationAddsRoomID(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	// vacuum_regions as the #628 builds created it, before room_id.
	if _, err := db.Exec(`
	CREATE TABLE vacuum_regions (
		blid TEXT NOT NULL, pmap_id TEXT NOT NULL, pmap_version TEXT NOT NULL,
		region_id TEXT NOT NULL, region_type TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'robot', map_name TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
		removed INTEGER NOT NULL DEFAULT 0, synced_at TIMESTAMP,
		PRIMARY KEY (blid, pmap_id, region_id));
	INSERT INTO vacuum_regions (blid, pmap_id, pmap_version, region_id, region_type, name) VALUES ('` + blid + `', 'm1', 'v1', '11', 'rid', 'Kitchen');`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStorage(logr.Discard(), db)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := store.regions(blid)
	if err != nil || len(recs) != 1 || recs[0].Name != "Kitchen" || recs[0].RoomID != "" {
		t.Fatalf("regions after migration = %+v, %v", recs, err)
	}
	if _, err := NewStorage(logr.Discard(), db); err != nil { // idempotent
		t.Fatal(err)
	}
}
