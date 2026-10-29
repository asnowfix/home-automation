package vacuum

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/roomba"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
)

const blid = "0123456789ABCDEF0123456789ABCDEF"

type fakeClient struct {
	mu        sync.Mutex
	host      string
	password  string
	connected bool
	connectFn func() error
	onChange  func(roomba.Status)
	commands  []string
	cleaned   []roomba.Region
	cleanMap  roomba.Map
}

func (f *fakeClient) Connect(ctx context.Context, retry time.Duration) error {
	if f.connectFn != nil {
		if err := f.connectFn(); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.connected = true
	f.mu.Unlock()
	return nil
}
func (f *fakeClient) Connected() bool               { f.mu.Lock(); defer f.mu.Unlock(); return f.connected }
func (f *fakeClient) Close()                        { f.mu.Lock(); f.connected = false; f.mu.Unlock() }
func (f *fakeClient) Status() (roomba.Status, bool) { return roomba.Status{}, false }
func (f *fakeClient) Command(ctx context.Context, cmd string, extra map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, cmd)
	return nil
}
func (f *fakeClient) CleanRegions(ctx context.Context, m roomba.Map, regions []roomba.Region) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanMap, f.cleaned = m, regions
	return nil
}

type fakeFinder struct {
	info roomba.Info
	err  error
}

func (f fakeFinder) Find(ctx context.Context, b string) (roomba.Info, error) {
	if f.err != nil || f.info.BLID != b {
		return roomba.Info{}, roomba.ErrNotFound
	}
	return f.info, nil
}
func (f fakeFinder) Scan(ctx context.Context) ([]roomba.Info, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []roomba.Info{f.info}, nil
}

type harness struct {
	svc     *Service
	store   *Storage
	mu      sync.Mutex
	clients []*fakeClient
}

func (h *harness) last() *fakeClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) == 0 {
		return nil
	}
	return h.clients[len(h.clients)-1]
}

// newHarness starts a service with cfg; seed, when not nil, is the robot row a
// previous run of the daemon left in the database.
func newHarness(t *testing.T, f finder, cfg Config, seed *robotRecord, opts ...func(*Service)) *harness {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	store, err := NewStorage(logr.Discard(), db)
	if err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		if _, err := db.NamedExec(`INSERT INTO vacuum_robots (blid, name, host) VALUES (:blid, :name, :host)`, seed); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &harness{store: store}
	h.svc = &Service{
		ctx: ctx, log: logr.Discard(), store: store, cfg: cfg, finder: f, robots: map[string]*robot{},
		newClient: func(_ logr.Logger, b, pw, host string, onChange func(roomba.Status)) robotClient {
			c := &fakeClient{host: host, password: pw, onChange: onChange}
			h.mu.Lock()
			h.clients = append(h.clients, c)
			h.mu.Unlock()
			return c
		},
	}
	for _, o := range opts {
		o(h.svc)
	}
	if err := h.svc.start(); err != nil {
		t.Fatal(err)
	}
	return h
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func connected(h *harness) bool {
	v, err := h.svc.Status("")
	return err == nil && v.Connected
}

func TestDiscoveryUpdatesHost(t *testing.T) {
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50", Name: "Edmond"}},
		Config{BLID: blid, Password: "pw"}, &robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.74"})
	eventually(t, "connection", func() bool { return connected(h) })

	if got := h.last().host; got != "192.0.2.50" {
		t.Fatalf("connected to %s, want the discovered address", got)
	}
	rec, _ := h.store.robot(blid)
	if rec.Host != "192.0.2.50" {
		t.Fatalf("stored host = %s, want the discovered address", rec.Host)
	}
}

func TestDiscoveryMissFallsBackToStoredHost(t *testing.T) {
	h := newHarness(t, fakeFinder{err: errors.New("no reply")},
		Config{BLID: blid, Password: "pw"}, &robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"})
	eventually(t, "connection", func() bool { return connected(h) })
	if got := h.last().host; got != "192.0.2.50" {
		t.Fatalf("connected to %s, want the stored address", got)
	}
}

func TestCommandsAndNoPasswordInResults(t *testing.T) {
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: "pw-must-not-leak"}, &robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"})
	eventually(t, "connection", func() bool { return connected(h) })

	if _, err := h.svc.Command(context.Background(), "edmond", roomba.CmdStart); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Command(context.Background(), "", "selfdestruct"); err == nil {
		t.Fatal("unknown command accepted")
	}
	if _, err := h.svc.Command(context.Background(), "nobody", roomba.CmdDock); err == nil {
		t.Fatal("unknown robot accepted")
	}
	if c := h.last(); len(c.commands) != 1 || c.commands[0] != roomba.CmdStart {
		t.Fatalf("commands = %v", c.commands)
	}

	h.last().onChange(roomba.Status{Name: "Edmond", Battery: 87, Phase: roomba.PhaseRun})
	v, err := h.svc.Status(blid)
	if err != nil || v.Status == nil || v.Status.Battery != 87 || v.Status.Phase != "run" {
		t.Fatalf("status = %+v, %v", v, err)
	}
	if strings.Contains(jsonOf(t, h.svc.List()), "pw-must-not-leak") {
		t.Fatal("password leaked into vacuum.list")
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRoomLearningNamingAndClean(t *testing.T) {
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: "pw"}, &robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"})
	eventually(t, "connection", func() bool { return connected(h) })
	c := h.last()

	// A room clean started from the iRobot app shows up in lastCommand.
	live := roomba.Status{
		Name: "Edmond", Maps: []roomba.Map{{ID: "m1", Version: "v1"}},
		LastCommand: roomba.LastCommand{Command: "start", MapID: "m1", MapVer: "v1",
			Regions: []roomba.Region{{ID: "11", Type: "rid"}, {ID: "4", Type: "rid"}}},
	}
	c.onChange(live)

	rooms, err := h.svc.Rooms("")
	if err != nil || len(rooms.Regions) != 2 {
		t.Fatalf("rooms = %+v, %v", rooms, err)
	}
	if _, err := h.svc.NameRoom("", "11", "Kitchen"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.NameRoom("", "99", "Nowhere"); err == nil {
		t.Fatal("naming an unknown region succeeded")
	}

	if _, err := h.svc.Clean(context.Background(), "", []string{"kitchen", "4"}); err != nil {
		t.Fatal(err)
	}
	if c.cleanMap != (roomba.Map{ID: "m1", Version: "v1"}) || len(c.cleaned) != 2 || c.cleaned[0].ID != "11" || c.cleaned[1].ID != "4" {
		t.Fatalf("cleaned %+v on %+v", c.cleaned, c.cleanMap)
	}

	// The map is edited in the app: its version changes and learned ids become stale.
	live.Maps = []roomba.Map{{ID: "m1", Version: "v2"}}
	live.LastCommand = roomba.LastCommand{Command: "dock"}
	c.onChange(live)
	if _, err := h.svc.Clean(context.Background(), "", []string{"kitchen"}); err == nil || !strings.Contains(err.Error(), "older version") {
		t.Fatalf("stale room accepted: %v", err)
	}
	if _, err := h.svc.Clean(context.Background(), "", []string{"attic"}); err == nil {
		t.Fatal("unknown room accepted")
	}
}

func TestDisabledWithoutCredentials(t *testing.T) {
	for _, cfg := range []Config{{}, {BLID: blid}, {Password: "pw"}, {Host: "192.0.2.50"}} {
		if cfg.Enabled() {
			t.Fatalf("%+v reported enabled", Config{BLID: cfg.BLID, Host: cfg.Host})
		}
		h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}}, cfg,
			&robotRecord{BLID: blid, Name: "Edmond", Host: "192.0.2.50"})
		if n := len(h.svc.List().Robots); n != 0 {
			t.Fatalf("disabled service lists %d robots", n)
		}
		if _, err := h.svc.Status(""); err == nil || !strings.Contains(err.Error(), "MYHOME_VACUUM_PASSWORD") {
			t.Fatalf("status err = %v, want a pointer to MYHOME_VACUUM_*", err)
		}
		time.Sleep(50 * time.Millisecond)
		if h.last() != nil {
			t.Fatal("disabled service opened a robot connection")
		}
	}
}

func TestEnabledUsesConfiguredPassword(t *testing.T) {
	cfg := Config{BLID: blid, Password: "configured-password"}
	if !cfg.Enabled() {
		t.Fatal("BLID and password set, but not enabled")
	}
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}}, cfg, nil)
	eventually(t, "connection", func() bool { return connected(h) })
	if got := h.last().password; got != "configured-password" {
		t.Fatal("client was not given the configured password")
	}
	var cols []string
	if err := h.store.db.Select(&cols, `SELECT name FROM pragma_table_info('vacuum_robots')`); err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		if c == "password" {
			t.Fatal("vacuum_robots still has a password column")
		}
	}
}

func TestConfiguredHostSkipsDiscovery(t *testing.T) {
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		Config{BLID: blid, Password: "pw", Host: "192.0.2.99"}, nil)
	eventually(t, "connection", func() bool { return connected(h) })
	if got := h.last().host; got != "192.0.2.99" {
		t.Fatalf("connected to %s, want MYHOME_VACUUM_HOST", got)
	}
}

func TestMigrationDropsStoredPassword(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	// The table as the first #606 build created it.
	if _, err := db.Exec(`
	CREATE TABLE vacuum_robots (
		blid       TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		password   TEXT NOT NULL,
		host       TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	INSERT INTO vacuum_robots (blid, name, password, host) VALUES ('` + blid + `', 'Edmond', 'old-stored-password', '192.0.2.50');
	`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStorage(logr.Discard(), db)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM pragma_table_info('vacuum_robots') WHERE name='password'`); err != nil || n != 0 {
		t.Fatalf("password column still present (%d, %v)", n, err)
	}
	rec, err := store.robot(blid)
	if err != nil || rec.Name != "Edmond" || rec.Host != "192.0.2.50" {
		t.Fatalf("robot after migration = %+v, %v", rec, err)
	}
	// Idempotent: a second start finds nothing to migrate.
	if _, err := NewStorage(logr.Discard(), db); err != nil {
		t.Fatal(err)
	}
}
