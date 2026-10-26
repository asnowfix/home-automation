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

func newHarness(t *testing.T, f finder, seed ...robotRecord) *harness {
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
	for _, r := range seed {
		if err := store.saveRobot(r); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &harness{store: store}
	h.svc = &Service{
		ctx: ctx, log: logr.Discard(), store: store, finder: f, robots: map[string]*robot{},
		newClient: func(_ logr.Logger, b, pw, host string, onChange func(roomba.Status)) robotClient {
			c := &fakeClient{host: host, onChange: onChange}
			h.mu.Lock()
			h.clients = append(h.clients, c)
			h.mu.Unlock()
			return c
		},
		password: func(ctx context.Context, host, b string) (string, error) { return "secret-password-30-chars-long", nil },
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
		robotRecord{BLID: blid, Name: "Edmond", Password: "pw", Host: "192.0.2.74"})
	eventually(t, "connection", func() bool { return connected(h) })

	if got := h.last().host; got != "192.0.2.50" {
		t.Fatalf("connected to %s, want the discovered address", got)
	}
	recs, _ := h.store.robots()
	if recs[0].Host != "192.0.2.50" {
		t.Fatalf("stored host = %s, want the discovered address", recs[0].Host)
	}
}

func TestDiscoveryMissFallsBackToStoredHost(t *testing.T) {
	h := newHarness(t, fakeFinder{err: errors.New("no reply")},
		robotRecord{BLID: blid, Name: "Edmond", Password: "pw", Host: "192.0.2.50"})
	eventually(t, "connection", func() bool { return connected(h) })
	if got := h.last().host; got != "192.0.2.50" {
		t.Fatalf("connected to %s, want the stored address", got)
	}
}

func TestCommandsAndNoPasswordInResults(t *testing.T) {
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}},
		robotRecord{BLID: blid, Name: "Edmond", Password: "pw-must-not-leak", Host: "192.0.2.50"})
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
		robotRecord{BLID: blid, Name: "Edmond", Password: "pw", Host: "192.0.2.50"})
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

func TestPairStoresPasswordAndStartsSupervising(t *testing.T) {
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50", Name: "Edmond"}})
	if _, err := h.svc.Status(""); err == nil {
		t.Fatal("status before pairing should fail")
	}
	v, err := h.svc.Pair(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != blid || v.Name != "Edmond" {
		t.Fatalf("paired %+v", v)
	}
	if strings.Contains(jsonOf(t, v), "secret-password") {
		t.Fatal("password leaked into the pair result")
	}
	recs, _ := h.store.robots()
	if len(recs) != 1 || recs[0].Password != "secret-password-30-chars-long" {
		t.Fatalf("stored %+v", recs)
	}
	eventually(t, "connection", func() bool { return connected(h) })
}

func TestPairNeedsButton(t *testing.T) {
	h := newHarness(t, fakeFinder{info: roomba.Info{BLID: blid, IP: "192.0.2.50"}})
	calls := 0
	h.svc.password = func(context.Context, string, string) (string, error) {
		calls++
		return "", roomba.ErrNeedsButton
	}
	if _, err := h.svc.Pair(context.Background(), ""); !errors.Is(err, roomba.ErrNeedsButton) {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("retried %d times; a missing button press cannot be fixed by retrying", calls)
	}
}
