package homedrive

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
)

const testTopic = "homedrive/testhost/testuser/status"

var t0 = time.Date(2026, 11, 15, 14, 32, 10, 0, time.UTC) // 10 s after the fixtures' ts

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Set(t time.Time)     { c.mu.Lock(); c.t = t; c.mu.Unlock() }
func (c *testClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// logLines collects log output so tests can count throttled messages.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) logger() logr.Logger {
	return funcr.New(func(prefix, args string) {
		l.mu.Lock()
		l.lines = append(l.lines, prefix+" "+args)
		l.mu.Unlock()
	}, funcr.Options{})
}

func (l *logLines) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

func newTestCache(t *testing.T, clk *testClock, st *Store) (*Cache, *logLines) {
	t.Helper()
	ll := &logLines{}
	return NewCache(Config{Log: ll.logger(), Now: clk.Now, Store: st}), ll
}

func send(t *testing.T, c *Cache, topic string, payload []byte) {
	t.Helper()
	if err := c.HandleMessage(topic, payload, "test"); err != nil {
		t.Fatal(err)
	}
}

func only(t *testing.T, c *Cache, now time.Time) View {
	t.Helper()
	v := c.Snapshot(now)
	if len(v) != 1 {
		t.Fatalf("snapshot has %d instances, want 1", len(v))
	}
	return v[0]
}

func TestClassify_Boundaries(t *testing.T) {
	start := t0
	iv := 30 * time.Second
	tests := []struct {
		name   string
		lastTs time.Time
		start  time.Time
		now    time.Time
		want   Liveness
	}{
		{"fresh", t0, start, t0.Add(1 * time.Second), Online},
		{"exactly 2 intervals is still online", t0, start, t0.Add(60 * time.Second), Online},
		{"just over 2 intervals is stale", t0, start, t0.Add(61 * time.Second), Stale},
		{"exactly 15 min is not yet offline", t0, start, t0.Add(15 * time.Minute), Stale},
		{"just over 15 min is offline", t0, start, t0.Add(15*time.Minute + time.Second), Offline},
		{"nothing arrived, 15 min after start", time.Time{}, start, start.Add(15 * time.Minute), Stale},
		{"nothing arrived, over 15 min after start", time.Time{}, start, start.Add(15*time.Minute + time.Second), Offline},
		{"old doc, process just started: not offline yet", t0.Add(-3 * time.Hour), start, start.Add(14 * time.Minute), Stale},
		{"old doc, counted from process start", t0.Add(-3 * time.Hour), start, start.Add(15*time.Minute + time.Second), Offline},
		{"ts slightly in the future is online", t0.Add(30 * time.Second), start, t0, Online},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.lastTs, iv, tt.start, tt.now); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
	if got := Classify(t0, 0, start, t0.Add(61*time.Second)); got != Stale {
		t.Errorf("zero interval must default to 30 s, got %v", got)
	}
	if got := Classify(t0, 120*time.Second, start, t0.Add(200*time.Second)); got != Online {
		t.Errorf("interval 120 s: 200 s old must be online, got %v", got)
	}
}

func TestCache_NeverSeen(t *testing.T) {
	c, _ := newTestCache(t, &testClock{t: t0}, nil)
	if v := c.Snapshot(t0); len(v) != 0 {
		t.Errorf("never seen must be an empty snapshot, got %+v", v)
	}
}

func TestCache_Running(t *testing.T) {
	clk := &testClock{t: t0}
	c, _ := newTestCache(t, clk, nil)
	send(t, c, testTopic, fixture(t, "running.json"))
	v := only(t, c, t0)
	if v.Host != "testhost" || v.User != "testuser" || v.Liveness != Online || v.Age != 10*time.Second {
		t.Errorf("view = %+v", v)
	}
	if v.Unsupported || v.ClockSkew || v.Degraded || v.StateError || v.AuthProblem || v.Problem != "" {
		t.Errorf("no overlay expected: %+v", v)
	}
	if got := only(t, c, t0.Add(2*time.Minute)).Liveness; got != Stale {
		t.Errorf("2 min later: %v, want stale", got)
	}
	if got := only(t, c, t0.Add(16*time.Minute)).Liveness; got != Offline {
		t.Errorf("16 min later: %v, want offline", got)
	}
}

func TestCache_Overlays(t *testing.T) {
	tests := []struct {
		file                          string
		stateErr, authProblem, degrad bool
	}{
		{"error.json", true, false, false},
		{"auth_token_invalid.json", false, true, false},
		{"schema_2_7_extra.json", false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			c, _ := newTestCache(t, &testClock{t: t0}, nil)
			send(t, c, testTopic, fixture(t, tt.file))
			v := only(t, c, t0)
			if v.StateError != tt.stateErr || v.AuthProblem != tt.authProblem || v.Degraded != tt.degrad {
				t.Errorf("overlays = %+v", v)
			}
			if v.Liveness != Online && v.Liveness != Stale {
				t.Errorf("liveness = %v", v.Liveness)
			}
		})
	}
}

// A retained document delivered on subscribe is judged by its own ts, never by
// the time it was received (contract 7.1).
func TestCache_RetainedOldDocumentIsNotFresh(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 11, 15, 16, 0, 0, 0, time.UTC)} // myhome starts 88 min after the doc
	c, _ := newTestCache(t, clk, nil)
	send(t, c, testTopic, fixture(t, "running.json"))
	for _, d := range []time.Duration{0, time.Minute, 14 * time.Minute} {
		if v := only(t, c, clk.Now().Add(d)); v.Liveness == Online {
			t.Errorf("+%v: a retained old document looks fresh", d)
		}
	}
	v := only(t, c, clk.Now().Add(15*time.Minute+time.Second))
	if v.Liveness != Offline {
		t.Errorf("must go offline 15 min after process start, got %v", v.Liveness)
	}
	if v.LastTs.Hour() != 14 {
		t.Errorf("LastTs = %v, want the payload ts", v.LastTs)
	}
	// re-delivery of the same retained message (client reconnect) changes nothing
	clk.Add(10 * time.Minute)
	send(t, c, testTopic, fixture(t, "running.json"))
	if got := only(t, c, clk.Now()).Liveness; got == Online {
		t.Error("re-delivered retained document refreshed liveness")
	}
}

func TestCache_ClockSkewUsesReceiptTime(t *testing.T) {
	clk := &testClock{t: t0}
	c, _ := newTestCache(t, clk, nil)
	future := strings.Replace(string(fixture(t, "running.json")), "2026-11-15T14:32:00Z", "2026-11-15T14:42:00Z", 1)
	send(t, c, testTopic, []byte(future))
	v := only(t, c, t0)
	if !v.ClockSkew || !v.LastTs.Equal(t0) || v.Liveness != Online {
		t.Errorf("skewed view = %+v", v)
	}
	if got := only(t, c, t0.Add(2*time.Minute)).Liveness; got != Stale {
		t.Errorf("a silent skewed instance must still go stale, got %v", got)
	}
	// skew clears when ts is sane again (older than the receipt time is fine: lastTs was a receipt time)
	send(t, c, testTopic, fixture(t, "running.json"))
	if v = only(t, c, t0); v.ClockSkew || v.LastTs.Minute() != 32 {
		t.Errorf("skew must clear: %+v", v)
	}
	// within tolerance (+120 s) is not skew
	near := strings.Replace(string(fixture(t, "running.json")), "2026-11-15T14:32:00Z", "2026-11-15T14:34:10Z", 1)
	send(t, c, testTopic, []byte(near))
	if v = only(t, c, t0); v.ClockSkew || v.LastTs.Minute() != 34 {
		t.Errorf("120 s ahead must not be skew: %+v", v)
	}
}

func TestCache_OlderDocumentDoesNotOverwriteNewer(t *testing.T) {
	c, _ := newTestCache(t, &testClock{t: t0}, nil)
	newer := strings.Replace(string(fixture(t, "running.json")), "2026-11-15T14:32:00Z", "2026-11-15T14:32:05Z", 1)
	send(t, c, testTopic, []byte(newer))
	send(t, c, testTopic, fixture(t, "error.json")) // ts 14:32:00, older
	v := only(t, c, t0)
	if v.LastTs.Second() != 5 || v.StateError || v.Status.State != StateRunning {
		t.Errorf("an older document replaced a newer one: %+v", v)
	}
	send(t, c, testTopic, []byte(strings.Replace(string(fixture(t, "error.json")), "14:32:00Z", "14:32:05Z", 1)))
	if !only(t, c, t0).StateError {
		t.Error("a document with the same ts is accepted")
	}
}

// A retained message redelivered after the instance went offline (client
// reconnect, broker restart) must not bring it back.
func TestCache_RetainedRedeliveryWhileOfflineStaysOffline(t *testing.T) {
	clk := &testClock{t: t0}
	c, _ := newTestCache(t, clk, nil)
	send(t, c, testTopic, fixture(t, "running.json"))
	clk.Add(20 * time.Minute)
	if got := only(t, c, clk.Now()).Liveness; got != Offline {
		t.Fatalf("setup: %v", got)
	}
	send(t, c, testTopic, fixture(t, "running.json"))
	if got := only(t, c, clk.Now()).Liveness; got != Offline {
		t.Errorf("redelivery revived the instance: %v", got)
	}
	send(t, c, testTopic, fixture(t, "error.json")) // even an older one
	if got := only(t, c, clk.Now()).Liveness; got != Offline {
		t.Errorf("older redelivery revived the instance: %v", got)
	}
}

func TestCache_UnexpectedTopicCountedAndLoggedOnce(t *testing.T) {
	clk := &testClock{t: t0}
	c, ll := newTestCache(t, clk, nil)
	send(t, c, "homedrive//u/status", fixture(t, "running.json"))
	send(t, c, "homedrive/h/"+strings.Repeat("u", 200)+"/status", fixture(t, "running.json"))
	if c.BadTopics() != 2 || ll.count("unexpected topic") != 1 {
		t.Errorf("badTopics=%d logs=%d", c.BadTopics(), ll.count("unexpected topic"))
	}
	clk.Add(time.Hour + time.Second)
	send(t, c, "homedrive//u/status", fixture(t, "running.json"))
	if ll.count("unexpected topic") != 2 {
		t.Error("must log again after an hour")
	}
	if len(c.Snapshot(clk.Now())) != 0 {
		t.Error("nothing is cached for a bad topic")
	}
}

func TestCache_UnsupportedSchemaLivenessOnly(t *testing.T) {
	clk := &testClock{t: t0}
	c, _ := newTestCache(t, clk, nil)
	send(t, c, testTopic, fixture(t, "schema_3_0.json"))
	v := only(t, c, t0)
	if !v.Unsupported || v.Liveness != Online || v.StateError || v.AuthProblem || v.Degraded {
		t.Errorf("view = %+v", v)
	}
	if got := only(t, c, t0.Add(16*time.Minute)).Liveness; got != Offline {
		t.Errorf("unsupported schema still goes offline by ts: %v", got)
	}
	// without any ts there is no liveness at all, and the previous one is kept
	send(t, c, testTopic, []byte(`{"schema":"3.0"}`))
	if v = only(t, c, t0); !v.Unsupported || !v.LastTs.Equal(time.Date(2026, 11, 15, 14, 32, 0, 0, time.UTC)) {
		t.Errorf("view = %+v", v)
	}
}

func TestCache_BadPayloads(t *testing.T) {
	clk := &testClock{t: t0}
	c, ll := newTestCache(t, clk, nil)
	big := []byte(`{"schema":"2.0","ts":"2026-11-15T14:32:00Z","version":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`)

	// unknown instance: nothing is created
	send(t, c, testTopic, []byte(`{nope`))
	send(t, c, testTopic, big)
	if len(c.Snapshot(t0)) != 0 || c.Invalid() != 2 {
		t.Fatalf("instances=%d invalid=%d", len(c.Snapshot(t0)), c.Invalid())
	}

	// known instance: the last good document is kept and the problem flagged
	send(t, c, testTopic, fixture(t, "running.json"))
	send(t, c, testTopic, []byte(`{nope`))
	v := only(t, c, t0)
	if v.Problem != ProblemInvalid || v.Status == nil || v.Status.State != StateRunning {
		t.Errorf("view = %+v", v)
	}
	send(t, c, testTopic, big)
	if v = only(t, c, t0); v.Problem != ProblemTooLarge {
		t.Errorf("problem = %q", v.Problem)
	}
	send(t, c, testTopic, fixture(t, "running.json"))
	if v = only(t, c, t0); v.Problem != "" {
		t.Errorf("a good document must clear the problem: %q", v.Problem)
	}

	// at most one log line per instance per hour
	if n := ll.count("invalid homedrive status"); n != 1 {
		t.Errorf("logged %d times, want 1", n)
	}
	clk.Add(time.Hour + time.Second)
	send(t, c, testTopic, []byte(`{nope`))
	if n := ll.count("invalid homedrive status"); n != 2 {
		t.Errorf("logged %d times after an hour, want 2", n)
	}
}

func TestCache_InvalidLogTrackingIsBounded(t *testing.T) {
	c, ll := newTestCache(t, &testClock{t: t0}, nil)
	for i := 0; i < maxLogTracked+10; i++ {
		send(t, c, fmt.Sprintf("homedrive/h%d/u/status", i), []byte(`{nope`))
	}
	if n := ll.count("invalid homedrive status"); n != maxLogTracked {
		t.Errorf("logged %d, want %d", n, maxLogTracked)
	}
	if len(c.logged) != maxLogTracked {
		t.Errorf("tracked %d keys", len(c.logged))
	}
}

func TestCache_InstanceCap(t *testing.T) {
	c, ll := newTestCache(t, &testClock{t: t0}, nil)
	for i := 0; i < MaxInstances+3; i++ {
		send(t, c, fmt.Sprintf("homedrive/host%d/u/status", i), fixture(t, "running.json"))
	}
	v := c.Snapshot(t0)
	if len(v) != MaxInstances {
		t.Fatalf("len = %d, want %d", len(v), MaxInstances)
	}
	if v[0].Host != "host0" || v[MaxInstances-1].Host != "host7" {
		t.Errorf("sorted hosts: %s .. %s", v[0].Host, v[MaxInstances-1].Host)
	}
	if n := ll.count("instance limit reached"); n != 1 {
		t.Errorf("logged %d times, want once", n)
	}
	// an existing instance still updates at the cap
	send(t, c, "homedrive/host0/u/status", fixture(t, "error.json"))
	if !c.Snapshot(t0)[0].StateError {
		t.Error("existing instance must keep updating at the cap")
	}
}

func TestParseTopic(t *testing.T) {
	long := strings.Repeat("a", maxNameLen+1)
	tests := []struct {
		topic string
		want  Key
		ok    bool
	}{
		{"homedrive/h/u/status", Key{"h", "u"}, true},
		{"homedrive/h/u/online", Key{}, false},
		{"homedrive/h/u/events/pull.success", Key{}, false},
		{"homedrive/h/status", Key{}, false},
		{"other/h/u/status", Key{}, false},
		{"homedrive//u/status", Key{}, false},
		{"homedrive/h//status", Key{}, false},
		{"homedrive/" + long + "/u/status", Key{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseTopic(tt.topic)
		if ok != tt.ok || got != tt.want {
			t.Errorf("%q: got %v %v", tt.topic, got, ok)
		}
	}
	c, _ := newTestCache(t, &testClock{t: t0}, nil)
	send(t, c, "homedrive/h/u/events/push.success", fixture(t, "running.json"))
	if len(c.Snapshot(t0)) != 0 {
		t.Error("a non-status topic must be ignored")
	}
}

func TestCache_Persistence(t *testing.T) {
	db := memDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	clk := &testClock{t: t0}
	c, _ := newTestCache(t, clk, store)
	raw := fixture(t, "running.json")
	send(t, c, testTopic, raw)

	rows, _ := store.Load()
	if len(rows) != 1 || rows[0].Host != "testhost" || rows[0].User != "testuser" ||
		rows[0].LastTs != time.Date(2026, 11, 15, 14, 32, 0, 0, time.UTC).Unix() || rows[0].LastStatus != string(raw) {
		t.Fatalf("rows = %+v", rows)
	}

	// restart simulation: a new cache, 18 min later, on the same database
	clk2 := &testClock{t: t0.Add(18 * time.Minute)}
	c2, _ := newTestCache(t, clk2, store)
	v := only(t, c2, clk2.Now())
	if v.Status == nil || v.Status.LastError == nil || v.Status.LastError.Op != "push" || v.Status.Auth.State != AuthOK {
		t.Fatalf("the last payload must survive the restart: %+v", v.Status)
	}
	if v.Age != 18*time.Minute+10*time.Second || v.Liveness != Stale {
		t.Errorf("age/liveness = %v/%v, want 18m10s/stale (offline only 15 min after the new start)", v.Age, v.Liveness)
	}
	if got := only(t, c2, clk2.Now().Add(15*time.Minute+time.Second)).Liveness; got != Offline {
		t.Errorf("a dead instance must be offline after the restart grace, got %v", got)
	}
	// "never seen" is no longer the answer for a persisted instance
	if len(c2.Snapshot(clk2.Now())) != 1 {
		t.Error("persisted instance missing")
	}
}

func TestCache_PersistThrottle(t *testing.T) {
	db := memDB(t)
	store, _ := NewStore(db)
	clk := &testClock{t: t0}
	c, _ := newTestCache(t, clk, store)

	sendAt := func(ts string) {
		doc := strings.Replace(string(fixture(t, "running.json")), "2026-11-15T14:32:00Z", ts, 1)
		send(t, c, testTopic, []byte(doc))
	}
	stored := func() int64 { r, _ := store.Load(); return r[0].LastTs }
	minute := func(m int) int64 { return time.Date(2026, 11, 15, 14, m, 0, 0, time.UTC).Unix() }

	sendAt("2026-11-15T14:32:00Z") // first status after start: written
	if stored() != minute(32) {
		t.Fatal("first status must be written at once")
	}
	clk.Add(30 * time.Second)
	sendAt("2026-11-15T14:32:30Z")
	clk.Add(4 * time.Minute) // 4 min 30 s after the write
	sendAt("2026-11-15T14:36:30Z")
	if stored() != minute(32) {
		t.Fatalf("throttled: row must be unchanged inside 5 min, got %v", time.Unix(stored(), 0).UTC())
	}
	clk.Add(30 * time.Second) // exactly 5 min after the write
	sendAt("2026-11-15T14:37:10Z")
	if got := time.Unix(stored(), 0).UTC(); got.Minute() != 37 {
		t.Fatalf("5 min later the row must be rewritten, got %v", got)
	}
	// in memory the latest document is always the one shown
	if v := only(t, c, clk.Now()); v.LastTs.Minute() != 37 {
		t.Errorf("memory must not be throttled: %v", v.LastTs)
	}
	// shutdown flush writes the latest regardless of the throttle
	clk.Add(time.Minute)
	sendAt("2026-11-15T14:38:10Z")
	if got := time.Unix(stored(), 0).UTC(); got.Minute() != 37 {
		t.Fatalf("still throttled, got %v", got)
	}
	c.Flush()
	if got := time.Unix(stored(), 0).UTC(); got.Minute() != 38 {
		t.Errorf("flush must write the latest, got %v", got)
	}
}

func TestCache_PersistFailureKeepsMemory(t *testing.T) {
	db := memDB(t)
	store, _ := NewStore(db)
	c, ll := newTestCache(t, &testClock{t: t0}, store)
	db.Close()
	send(t, c, testTopic, fixture(t, "running.json"))
	if only(t, c, t0).Liveness != Online {
		t.Error("memory operation must continue when SQLite fails")
	}
	if ll.count("cannot persist") != 1 {
		t.Errorf("want one logged persist error, lines=%v", ll.lines)
	}
	c.Flush()
	c.Prune(t0.Add(ForgetAfter * 2))
}

func TestCache_LoadCapAndBadRows(t *testing.T) {
	db := memDB(t)
	store, _ := NewStore(db)
	for i := 0; i < MaxInstances+2; i++ {
		_ = store.Save(Row{Host: fmt.Sprintf("h%d", i), User: "u", LastTs: t0.Unix() - int64(i), LastStatus: string(fixture(t, "running.json"))})
	}
	_ = store.Save(Row{Host: "garbled", User: "u", LastTs: t0.Unix() + 10, LastStatus: `{nope`})
	c, _ := newTestCache(t, &testClock{t: t0}, store)
	v := c.Snapshot(t0)
	if len(v) != MaxInstances {
		t.Fatalf("loaded %d, want %d", len(v), MaxInstances)
	}
	var garbled *View
	for i := range v {
		if v[i].Host == "garbled" {
			garbled = &v[i]
		}
	}
	if garbled == nil || garbled.Status != nil {
		t.Fatalf("an unparsable row is kept, without a status: %+v", garbled)
	}
	// a database that cannot be read leaves an empty cache, not a crash
	db.Close()
	c2, ll := newTestCache(t, &testClock{t: t0}, store)
	if len(c2.Snapshot(t0)) != 0 || ll.count("cannot load") != 1 {
		t.Error("load failure must be logged and tolerated")
	}
}

func TestCache_PruneForgetsAfter30Days(t *testing.T) {
	db := memDB(t)
	store, _ := NewStore(db)
	c, _ := newTestCache(t, &testClock{t: t0}, store)
	send(t, c, testTopic, fixture(t, "running.json"))
	last := time.Date(2026, 11, 15, 14, 32, 0, 0, time.UTC)

	c.Prune(last.Add(ForgetAfter))
	if len(c.Snapshot(t0)) != 1 {
		t.Fatal("exactly 30 days: still tracked")
	}
	c.Prune(last.Add(ForgetAfter + time.Second))
	if len(c.Snapshot(t0)) != 0 {
		t.Fatal("over 30 days: forgotten")
	}
	if rows, _ := store.Load(); len(rows) != 0 {
		t.Errorf("row must be deleted: %+v", rows)
	}
}

func TestCache_RunFlushesOnShutdown(t *testing.T) {
	db := memDB(t)
	store, _ := NewStore(db)
	clk := &testClock{t: t0}
	c, _ := newTestCache(t, clk, store)
	send(t, c, testTopic, fixture(t, "running.json"))
	clk.Add(time.Minute)
	send(t, c, testTopic, []byte(strings.Replace(string(fixture(t, "running.json")), "14:32:00Z", "14:33:00Z", 1)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if rows, _ := store.Load(); rows[0].LastTs != time.Date(2026, 11, 15, 14, 33, 0, 0, time.UTC).Unix() {
		t.Errorf("shutdown flush missing: %+v", rows)
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	c, _ := newTestCache(t, &testClock{t: t0}, nil)
	raw := fixture(t, "running.json")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.HandleMessage(testTopic, raw, "")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.Snapshot(t0)
			}
		}()
	}
	wg.Wait()
}

// Schema 2.1 guards: the contract fixtures set the overlays, and the guards
// survive a restart through the persisted raw document.
func TestCache_GuardOverlays(t *testing.T) {
	tests := []struct {
		file                          string
		rootBlocked, deletesHeld, deg bool
	}{
		{"contract/status-running.json", false, false, false},
		{"contract/status-root-blocked.json", true, false, false},
		{"contract/status-delete-blocked.json", false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			store, err := NewStore(memDB(t))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 11, 15, 12, 33, 23, 0, time.UTC) // 10 s after the fixtures' ts
			clk := &testClock{t: now}
			c, _ := newTestCache(t, clk, store)
			send(t, c, testTopic, fixture(t, tt.file))
			check := func(v View) {
				t.Helper()
				if v.RootBlocked != tt.rootBlocked || v.DeletesHeld != tt.deletesHeld || v.Degraded != tt.deg || v.StateError {
					t.Errorf("overlays = %+v", v)
				}
				if v.Status == nil || v.Status.Guards == nil {
					t.Errorf("guards lost: %+v", v.Status)
				}
			}
			check(only(t, c, now))
			c2, _ := newTestCache(t, &testClock{t: now}, store)
			check(only(t, c2, now))
		})
	}
}

// Future values (a state or guard reason a newer 2.x producer may add) must not
// break the parse; an unknown state is degraded, as before.
func TestParse_FutureGuardValues(t *testing.T) {
	base := string(fixture(t, "contract/status-running.json"))
	tests := []struct {
		name, from, to string
		degraded       bool
	}{
		{"unknown state", `"state": "running"`, `"state": "quota_blocked"`, true},
		{"unknown root reason", `"local_root": null`, `"local_root": {"reason": "read_only", "since": "2026-11-15T12:30:04Z"}`, false},
		{"unknown guard key", `"guards": {`, `"guards": {"quota": {"reason": "x"},`, false},
		{"unknown deletes reason", `"bisync_deletes": null`, `"bisync_deletes": {"reason": "new", "planned": 1, "threshold": 2, "since": "2026-11-15T12:30:04Z"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := strings.Replace(base, tt.from, tt.to, 1)
			if doc == base {
				t.Fatal("fixture no longer contains the replaced text")
			}
			c, _ := newTestCache(t, &testClock{t: time.Date(2026, 11, 15, 12, 33, 23, 0, time.UTC)}, nil)
			send(t, c, testTopic, []byte(doc))
			v := only(t, c, time.Date(2026, 11, 15, 12, 33, 23, 0, time.UTC))
			if v.Status == nil || v.Unsupported || v.Degraded != tt.degraded {
				t.Errorf("view = %+v", v)
			}
		})
	}
}
