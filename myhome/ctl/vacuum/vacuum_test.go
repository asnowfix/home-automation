package vacuum

import (
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
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
