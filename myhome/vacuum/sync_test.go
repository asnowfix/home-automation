package vacuum

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/roomba"
	"github.com/asnowfix/home-automation/pkg/roomba/cloud"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/jmoiron/sqlx"
)

// fakeCloudMaps stands in for *cloud.Client.
type fakeCloudMaps struct {
	mu    sync.Mutex
	maps  []cloud.Map
	err   error
	calls int
	email string
	pw    string
}

func (f *fakeCloudMaps) Maps(ctx context.Context, email, password, b string) ([]cloud.Map, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.email, f.pw = email, password
	if f.err != nil {
		return nil, f.err
	}
	return f.maps, nil
}

func (f *fakeCloudMaps) set(maps []cloud.Map, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.maps, f.err = maps, err
}

func (f *fakeCloudMaps) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func cloudCfg() Config {
	return Config{BLID: blid, Password: "pw", IRobotEmail: "owner@example.com", IRobotPassword: "account-pw"}
}

func regionByID(t *testing.T, h *harness, id string) myhome.VacuumRegion {
	t.Helper()
	res, err := h.svc.Rooms("")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Regions {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("region %s not listed: %+v", id, res.Regions)
	return myhome.VacuumRegion{}
}

func TestRoomSyncUpsertNamePolicyAndClean(t *testing.T) {
	fc := &fakeCloudMaps{maps: []cloud.Map{
		{ID: "m1", Version: "251027T114719", Name: "Rez-de-chaussée", Regions: []cloud.Region{
			{ID: "1", Name: "Salle à manger", Type: "dining_room"},
			{ID: "21", Name: "Cuisine", Type: "kitchen"},
		}},
		{ID: "m2", Version: "231016T113248", Name: "Étage", Regions: []cloud.Region{}},
	}}
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}}, cloudCfg(),
		&robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"},
		func(s *Service) {
			s.cloud = fc
			// A region learned earlier from the robot and named locally: the cloud must win.
			if err := s.store.learnRegion(regionRecord{BLID: blid, MapID: "m1", MapVersion: "old", RegionID: "21", RegionType: "rid"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.store.nameRegion(blid, "21", "Kitchen", false); err != nil {
				t.Fatal(err)
			}
		})

	// One sync at start, with the configured account.
	eventually(t, "sync at start", func() bool {
		res, _ := h.svc.Rooms("")
		return res != nil && len(res.Regions) == 2 && res.Regions[0].Source == myhome.VacuumSourceCloud &&
			res.Regions[1].Source == myhome.VacuumSourceCloud
	})
	if fc.count() != 1 || fc.email != "owner@example.com" || fc.pw != "account-pw" {
		t.Fatalf("cloud called %d times with %q", fc.count(), fc.email)
	}
	k := regionByID(t, h, "21")
	if k.Name != "Cuisine" || k.Kind != "kitchen" || k.MapName != "Rez-de-chaussée" || k.MapVersion != "251027T114719" || k.Type != "rid" || k.Stale {
		t.Fatalf("cloud did not overwrite the local region: %+v", k)
	}

	// The robot reports a newer map version: normal, so informational, not stale.
	eventually(t, "connection", func() bool { return connected(h) })
	c := h.last()
	live := roomba.Status{Name: "Edmond", Maps: []roomba.Map{{ID: "m1", Version: "261029T090306"}}}
	c.onChange(live)
	k = regionByID(t, h, "21")
	if !k.Unverified || k.Stale || k.RobotMapVersion != "261029T090306" {
		t.Fatalf("version mismatch not reported as unverified: %+v", k)
	}

	// A room clean sends the robot's own version, never the cloud's.
	if _, err := h.svc.Clean(context.Background(), "", []string{"cuisine", "salle à manger"}); err != nil {
		t.Fatal(err)
	}
	if c.cleanMap != (roomba.Map{ID: "m1", Version: "261029T090306"}) || len(c.cleaned) != 2 || c.cleaned[0] != (roomba.Region{ID: "21", Type: "rid"}) {
		t.Fatalf("cleaned %+v on %+v", c.cleaned, c.cleanMap)
	}

	// The robot echoes the clean in lastCommand: the cloud region keeps its cloud version.
	live.LastCommand = roomba.LastCommand{Command: "start", MapID: "m1", MapVer: "261029T090306",
		Regions: []roomba.Region{{ID: "21", Type: "rid"}, {ID: "5", Type: "rid"}}}
	c.onChange(live)
	if k = regionByID(t, h, "21"); k.Source != myhome.VacuumSourceCloud || k.MapVersion != "251027T114719" || k.Name != "Cuisine" {
		t.Fatalf("lastCommand overwrote a cloud region: %+v", k)
	}

	// Names: cloud regions refuse, the cloud-less learned region 5 accepts.
	if _, err := h.svc.NameRoom("", "21", "Kitchen"); err == nil || !strings.Contains(err.Error(), "managed by the iRobot cloud") {
		t.Fatalf("renaming a cloud region: %v", err)
	}
	if _, err := h.svc.NameRoom("", "5", "Cellier"); err != nil {
		t.Fatal(err)
	}
	if regionByID(t, h, "21").Name != "Cuisine" {
		t.Fatal("refused rename changed the name")
	}

	// A room that disappears from the cloud is marked stale, not deleted, and not cleaned.
	fc.set([]cloud.Map{{ID: "m1", Version: "251101T080000", Name: "Rez-de-chaussée",
		Regions: []cloud.Region{{ID: "1", Name: "Salle à manger", Type: "dining_room"}}}}, nil)
	res, err := syncNow(t, h)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Updated != 1 || res.Stale != 1 || len(res.Maps) != 1 || res.Maps[0].RobotVersion != "261029T090306" {
		t.Fatalf("sync result = %+v", res)
	}
	if k = regionByID(t, h, "21"); !k.Stale {
		t.Fatalf("vanished room not stale: %+v", k)
	}
	if _, err := h.svc.Clean(context.Background(), "", []string{"cuisine"}); err == nil || !strings.Contains(err.Error(), "no longer on the iRobot cloud map") {
		t.Fatalf("stale cloud room accepted: %v", err)
	}
	if regionByID(t, h, "5").Name != "Cellier" {
		t.Fatal("sync touched a region the cloud never provided")
	}

	// A failed sync keeps every stored room.
	before, _ := h.svc.Rooms("")
	fc.set(nil, &cloud.Error{Step: cloud.StepGigya, GigyaCode: 403042, Msg: "invalid email or password"})
	if _, err := h.svc.SyncRooms(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "403042") || !strings.Contains(err.Error(), "stored rooms kept") {
		t.Fatalf("failed sync err = %v", err)
	}
	after, _ := h.svc.Rooms("")
	if jsonOf(t, before) != jsonOf(t, after) {
		t.Fatalf("failed sync changed rooms:\n%s\n%s", jsonOf(t, before), jsonOf(t, after))
	}
}

func TestCloudRoomNeedsRobotVersion(t *testing.T) {
	fc := &fakeCloudMaps{maps: []cloud.Map{{ID: "m1", Version: "c1", Regions: []cloud.Region{{ID: "21", Name: "Cuisine"}}}}}
	h := newHarness(t, fakeFinder{err: errors.New("offline")}, cloudCfg(), nil, func(s *Service) { s.cloud = fc })
	eventually(t, "sync at start", func() bool { res, _ := h.svc.Rooms(""); return res != nil && len(res.Regions) == 1 })
	// No host known and discovery fails: the robot never reports its maps.
	if _, err := h.svc.Clean(context.Background(), "", []string{"cuisine"}); err == nil || !strings.Contains(err.Error(), "has not reported map m1") {
		t.Fatalf("clean without the robot's map version: %v", err)
	}
}

func TestRoomSyncDisabledWithoutAccount(t *testing.T) {
	fc := &fakeCloudMaps{}
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}}, Config{BLID: blid, Password: "pw"},
		&robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"}, func(s *Service) { s.cloud = fc })
	if _, err := h.svc.SyncRooms(context.Background(), ""); !errors.Is(err, errCloudDisabled) {
		t.Fatalf("err = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if fc.count() != 0 {
		t.Fatal("cloud contacted without an account")
	}
	res, _ := h.svc.Rooms("")
	if res.CloudNames {
		t.Fatal("CloudNames set without an account")
	}
}

func TestRoomSyncAtStartDoesNotBlock(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	fc := blockingCloud{block}
	done := make(chan struct{})
	go func() {
		newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}}, cloudCfg(), nil, func(s *Service) { s.cloud = fc })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("service start waited for the cloud")
	}
}

type blockingCloud struct{ block chan struct{} }

func (b blockingCloud) Maps(ctx context.Context, _, _, _ string) ([]cloud.Map, error) {
	select {
	case <-b.block:
	case <-ctx.Done():
	}
	return nil, errors.New("unreachable")
}

func TestMigrationAddsRegionColumns(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	// vacuum_regions as the #606 builds before #628 created it.
	if _, err := db.Exec(`
	CREATE TABLE vacuum_regions (
		blid TEXT NOT NULL, pmap_id TEXT NOT NULL, pmap_version TEXT NOT NULL,
		region_id TEXT NOT NULL, region_type TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (blid, pmap_id, region_id));
	INSERT INTO vacuum_regions VALUES ('` + blid + `', 'm1', 'v1', '11', 'rid', 'Kitchen');`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStorage(logr.Discard(), db)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := store.regions(blid)
	if err != nil || len(recs) != 1 || recs[0].Source != "robot" || recs[0].Name != "Kitchen" || recs[0].Removed {
		t.Fatalf("regions after migration = %+v, %v", recs, err)
	}
	if _, err := NewStorage(logr.Discard(), db); err != nil {
		t.Fatal(err)
	}
}

// TestRoomSyncLeaksNoSecret runs the real cloud client against a fake iRobot
// cloud, once successfully and once failing at each login step, and checks the
// account password, the Gigya signature, the AWS credentials and the robot's
// local password appear in no log line and no RPC result.
func TestRoomSyncLeaksNoSecret(t *testing.T) {
	const (
		robotPw   = "robot-local-pw-must-not-leak"
		accountPw = "account-pw-must-not-leak"
		gigyaSig  = "gigya-sig-must-not-leak"
		awsSecret = "aws-secret-must-not-leak"
		awsToken  = "aws-token-must-not-leak"
	)
	secrets := []string{robotPw, accountPw, gigyaSig, awsSecret, awsToken}

	for _, failAt := range []string{"", "/accounts.login", "/v1/login/account", "/auth/v1/" + blid + "/pmaps"} {
		t.Run("fail at "+failAt, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == failAt {
					if failAt == "/accounts.login" {
						_, _ = io.WriteString(w, `{"errorCode":403042,"errorMessage":"Invalid LoginID"}`)
						return
					}
					w.WriteHeader(http.StatusForbidden)
					return
				}
				switch r.URL.Path {
				case "/v1/app/discover":
					_ = json.NewEncoder(w).Encode(map[string]any{"gigya": map[string]string{"api_key": "k"},
						"httpBase": srv.URL, "httpBaseAuth": srv.URL + "/auth", "awsRegion": "us-east-1"})
				case "/accounts.login":
					_, _ = io.WriteString(w, `{"errorCode":0,"UID":"u","UIDSignature":"`+gigyaSig+`","signatureTimestamp":"1"}`)
				case "/v1/login/account":
					_, _ = io.WriteString(w, `{"credentials":{"AccessKeyId":"AKID","SecretKey":"`+awsSecret+`","SessionToken":"`+awsToken+`"}}`)
				case "/auth/v1/" + blid + "/pmaps":
					_, _ = io.WriteString(w, `[{"pmap_id":"m1","user_pmapv_id":"c1","active_pmapv_details":{"map_header":{"name":"RdC"},"regions":[{"id":"21","name":"Cuisine","region_type":"kitchen"}]}}]`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			var logMu sync.Mutex
			var logs strings.Builder
			logger := funcr.New(func(prefix, args string) {
				logMu.Lock()
				logs.WriteString(prefix + " " + args + "\n")
				logMu.Unlock()
			}, funcr.Options{Verbosity: 10})

			cc := cloud.New()
			cc.HTTP = srv.Client()
			cc.Discovery, cc.Gigya = srv.URL, srv.URL
			cfg := Config{BLID: blid, Password: robotPw, IRobotEmail: "owner@example.com", IRobotPassword: accountPw}
			h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}}, cfg, nil, func(s *Service) {
				s.cloud = cc
				s.log = logger
			})
			res, err := syncNow(t, h)
			if (err == nil) != (failAt == "") {
				t.Fatalf("sync err = %v", err)
			}
			rooms, _ := h.svc.Rooms("")
			var out strings.Builder
			out.WriteString(jsonOf(t, res) + jsonOf(t, rooms) + jsonOf(t, h.svc.List()))
			if err != nil {
				out.WriteString(err.Error())
			}
			logMu.Lock()
			out.WriteString(logs.String())
			logMu.Unlock()
			for _, sec := range secrets {
				if strings.Contains(out.String(), sec) {
					t.Fatalf("secret %q leaked:\n%s", sec, out.String())
				}
			}
			if failAt == "" && !strings.Contains(jsonOf(t, rooms), "Cuisine") {
				t.Fatalf("rooms = %s", jsonOf(t, rooms))
			}
		})
	}
}

// syncNow runs a sync, waiting out the one the service starts on its own.
func syncNow(t *testing.T, h *harness) (*myhome.VacuumRoomsSyncResult, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		res, err := h.svc.SyncRooms(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), "already running") || time.Now().After(deadline) {
			return res, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
