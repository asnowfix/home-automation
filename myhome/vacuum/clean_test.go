package vacuum

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/roomba"
)

func init() {
	// Keep the watch short: tests that need the window to expire wait it out.
	abortWindow = 300 * time.Millisecond
}

const (
	cloudVer = "251027T114719" // the cloud's active version (the one measured to work)
	robotVer = "261029T090306" // the version the robot reports (rejected with 224, 2026-10-30)
	hint224  = "error 224: the robot opened the mission and aborted at once; seen when a room clean names a map version the robot does not accept. Rooms from the iRobot cloud are cleaned with the cloud map version"
)

// docked is the robot's state before a room clean.
func docked(reportedVer string) roomba.Status {
	return roomba.Status{Name: "Edmond", Phase: roomba.PhaseCharge, Cycle: "none", Mission: 100,
		Maps: []roomba.Map{{ID: "m1", Version: reportedVer}}}
}

func running(st roomba.Status) roomba.Status {
	st.Phase, st.Cycle, st.Mission, st.Error = roomba.PhaseRun, "clean", st.Mission+1, 0
	return st
}

func aborted(st roomba.Status, code int) roomba.Status {
	st.Phase, st.Cycle, st.Mission, st.Error = roomba.PhaseCharge, "none", st.Mission+1, code
	return st
}

// cleanHarness has the cloud room Cuisine (region 21, cloud version cloudVer)
// and a connected robot reporting reportedVer for map m1. react plays the
// robot: it gets the version each room clean names and the robot's state.
func cleanHarness(t *testing.T, reportedVer string, react func(h *harness, c *fakeClient, version string)) (*harness, *fakeClient) {
	t.Helper()
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}}, Config{BLID: blid, Password: "pw"},
		&robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"},
		func(s *Service) {
			if _, err := s.store.syncCloud(blid, []regionRecord{{MapID: "m1", MapVersion: cloudVer, MapName: "RdC",
				RegionID: "21", RegionType: "rid", Name: "Cuisine", Kind: "kitchen"}}); err != nil {
				t.Fatal(err)
			}
		})
	eventually(t, "connection", func() bool { return connected(h) })
	c := h.last()
	c.onChange(docked(reportedVer))
	c.react = func(m roomba.Map) { react(h, c, m.Version) }
	return h, c
}

func versions(c *fakeClient) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, m := range c.cleans {
		out = append(out, m.Version)
	}
	return out
}

func TestCleanCloudRoomRunsFirstTime(t *testing.T) {
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		c.onChange(running(docked(robotVer)))
	})
	res, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err != nil {
		t.Fatal(err)
	}
	if got := versions(c); len(got) != 1 || got[0] != cloudVer {
		t.Fatalf("commands = %v, want exactly one with the cloud version", got)
	}
	if got, want := payload(t, c.cleanMap, c.cleaned),
		`{"ordered":1,"pmap_id":"m1","regions":[{"region_id":"21","type":"rid"}],"user_pmapv_id":"`+cloudVer+`"}`; got != want {
		t.Fatalf("payload\n got  %s\n want %s", got, want)
	}
	if !res.OK || len(res.Attempts) != 1 || res.Attempts[0].Outcome != myhome.VacuumCleanRunning {
		t.Fatalf("result = %+v", res)
	}
}

func TestCleanRetriesOnceAfterImmediateAbort(t *testing.T) {
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		if v == cloudVer {
			c.onChange(aborted(docked(robotVer), 224))
		} else {
			c.onChange(running(aborted(docked(robotVer), 224)))
		}
	})
	res, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err != nil {
		t.Fatal(err)
	}
	if got := versions(c); len(got) != 2 || got[0] != cloudVer || got[1] != robotVer {
		t.Fatalf("commands = %v, want the cloud version then the robot's", got)
	}
	if !res.OK || len(res.Attempts) != 2 || res.Attempts[0].Error != 224 || res.Attempts[1].Outcome != myhome.VacuumCleanRunning ||
		res.Attempts[1].MapVersion != robotVer {
		t.Fatalf("result = %+v", res)
	}
}

func TestCleanBothAttemptsAbort(t *testing.T) {
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		h.svc.mu.Lock()
		cur := *h.svc.robots[blid].status
		h.svc.mu.Unlock()
		st := docked(robotVer)
		st.Mission = cur.Mission
		c.onChange(aborted(st, 224))
	})
	_, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err == nil {
		t.Fatal("both attempts aborted, but the clean reported success")
	}
	if got := versions(c); len(got) != 2 {
		t.Fatalf("commands = %v, want exactly two (never a third)", got)
	}
	for _, want := range []string{"attempt 1 with map version " + cloudVer, "attempt 2 with map version " + robotVer, hint224} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%s", want, err)
		}
	}
}

func TestCleanNoOtherVersion(t *testing.T) {
	// The robot reports the cloud's version: nothing else to try.
	h, c := cleanHarness(t, cloudVer, func(h *harness, c *fakeClient, v string) {
		c.onChange(aborted(docked(cloudVer), 224))
	})
	_, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err == nil || !strings.Contains(err.Error(), "no other version") || !strings.Contains(err.Error(), hint224) {
		t.Fatalf("err = %v", err)
	}
	if got := versions(c); len(got) != 1 {
		t.Fatalf("commands = %v, want one", got)
	}
}

func TestCleanNoRetryWhenNotDocked(t *testing.T) {
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		st := aborted(docked(robotVer), 224)
		c.onChange(st)
		st.Phase, st.Cycle = roomba.PhaseStop, "none" // picked up off the dock meanwhile
		c.onChange(st)
	})
	_, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err == nil || !strings.Contains(err.Error(), "not docked") {
		t.Fatalf("err = %v", err)
	}
	if got := versions(c); len(got) != 1 {
		t.Fatalf("commands = %v, want one", got)
	}
}

func TestCleanNoRetryAfterUserCommand(t *testing.T) {
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		if _, err := h.svc.Command(context.Background(), "", roomba.CmdDock); err != nil {
			t.Error(err)
		}
		c.onChange(aborted(docked(robotVer), 224))
	})
	_, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err == nil || !strings.Contains(err.Error(), "dock command was sent meanwhile") {
		t.Fatalf("err = %v", err)
	}
	if got := versions(c); len(got) != 1 {
		t.Fatalf("commands = %v, want one", got)
	}
}

func TestCleanWindowExpires(t *testing.T) {
	// Running before the window ends is a success, and anything after it is
	// not watched: no retry either way.
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		go func() {
			time.Sleep(2 * abortWindow)
			c.onChange(aborted(docked(robotVer), 224))
		}()
	})
	res, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || len(res.Attempts) != 1 || res.Attempts[0].Outcome != myhome.VacuumCleanNoReport {
		t.Fatalf("result = %+v", res)
	}
	time.Sleep(3 * abortWindow)
	if got := versions(c); len(got) != 1 {
		t.Fatalf("commands = %v, want one", got)
	}
}

func TestCleanIgnoresLeftoverError(t *testing.T) {
	// An error code left from an earlier mission, same mission counter: not an abort.
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		st := docked(robotVer)
		st.Error = 224
		c.onChange(st)
	})
	before := docked(robotVer)
	before.Error = 224
	c.onChange(before)
	res, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err != nil || res.Attempts[0].Outcome != myhome.VacuumCleanNoReport {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got := versions(c); len(got) != 1 {
		t.Fatalf("commands = %v, want one", got)
	}
}

func TestCleanRetryCanBeTurnedOff(t *testing.T) {
	retryOtherVersion = false
	t.Cleanup(func() { retryOtherVersion = true })
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {
		c.onChange(aborted(docked(robotVer), 224))
	})
	if _, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"}); err == nil || !strings.Contains(err.Error(), "retry disabled") {
		t.Fatalf("err = %v", err)
	}
	if got := versions(c); len(got) != 1 {
		t.Fatalf("commands = %v, want one", got)
	}
}

func TestCleanRefusesMapTheRobotDoesNotReport(t *testing.T) {
	h, c := cleanHarness(t, robotVer, func(h *harness, c *fakeClient, v string) {})
	c.onChange(roomba.Status{Name: "Edmond", Phase: roomba.PhaseCharge, Cycle: "none",
		Maps: []roomba.Map{{ID: "m2", Version: "x"}}})
	_, err := h.svc.Clean(context.Background(), "", []string{"Cuisine"})
	if err == nil || !strings.Contains(err.Error(), "does not report map m1") {
		t.Fatalf("err = %v", err)
	}
	if got := versions(c); len(got) != 0 {
		t.Fatalf("commands = %v, want none", got)
	}
}

func TestErrorHint(t *testing.T) {
	if myhome.VacuumErrorHint(224) != hint224 {
		t.Fatalf("hint = %q", myhome.VacuumErrorHint(224))
	}
	if myhome.VacuumErrorHint(0) != "" || myhome.VacuumErrorHint(15) != "" {
		t.Fatal("hint for an unknown code")
	}
}
