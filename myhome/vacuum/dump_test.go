package vacuum

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"

	"github.com/asnowfix/home-automation/internal/myhome"
)

func newDumpStorage(t *testing.T) *Storage {
	t.Helper()
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // one in-memory database for every statement
	t.Cleanup(func() { db.Close() })
	s, err := NewStorage(logr.Discard(), db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func seed(t *testing.T, s *Storage) {
	t.Helper()
	if _, err := s.robot("BLID1"); err != nil {
		t.Fatal(err)
	}
	if err := s.saveName("BLID1", "Roomy"); err != nil {
		t.Fatal(err)
	}
	if err := s.saveHost("BLID1", "10.0.0.5"); err != nil {
		t.Fatal(err)
	}
	if err := s.learnRegion(regionRecord{BLID: "BLID1", MapID: "pm1", MapVersion: "v1", RegionID: "3", RegionType: "rid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.syncCloud("BLID1", []regionRecord{{MapID: "pm1", MapVersion: "v2", RegionID: "7", RegionType: "rid", Name: "Kitchen", MapName: "Ground floor", Kind: "kitchen"}}); err != nil {
		t.Fatal(err)
	}
}

// roundTrip sends a dump through JSON, as the RPC does.
func roundTrip(t *testing.T, d *myhome.VacuumDump) *myhome.VacuumDump {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var out myhome.VacuumDump
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func TestVacuumDumpRoundTripIntoEmptyDB(t *testing.T) {
	src := newDumpStorage(t)
	seed(t, src)
	dump, err := src.Export()
	if err != nil {
		t.Fatal(err)
	}
	if len(dump.Robots) != 1 || len(dump.Regions) != 2 {
		t.Fatalf("dump = %d robots, %d regions", len(dump.Robots), len(dump.Regions))
	}
	if _, ok := dump.Robots[0]["password"]; ok {
		t.Error("a robot password column is exported")
	}
	if _, ok := dump.Robots[0]["updated_at"]; ok {
		t.Error("bookkeeping timestamp exported")
	}

	dst := newDumpStorage(t)
	res, err := dst.Import(roundTrip(t, dump))
	if err != nil {
		t.Fatal(err)
	}
	if res.Robots != 1 || res.Regions != 2 {
		t.Errorf("import result = %+v", res)
	}
	want, _ := src.regions("BLID1")
	got, _ := dst.regions("BLID1")
	if len(got) != 2 || !reflect.DeepEqual(want, got) {
		t.Errorf("regions differ:\nsrc %+v\ndst %+v", want, got)
	}
	r, _ := dst.robot("BLID1")
	if r.Name != "Roomy" || r.Host != "10.0.0.5" {
		t.Errorf("robot = %+v", r)
	}
	// importing twice is idempotent
	if _, err := dst.Import(roundTrip(t, dump)); err != nil {
		t.Fatal(err)
	}
	if got, _ := dst.regions("BLID1"); len(got) != 2 {
		t.Errorf("second import duplicated rows: %d", len(got))
	}
}

// vacuum_regions gained a room_id column (#751): a dump must survive both
// directions of that schema difference (an older build is simulated by dropping it).
func TestVacuumDumpToleratesRoomIDColumn(t *testing.T) {
	withRoom := newDumpStorage(t)
	seed(t, withRoom)
	if _, err := withRoom.db.Exec(`UPDATE vacuum_regions SET room_id='kitchen' WHERE region_id='7'`); err != nil {
		t.Fatal(err)
	}
	dump, err := withRoom.Export()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range dump.Regions {
		if r["region_id"] == "7" && r["room_id"] == "kitchen" {
			found = true
		}
	}
	if !found {
		t.Fatalf("room_id not exported: %+v", dump.Regions)
	}

	// into a build that has the column: kept
	again := newDumpStorage(t)
	if _, err := again.Import(roundTrip(t, dump)); err != nil {
		t.Fatal(err)
	}
	var room string
	if err := again.db.Get(&room, `SELECT room_id FROM vacuum_regions WHERE region_id='7'`); err != nil || room != "kitchen" {
		t.Errorf("room_id after import = %q, %v", room, err)
	}

	// into a build without it: ignored, the rest imports
	without := newDumpStorage(t)
	if _, err := without.db.Exec(`ALTER TABLE vacuum_regions DROP COLUMN room_id`); err != nil {
		t.Fatal(err)
	}
	res, err := without.Import(roundTrip(t, dump))
	if err != nil || res.Regions != 2 {
		t.Fatalf("import into older schema: %+v, %v", res, err)
	}

	// an old dump (no room_id) into a build with it: default applies
	old := roundTrip(t, func() *myhome.VacuumDump { d, _ := without.Export(); return d }())
	if _, err := again.Import(old); err != nil {
		t.Fatal(err)
	}
}
