package vacuum

import (
	"context"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/spf13/cobra"
)

func TestPrintRoomsVersionsAndStale(t *testing.T) {
	var b strings.Builder
	printRooms(&b, &myhome.VacuumRoomsResult{CloudNames: true, Regions: []myhome.VacuumRegion{
		{MapID: "m1", MapName: "RdC", MapVersion: "251027T114719", RobotMapVersion: "261029T090306", ID: "21", Type: "rid",
			Kind: "kitchen", Name: "Cuisine", Source: myhome.VacuumSourceCloud},
		{MapID: "m1", MapName: "RdC", MapVersion: "251027T114719", ID: "9", Type: "rid", Name: "Old", Source: myhome.VacuumSourceCloud, Stale: true},
	}})
	out := b.String()
	for _, want := range []string{
		"room cleans name iRobot cloud map version 251027T114719; the robot reports 261029T090306.",
		"[stale: no longer in the iRobot cloud]", "Cuisine", "kitchen",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "unverified") {
		t.Errorf("a version mismatch is presented as suspicious:\n%s", out)
	}
}

func TestPrintCleanAfterRetry(t *testing.T) {
	var b strings.Builder
	printClean(&b, "Cuisine", &myhome.VacuumCleanResult{OK: true, MapID: "m1", Attempts: []myhome.VacuumCleanAttempt{
		{MapVersion: "261029T090306", Outcome: myhome.VacuumCleanAborted, Error: 224},
		{MapVersion: "251027T114719", Outcome: myhome.VacuumCleanRunning},
	}})
	out := b.String()
	for _, want := range []string{
		"attempt 1, map version 261029T090306: aborted (error 224)",
		"cleaning Cuisine: running with map version 251027T114719",
		"error 224: the robot opened the mission and aborted at once",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// The CLI prints only what the RPC results carry, and those types have no
// secret field; this pins that the printed output stays free of the strings
// a leak would show (#628, same idea as the vacuum.list check).
func TestPrintSyncNoSecretFields(t *testing.T) {
	var b strings.Builder
	printSync(&b, &myhome.VacuumRoomsSyncResult{Maps: []myhome.VacuumSyncedMap{{ID: "m1", Name: "RdC", CloudVersion: "c1", Rooms: 8}}, Added: 8})
	out := strings.ToLower(b.String())
	for _, bad := range []string{"password", "token", "secret", "signature"} {
		if strings.Contains(out, bad) {
			t.Fatalf("sync output mentions %q:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "8 rooms") || !strings.Contains(out, "robot version not reported") {
		t.Fatalf("output = %s", out)
	}
}

func TestPrintAssign(t *testing.T) {
	for _, c := range []struct {
		res  myhome.VacuumRoomAssignResult
		want string
	}{
		{myhome.VacuumRoomAssignResult{RegionID: "12", RegionName: "Cuisine", RoomID: "cuisine"}, "area Cuisine is now in room cuisine"},
		{myhome.VacuumRoomAssignResult{RegionID: "12", RegionName: "Cuisine", RoomID: "cuisine", Previous: "salon"}, "area Cuisine moved from room salon to room cuisine"},
		{myhome.VacuumRoomAssignResult{RegionID: "12", RegionName: "Cuisine", RoomID: "cuisine", Previous: "cuisine"}, "area Cuisine was already in room cuisine"},
		{myhome.VacuumRoomAssignResult{RegionID: "12", RegionName: "Cuisine", Previous: "salon"}, "area Cuisine removed from room salon"},
		{myhome.VacuumRoomAssignResult{RegionID: "12", RegionName: "Cuisine"}, "area Cuisine was not in a room"},
	} {
		var b strings.Builder
		printAssign(&b, &c.res)
		if got := strings.TrimSpace(b.String()); got != c.want {
			t.Errorf("printAssign(%+v) = %q, want %q", c.res, got, c.want)
		}
	}
}

func TestPrintRoomsShowsRoomColumn(t *testing.T) {
	var b strings.Builder
	printRooms(&b, &myhome.VacuumRoomsResult{Regions: []myhome.VacuumRegion{
		{MapID: "m1", ID: "11", Type: "rid", Name: "Cuisine", Source: myhome.VacuumSourceRobot, RoomID: "kitchen"},
		{MapID: "m1", ID: "4", Type: "rid", Name: "Hall", Source: myhome.VacuumSourceRobot},
	}})
	out := b.String()
	if !strings.Contains(out, "ROOM") || !strings.Contains(out, "kitchen") {
		t.Errorf("no ROOM column:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Hall") && !strings.Contains(l, " - ") {
			t.Errorf("unassigned region not shown as '-': %q", l)
		}
	}
}

// The CLI only relays the RPC: the daemon resolves the region and checks the room.
func TestRunAssignCallsOnlyTheRPC(t *testing.T) {
	fake := myhome.NewFakeClient()
	fake.SetResult(myhome.VacuumRoomAssign, &myhome.VacuumRoomAssignResult{MapID: "m1", RegionID: "12", RegionName: "Cuisine", RoomID: "cuisine", Previous: "salon"})
	old := myhome.TheClient
	myhome.TheClient = fake
	t.Cleanup(func() { myhome.TheClient = old; assignMap = "" })
	assignMap = "m1"

	var b strings.Builder
	if err := runAssign(context.Background(), &b, "Cuisine", "cuisine"); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls) != 1 || fake.Calls[0].Method != myhome.VacuumRoomAssign {
		t.Fatalf("calls = %+v", fake.Calls)
	}
	p, ok := fake.Calls[0].Params.(*myhome.VacuumRoomAssignParams)
	if !ok || p.MapID != "m1" || p.RegionName != "Cuisine" || p.RegionID != "" || p.RoomID != "cuisine" {
		t.Errorf("params = %+v", fake.Calls[0].Params)
	}
	if !strings.Contains(b.String(), "moved from room salon to room cuisine") {
		t.Errorf("output = %q", b.String())
	}
}

// The ROOM column is sized from the data: a long room id keeps FROM aligned.
func TestPrintRoomsAlignsLongRoomID(t *testing.T) {
	var b strings.Builder
	printRooms(&b, &myhome.VacuumRoomsResult{Regions: []myhome.VacuumRegion{
		{MapID: "m1", ID: "11", Type: "rid", Name: "Chambre a coucher", Source: myhome.VacuumSourceRobot, RoomID: "chambre-parents-etage-superieur"},
		{MapID: "m1", ID: "4", Type: "rid", Name: "Hall", Source: myhome.VacuumSourceRobot},
	}})
	col := -1
	for _, l := range strings.Split(strings.TrimSpace(strings.SplitN(b.String(), "\n\n", 2)[0]), "\n") {
		i := strings.Index(l, "robot")
		if strings.HasPrefix(l, "REGION") {
			i = strings.Index(l, "FROM")
		}
		if i < 0 || (col >= 0 && i != col) {
			t.Fatalf("FROM column misaligned at %d (want %d) in:\n%s", i, col, b.String())
		}
		col = i
	}
}

// Blank names are refused by the CLI, before any RPC, and without printing usage.
func TestAssignRefusesBlankArguments(t *testing.T) {
	fake := myhome.NewFakeClient()
	old := myhome.TheClient
	myhome.TheClient = fake
	t.Cleanup(func() { myhome.TheClient = old })
	for _, c := range []struct {
		cmd  *cobra.Command
		args []string
		want string
	}{
		{roomsAssignCmd, []string{"  ", "salon"}, "area name is empty"},
		{roomsAssignCmd, []string{"Cuisine", " "}, "room is empty"},
		{roomsUnassignCmd, []string{""}, "area name is empty"},
	} {
		c.cmd.SilenceUsage = false
		err := c.cmd.RunE(c.cmd, c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: err = %v, want %q", c.cmd.Name(), c.args, err, c.want)
		}
		if !c.cmd.SilenceUsage {
			t.Errorf("%s: usage not silenced", c.cmd.Name())
		}
	}
	if len(fake.Calls) != 0 {
		t.Errorf("RPC called for blank input: %+v", fake.Calls)
	}
}
