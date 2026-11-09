package vacuum

import (
	"context"
	"errors"
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
		if id == "broken" {
			return nil, errors.New("database is locked")
		}
		return nil, fmt.Errorf("%w: %s", myhome.ErrRoomNotFound, id)
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

// assignID assigns by the robot's region id.
func assignID(h *harness, ctx context.Context, sel, mapID, regionID, roomID string) (*myhome.VacuumRoomAssignResult, error) {
	return h.svc.AssignRoom(ctx, &myhome.VacuumRoomAssignParams{Robot: sel, MapID: mapID, RegionID: regionID, RoomID: roomID})
}

// assignName assigns by the area's human-friendly name.
func assignName(h *harness, ctx context.Context, mapID, name, roomID string) (*myhome.VacuumRoomAssignResult, error) {
	return h.svc.AssignRoom(ctx, &myhome.VacuumRoomAssignParams{MapID: mapID, RegionName: name, RoomID: roomID})
}

func TestAssignByName(t *testing.T) {
	fakeRooms(t, "salon", "cuisine")
	h := assignHarness(t)
	ctx := context.Background()
	if _, err := h.store.nameRegion(blid, "11", "Cuisine", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.nameRegion(blid, "4", "Hall", false); err != nil {
		t.Fatal(err)
	}

	res, err := assignName(h, ctx, "", "  cuisine ", "cuisine") // case and spaces do not matter
	if err != nil || res.RegionID != "11" || res.RegionName != "Cuisine" || res.Previous != "" {
		t.Fatalf("assign by name = %+v, %v", res, err)
	}
	if roomOf(t, h, "m1", "11") != "cuisine" || roomOf(t, h, "m1", "4") != "" {
		t.Error("wrong area assigned")
	}
	res, err = assignName(h, ctx, "", "Cuisine", "salon")
	if err != nil || res.Previous != "cuisine" {
		t.Fatalf("move by name = %+v, %v", res, err)
	}
	if res, err = assignName(h, ctx, "", "Cuisine", ""); err != nil || res.Previous != "salon" {
		t.Fatalf("unassign by name = %+v, %v", res, err)
	}

	if _, err := assignName(h, ctx, "", "Garage", "salon"); err == nil || !strings.Contains(err.Error(), `no area "Garage"`) {
		t.Errorf("unknown name: %v", err)
	}
	// A name is not an id: "11" is not the name of anything.
	if _, err := assignName(h, ctx, "", "11", "salon"); err == nil {
		t.Error("an id was accepted as a name")
	}
	for _, p := range []myhome.VacuumRoomAssignParams{
		{RoomID: "salon"},
		{RegionName: "Cuisine", RegionID: "11", RoomID: "salon"},
	} {
		if _, err := h.svc.AssignRoom(ctx, &p); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("params %+v: %v", p, err)
		}
	}
}

// The same name on two maps needs --map; two areas of one map with one name
// cannot be told apart and must be renamed.
func TestAssignByNameAmbiguous(t *testing.T) {
	fakeRooms(t, "salon")
	h := assignHarness(t)
	ctx := context.Background()
	if err := h.store.learnRegion(regionRecord{BLID: blid, MapID: "m2", MapVersion: "v9", RegionID: "11", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.nameRegion(blid, "11", "Cuisine", false); err != nil { // names 11 on both maps
		t.Fatal(err)
	}
	if _, err := assignName(h, ctx, "", "Cuisine", "salon"); err == nil || !strings.Contains(err.Error(), "--map") {
		t.Fatalf("same name on two maps: %v", err)
	}
	if res, err := assignName(h, ctx, "m2", "Cuisine", "salon"); err != nil || res.MapID != "m2" {
		t.Fatalf("--map m2: %+v, %v", res, err)
	}
	if _, err := h.store.nameRegion(blid, "4", "Cuisine", false); err != nil { // second "Cuisine" on m1
		t.Fatal(err)
	}
	if _, err := assignName(h, ctx, "m1", "Cuisine", "salon"); err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("same name twice on one map: %v", err)
	}
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

	res, err := assignID(h, ctx, "", "", "11", "salon")
	if err != nil || res.Previous != "" || res.RoomID != "salon" || res.MapID != "m1" {
		t.Fatalf("assign = %+v, %v", res, err)
	}
	// Many areas may share a room.
	if _, err := assignID(h, ctx, "", "", "4", "salon"); err != nil {
		t.Fatal(err)
	}
	// Moving reports the old room, and the area is in exactly one room.
	res, err = assignID(h, ctx, "", "", "11", "cuisine")
	if err != nil || res.Previous != "salon" {
		t.Fatalf("move = %+v, %v", res, err)
	}
	if got := roomOf(t, h, "m1", "11"); got != "cuisine" {
		t.Errorf("region 11 room = %q", got)
	}
	if got := roomOf(t, h, "m1", "4"); got != "salon" {
		t.Errorf("region 4 room = %q (moving 11 must not touch it)", got)
	}
	res, err = assignID(h, ctx, "", "", "11", "")
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

	if _, err := assignID(h, ctx, "", "", "11", "nowhere"); err == nil || !strings.Contains(err.Error(), `unknown room "nowhere"`) {
		t.Errorf("unknown room: %v", err)
	}
	// Another failure of the rooms service is not reported as "unknown room".
	_, err := assignID(h, ctx, "", "", "11", "broken")
	if err == nil || strings.Contains(err.Error(), "unknown room") ||
		!strings.Contains(err.Error(), `checking room "broken"`) || !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("other rooms error: %v", err)
	}
	if got := roomOf(t, h, "m1", "11"); got != "" {
		t.Errorf("a refused assignment wrote %q", got)
	}
	if _, err := assignID(h, ctx, "", "", "99", "salon"); err == nil || !strings.Contains(err.Error(), `no region id "99"`) {
		t.Errorf("unknown region: %v", err)
	}
	if _, err := assignID(h, ctx, "", "m9", "11", "salon"); err == nil || !strings.Contains(err.Error(), `on map "m9"`) {
		t.Errorf("unknown map: %v", err)
	}
	if _, err := assignID(h, ctx, "nobody", "", "11", "salon"); err == nil {
		t.Error("unknown robot accepted")
	}

	noRoomsService(t)
	if _, err := assignID(h, ctx, "", "", "11", "salon"); err == nil || !strings.Contains(err.Error(), "rooms are not enabled") {
		t.Errorf("no rooms service: %v", err)
	}
	// Unassigning needs no rooms service.
	if _, err := assignID(h, ctx, "", "", "11", ""); err != nil {
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

	_, err := assignID(h, ctx, "", "", "11", "salon")
	if err == nil || !strings.Contains(err.Error(), "--map") || !strings.Contains(err.Error(), "2 robot maps") {
		t.Fatalf("ambiguous id: %v", err)
	}
	if roomOf(t, h, "m1", "11") != "" || roomOf(t, h, "m2", "11") != "" {
		t.Error("an ambiguous assignment wrote something")
	}
	if _, err := assignID(h, ctx, "", "m2", "11", "etage"); err != nil {
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
	if _, err := assignID(h, ctx, "", "", "30", "salon"); err == nil {
		t.Error("two current regions with one id must stay ambiguous")
	}
	if _, err := h.store.syncCloud(blid, both[1:]); err != nil {
		t.Fatal(err)
	}
	res, err := assignID(h, ctx, "", "", "30", "salon")
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
	if _, err := assignID(h, context.Background(), "", "", "7", "salon"); err != nil {
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

// fakeRoomNames registers a temperature.list returning the given id -> name
// rooms (registered on top of fakeRooms, which answers temperature.get).
func fakeRoomNames(t *testing.T, names map[string]string) {
	t.Helper()
	prev, _ := myhome.Methods(myhome.TemperatureList)
	t.Cleanup(func() {
		if prev != nil {
			myhome.RegisterMethodHandler(myhome.TemperatureList, prev.ActionE)
		} else {
			myhome.UnregisterMethodHandler(myhome.TemperatureList)
		}
	})
	myhome.RegisterMethodHandler(myhome.TemperatureList, func(context.Context, any) (any, error) {
		l := myhome.TemperatureRoomList{}
		for id, n := range names {
			l[id] = &myhome.TemperatureRoomConfig{RoomID: id, Name: n}
		}
		return &l, nil
	})
}

// The room argument may be the room's name: case-insensitive, trimmed, and an
// exact id match wins over a name match. Several rooms with one name are refused.
func TestAssignRoomByName(t *testing.T) {
	fakeRooms(t, "salon", "cuisine", "kitchen2")
	fakeRoomNames(t, map[string]string{
		"salon": "Salon", "cuisine": "Cuisine", "kitchen2": "cuisine", // "kitchen2" is also named like the id "cuisine"
		"ch1": "Chambre", "ch2": " chambre ", "sdb": "Salle de bains",
	})
	h := assignHarness(t)
	ctx := context.Background()

	res, err := assignID(h, ctx, "", "", "11", "  SALLE DE BAINS ")
	if err != nil || res.RoomID != "sdb" {
		t.Fatalf("by name: %+v, %v", res, err)
	}
	if got := roomOf(t, h, "m1", "11"); got != "sdb" {
		t.Errorf("stored room = %q, want the id", got)
	}
	// The id "cuisine" also is the name of kitchen2: the id wins.
	if res, err = assignID(h, ctx, "", "", "11", "cuisine"); err != nil || res.RoomID != "cuisine" || res.Previous != "sdb" {
		t.Fatalf("id vs name: %+v, %v", res, err)
	}
	// RoomID is trimmed daemon-side too.
	if res, err = assignID(h, ctx, "", "", "4", "  salon\t"); err != nil || res.RoomID != "salon" {
		t.Fatalf("untrimmed id: %+v, %v", res, err)
	}
	_, err = assignID(h, ctx, "", "", "11", "Chambre")
	if err == nil || !strings.Contains(err.Error(), "ch1") || !strings.Contains(err.Error(), "ch2") {
		t.Fatalf("ambiguous name must list ids: %v", err)
	}
	if got := roomOf(t, h, "m1", "11"); got != "cuisine" {
		t.Errorf("a refused assignment changed the room to %q", got)
	}
	if _, err = assignID(h, ctx, "", "", "11", "nothing"); err == nil || !strings.Contains(err.Error(), `unknown room "nothing"`) {
		t.Errorf("unknown: %v", err)
	}
}

// A stale duplicate on the same map must not resolve silently, and the errors
// list the clashing map/id pairs.
func TestAssignDuplicatesListPairs(t *testing.T) {
	fakeRooms(t, "salon")
	h := assignHarness(t)
	ctx := context.Background()
	for _, id := range []string{"11", "4"} {
		if _, err := h.store.nameRegion(blid, id, "Cuisine", false); err != nil {
			t.Fatal(err)
		}
	}
	// Make region 4 stale (the robot reports another version of map m1).
	h.svc.mu.Lock()
	h.svc.robots[blid].status = &myhome.VacuumStatus{Maps: []myhome.VacuumMap{{ID: "m1", Version: "other"}}}
	h.svc.mu.Unlock()
	_, err := assignName(h, ctx, "", "Cuisine", "salon")
	if err == nil || !strings.Contains(err.Error(), "m1/11") || !strings.Contains(err.Error(), "m1/4") || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("same-map duplicate: %v", err)
	}
	// The same name on two maps lists the pairs too.
	if err := h.store.learnRegion(regionRecord{BLID: blid, MapID: "m2", MapVersion: "v9", RegionID: "7", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.nameRegion(blid, "7", "Hall", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.nameRegion(blid, "4", "Hall", false); err != nil {
		t.Fatal(err)
	}
	h.svc.mu.Lock()
	h.svc.robots[blid].status = nil // no live state: nothing is stale
	h.svc.mu.Unlock()
	_, err = assignName(h, ctx, "", "Hall", "salon")
	if err == nil || !strings.Contains(err.Error(), "m1/4") || !strings.Contains(err.Error(), "m2/7") || !strings.Contains(err.Error(), "--map") {
		t.Fatalf("name on two maps: %v", err)
	}
}
