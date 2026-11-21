package homedrive

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/alert"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
)

// recPub records the documents the core publishes, refusing what Validate refuses (like the real publisher).
type recPub struct{ docs []alert.Alert }

func (p *recPub) Publish(_ context.Context, a *alert.Alert) error {
	if _, err := alert.Encode(a); err != nil {
		return err
	}
	p.docs = append(p.docs, *a)
	return nil
}

func (p *recPub) count(rule string, st alert.State) (n int) {
	for _, d := range p.docs {
		if d.Rule == rule && d.State == st {
			n++
		}
	}
	return n
}

func (p *recPub) firedRules() []string {
	set := map[string]bool{}
	for _, d := range p.docs {
		if d.State == alert.Firing {
			set[d.Rule] = true
		}
	}
	var out []string
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// Fragments of the fixtures' last_error / bisync error texts and paths: none may reach an alert document.
var forbidden = []string{"docs/a.txt", "googleapi", "backend error", "invalid_grant", "oauth", "rclone", "boom", "/"}

// harness is a daemon on one myhome.db: boot() is a (re)start, the publisher survives restarts.
type harness struct {
	t     *testing.T
	db    *sqlx.DB
	clk   *testClock
	pub   *recPub
	cache *Cache
	prod  *Producer
	base  string
	topic string
}

func newHarness(t *testing.T, base string, start time.Time) *harness {
	t.Helper()
	h := &harness{t: t, db: memDB(t), clk: &testClock{t: start}, pub: &recPub{}, base: base, topic: testTopic}
	h.boot()
	t.Cleanup(func() { // privacy: no document carries last_error.message, a path or a file name
		for _, d := range h.pub.docs {
			for _, f := range forbidden {
				if strings.Contains(strings.ToLower(d.Summary+" "+d.Detail), f) {
					t.Errorf("alert %s %s leaks %q: %q / %q", d.Fingerprint, d.State, f, d.Summary, d.Detail)
				}
			}
		}
	})
	return h
}

// boot simulates a daemon start: fresh cache (reloaded from myhome.db) and a fresh core.
func (h *harness) boot() {
	h.t.Helper()
	ds, err := NewStore(h.db)
	if err != nil {
		h.t.Fatal(err)
	}
	h.cache = NewCache(Config{Log: logr.Discard(), Now: h.clk.Now, Store: ds})
	as, err := alert.NewStore(h.db)
	if err != nil {
		h.t.Fatal(err)
	}
	ev, err := alert.NewEvaluator(context.Background(), logr.Discard(), h.pub, as, alert.WithClock(h.clk.Now), alert.WithGrace(0))
	if err != nil {
		h.t.Fatal(err)
	}
	h.prod = NewProducer(logr.Discard(), h.cache, ev, h.clk.Now)
}

// shutdown is the daemon's clean stop: the cache flushes its instances.
func (h *harness) shutdown() { h.cache.Flush() }

type mutator func(m map[string]any, now time.Time)

// set writes a dotted path (objects only) into a decoded status document.
func set(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// healthy makes the base fixture a healthy running document at now.
func healthy(m map[string]any, now time.Time) {
	set(m, "ts", ts(now))
	set(m, "last_poll", ts(now))
	set(m, "auth", map[string]any{"state": "ok"})
	set(m, "bisync.at", ts(now.Add(-10*time.Minute)))
}

func (h *harness) feed(mut mutator) {
	h.t.Helper()
	var m map[string]any
	if err := json.Unmarshal(fixture(h.t, h.base), &m); err != nil {
		h.t.Fatal(err)
	}
	now := h.clk.Now()
	if h.base == "contract/status-running.json" {
		healthy(m, now)
	} else {
		set(m, "ts", ts(now))
	}
	if mut != nil {
		mut(m, now)
	}
	b, _ := json.Marshal(m)
	send(h.t, h.cache, h.topic, b)
}

// run advances d in 30 s steps; when feeding, the producer publishes a fresh document each step.
func (h *harness) run(d time.Duration, feed bool, mut mutator) {
	h.t.Helper()
	for el := time.Duration(0); el < d; el += 30 * time.Second {
		h.clk.Add(30 * time.Second)
		if feed {
			h.feed(mut)
		}
		h.prod.Evaluate(context.Background())
	}
}

const running = "contract/status-running.json"

var runningStart = time.Date(2026, 11, 15, 12, 33, 23, 0, time.UTC)

func TestRules_FireAndResolve(t *testing.T) {
	failing := func(m map[string]any, now time.Time) {
		set(m, "counts.1h.errors", 7)
		set(m, "counts.1h.errors_up", 7)
		set(m, "last_error.ts", ts(now))
	}
	tests := []struct {
		name   string
		rule   string
		sev    alert.Severity
		forDur time.Duration
		active mutator
	}{
		{"R2 starts", RuleRestartLoop, alert.Critical, 0, func(m map[string]any, _ time.Time) { set(m, "counts.1h.starts", 3) }},
		{"R2 error state", RuleRestartLoop, alert.Critical, 0, func(m map[string]any, _ time.Time) { set(m, "state", "error") }},
		{"R3 token_invalid", RuleAuthBroken, alert.Critical, 0, func(m map[string]any, _ time.Time) { set(m, "auth.state", "token_invalid") }},
		{"R3 client_missing", RuleAuthBroken, alert.Critical, 0, func(m map[string]any, _ time.Time) { set(m, "auth.state", "client_missing") }},
		{"R3 client_misconfigured", RuleAuthBroken, alert.Critical, 0, func(m map[string]any, _ time.Time) { set(m, "auth.state", "client_misconfigured") }},
		{"R3b refresh_failing", RuleAuthRefresh, alert.Critical, 10 * time.Minute, func(m map[string]any, _ time.Time) { set(m, "auth.state", "refresh_failing") }},
		{"R3c unknown 1h", RuleAuthUnknown, alert.Warning, time.Hour, func(m map[string]any, _ time.Time) { set(m, "auth.state", "unknown") }},
		{"R3c absent auth", RuleAuthUnknown, alert.Warning, time.Hour, func(m map[string]any, _ time.Time) { delete(m, "auth") }},
		{"R4 push errors", RuleSyncFailing, alert.Critical, 5 * time.Minute, failing},
		{"R4 push errors masked by pulls", RuleSyncFailing, alert.Critical, 5 * time.Minute, func(m map[string]any, now time.Time) {
			failing(m, now)
			set(m, "last_pull", ts(now))
			set(m, "last_poll", ts(now))
			set(m, "last_activity", ts(now))
		}},
		{"R4 total errors, no split", RuleSyncFailing, alert.Critical, 5 * time.Minute, func(m map[string]any, now time.Time) {
			failing(m, now)
			delete(m["counts"].(map[string]any)["1h"].(map[string]any), "errors_up")
			delete(m["counts"].(map[string]any)["1h"].(map[string]any), "errors_down")
		}},
		{"R4 pull errors", RuleSyncFailing, alert.Critical, 5 * time.Minute, func(m map[string]any, now time.Time) {
			set(m, "counts.1h.errors", 120)
			set(m, "counts.1h.errors_down", 120)
			set(m, "last_error.ts", ts(now))
			set(m, "last_poll", ts(now.Add(-time.Minute)))
		}},
		{"R4 poll stuck", RuleSyncFailing, alert.Critical, 5 * time.Minute, func(m map[string]any, now time.Time) { set(m, "last_poll", ts(now.Add(-20*time.Minute))) }},
		{"R5 bisync error", RuleBisync, alert.Warning, 0, func(m map[string]any, _ time.Time) {
			set(m, "bisync.result", "error")
			set(m, "bisync.error", "boom: /home/u/Drive/a.txt")
		}},
		{"R5 bisync stale", RuleBisync, alert.Warning, 0, func(m map[string]any, now time.Time) { set(m, "bisync.at", ts(now.Add(-4*time.Hour))) }},
		{"R5 no bisync, old process", RuleBisync, alert.Warning, 0, func(m map[string]any, now time.Time) {
			set(m, "bisync", nil)
			set(m, "started_at", ts(now.Add(-4*time.Hour)))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, running, runningStart)
			h.run(2*time.Minute, true, nil)
			if len(h.pub.docs) != 0 {
				t.Fatalf("healthy instance alerted: %+v", h.pub.docs)
			}
			if tt.forDur > time.Minute { // `for` holds the alert back
				h.run(tt.forDur-time.Minute, true, tt.active)
				if n := len(h.pub.docs); n != 0 {
					t.Fatalf("fired %d document(s) before its for=%s", n, tt.forDur)
				}
			}
			h.run(2*time.Minute, true, tt.active)
			if h.pub.count(tt.rule, alert.Firing) != 1 {
				t.Fatalf("%s firing = %d, want 1; docs %+v", tt.rule, h.pub.count(tt.rule, alert.Firing), h.pub.docs)
			}
			var doc alert.Alert
			for _, d := range h.pub.docs {
				if d.Rule == tt.rule {
					doc = d
				}
			}
			if doc.Severity != tt.sev || doc.Source != AlertSource || doc.Key != "testhost-testuser" || doc.Labels["subject"] != "homedrive:testhost/testuser" {
				t.Errorf("document = %+v", doc)
			}
			if !strings.Contains(doc.Summary, "testuser@testhost") {
				t.Errorf("summary %q lacks the instance", doc.Summary)
			}
			h.run(10*time.Minute, true, tt.active) // still one incident, no duplicate
			if h.pub.count(tt.rule, alert.Firing) != 1 || h.pub.count(tt.rule, alert.Resolved) != 0 {
				t.Fatalf("duplicate document while still active: %+v", h.pub.docs)
			}
			h.run(4*time.Minute, true, nil) // cleared, resolve hold (5 min) not over
			if h.pub.count(tt.rule, alert.Resolved) != 0 {
				t.Fatal("resolved before the hold elapsed")
			}
			h.run(2*time.Minute, true, nil)
			if h.pub.count(tt.rule, alert.Resolved) != 1 {
				t.Fatalf("%s resolved = %d, want 1; docs %+v", tt.rule, h.pub.count(tt.rule, alert.Resolved), h.pub.docs)
			}
			if r := h.pub.docs[len(h.pub.docs)-1]; r.Since != doc.Since || r.Ended < r.Since {
				t.Errorf("resolve %+v does not close episode since=%d", r, doc.Since)
			}
		})
	}
}

func TestRules_DoNotFire(t *testing.T) {
	tests := []struct {
		name string
		mut  mutator
	}{
		{"2 starts", func(m map[string]any, _ time.Time) { set(m, "counts.1h.starts", 2) }},
		{"push failures followed by a push success", func(m map[string]any, now time.Time) {
			set(m, "counts.1h.errors", 7)
			set(m, "counts.1h.errors_up", 7)
			set(m, "last_error.ts", ts(now.Add(-time.Minute)))
			set(m, "last_push", ts(now))
		}},
		{"4 errors", func(m map[string]any, now time.Time) {
			set(m, "counts.1h.errors", 4)
			set(m, "counts.1h.errors_up", 4)
			set(m, "last_error.ts", ts(now))
		}},
		{"paused with an old bisync", func(m map[string]any, now time.Time) {
			set(m, "state", "paused")
			set(m, "bisync.at", ts(now.Add(-5*time.Hour)))
		}},
		{"unknown auth while paused", func(m map[string]any, _ time.Time) {
			set(m, "state", "paused")
			set(m, "auth.state", "unknown")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, running, runningStart)
			h.run(90*time.Minute, true, tt.mut)
			if len(h.pub.docs) != 0 {
				t.Fatalf("unexpected alerts: %+v", h.pub.docs)
			}
		})
	}
	t.Run("refresh_failing does not fire R3 (own rule)", func(t *testing.T) {
		h := newHarness(t, running, runningStart)
		h.run(20*time.Minute, true, func(m map[string]any, _ time.Time) { set(m, "auth.state", "refresh_failing") })
		if h.pub.count(RuleAuthBroken, alert.Firing) != 0 || h.pub.count(RuleAuthRefresh, alert.Firing) != 1 {
			t.Fatalf("docs %+v", h.pub.docs)
		}
	})
}

func TestOffline_FiresAfter15MinAndResolves(t *testing.T) {
	h := newHarness(t, running, runningStart)
	h.run(time.Minute, true, nil)
	h.run(14*time.Minute, false, nil) // silent: 14 min since the last document
	if len(h.pub.docs) != 0 {
		t.Fatalf("alerted before the 15 min threshold: %+v", h.pub.docs)
	}
	h.run(2*time.Minute, false, nil)
	if h.pub.count(RuleOffline, alert.Firing) != 1 {
		t.Fatalf("offline not fired: %+v", h.pub.docs)
	}
	d := h.pub.docs[0]
	if d.Severity != alert.Critical || !strings.Contains(d.Detail, "No status since") {
		t.Errorf("doc = %+v", d)
	}
	h.run(20*time.Minute, false, nil)
	if len(h.pub.docs) != 1 {
		t.Fatalf("duplicate while offline: %+v", h.pub.docs)
	}
	h.run(4*time.Minute, true, nil)
	h.run(2*time.Minute, true, nil)
	if h.pub.count(RuleOffline, alert.Resolved) != 1 || len(h.pub.docs) != 2 {
		t.Fatalf("resolve: %+v", h.pub.docs)
	}
}

// A normal restart or deploy of homedrive (seconds to a couple of minutes of silence, a start counted,
// state starting) must never alert.
func TestOffline_NormalRestartWithinHoldoffNoAlert(t *testing.T) {
	h := newHarness(t, running, runningStart)
	h.run(5*time.Minute, true, nil)
	h.run(3*time.Minute, false, nil)                                    // the unit is down
	h.run(10*time.Minute, true, func(m map[string]any, now time.Time) { // back, one start counted
		set(m, "state", "starting")
		set(m, "counts.1h.starts", 2)
		set(m, "started_at", ts(now.Add(-30*time.Second)))
	})
	h.run(2*time.Hour, true, func(m map[string]any, _ time.Time) { set(m, "counts.1h.starts", 2) })
	if len(h.pub.docs) != 0 {
		t.Fatalf("a normal restart alerted: %+v", h.pub.docs)
	}
}

// R2 to R5 neither fire nor resolve from a document that may be hours old: R1 owns that case.
func TestRules_ContentFreezesWhileOffline(t *testing.T) {
	h := newHarness(t, running, runningStart)
	h.run(2*time.Minute, true, func(m map[string]any, _ time.Time) { set(m, "auth.state", "token_invalid") })
	if h.pub.count(RuleAuthBroken, alert.Firing) != 1 {
		t.Fatal("R3 should be firing")
	}
	h.run(45*time.Minute, false, nil) // instance goes silent
	if h.pub.count(RuleOffline, alert.Firing) != 1 || h.pub.count(RuleAuthBroken, alert.Resolved) != 0 {
		t.Fatalf("offline must fire and R3 stay open: %+v", h.pub.docs)
	}
	h.run(2*time.Minute, true, nil) // back and healthy
	h.run(6*time.Minute, true, nil)
	if h.pub.count(RuleOffline, alert.Resolved) != 1 || h.pub.count(RuleAuthBroken, alert.Resolved) != 1 {
		t.Fatalf("both must resolve once reporting again: %+v", h.pub.docs)
	}
}

func TestRules_UnsupportedSchemaOnlyLiveness(t *testing.T) {
	h := newHarness(t, "schema_3_0.json", runningStart)
	h.run(90*time.Minute, true, nil)
	if len(h.pub.docs) != 0 {
		t.Fatalf("alert from an unsupported schema's contents: %+v", h.pub.docs)
	}
}

// The core's persisted clocks make a daemon restart invisible: the `for` clock is kept, no firing is
// repeated, and the resolve after a restart closes the same episode.
func TestRestart_KeepsForClockNoDuplicateAndResolves(t *testing.T) {
	h := newHarness(t, running, runningStart)
	refresh := func(m map[string]any, _ time.Time) { set(m, "auth.state", "refresh_failing") }
	h.run(6*time.Minute, true, refresh) // pending, for=10 min
	if len(h.pub.docs) != 0 {
		t.Fatalf("fired early: %+v", h.pub.docs)
	}
	h.shutdown()
	h.clk.Add(time.Minute) // the 2-day restart is quick
	h.boot()
	h.run(2*time.Minute, true, refresh) // 9 min of the condition so far: not yet
	if len(h.pub.docs) != 0 {
		t.Fatalf("for clock was reset by the restart? fired at 9 min: %+v", h.pub.docs)
	}
	h.run(2*time.Minute, true, refresh)
	if h.pub.count(RuleAuthRefresh, alert.Firing) != 1 {
		t.Fatalf("for clock lost: %+v", h.pub.docs)
	}
	since := h.pub.docs[0].Since

	h.shutdown() // restart while firing
	h.boot()
	h.run(20*time.Minute, true, refresh)
	if len(h.pub.docs) != 1 {
		t.Fatalf("duplicate firing after restart: %+v", h.pub.docs)
	}
	h.shutdown() // restart again, then the condition clears
	h.boot()
	h.run(7*time.Minute, true, nil)
	if h.pub.count(RuleAuthRefresh, alert.Resolved) != 1 || len(h.pub.docs) != 2 {
		t.Fatalf("resolve after restart: %+v", h.pub.docs)
	}
	if r := h.pub.docs[1]; r.Since != since {
		t.Errorf("resolve closes since=%d, firing was since=%d", r.Since, since)
	}
}

func TestRules_ContractFixtures(t *testing.T) {
	tests := []struct {
		file string
		now  time.Time
		want []string
	}{
		{"contract/status-error.json", time.Date(2026, 11, 15, 12, 40, 10, 0, time.UTC), []string{RuleRestartLoop}},
		{"contract/status-auth-failure.json", time.Date(2026, 11, 15, 12, 45, 10, 0, time.UTC), []string{RuleAuthBroken, RuleSyncFailing}},
		{"contract/status-running.json", runningStart, nil},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			h := newHarness(t, tt.file, tt.now)
			h.run(10*time.Minute, true, nil)
			got := h.pub.firedRules()
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("fired %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRules_RestartLoopDetailNamesOpOnly(t *testing.T) {
	h := newHarness(t, "contract/status-error.json", time.Date(2026, 11, 15, 12, 40, 10, 0, time.UTC))
	h.run(time.Minute, true, nil)
	if len(h.pub.docs) != 1 {
		t.Fatalf("docs %+v", h.pub.docs)
	}
	want := "3 starts in the last hour; state error. Last failing step: startup."
	if got := h.pub.docs[0].Detail; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
}

// A host named like an IP or a MAC must not wedge the alert: the summary falls back to a generic name.
func TestRules_HostileInstanceNameFallsBack(t *testing.T) {
	for _, host := range []string{"203.0.113.20", "aa-bb-cc-dd-ee-ff", "Host_Mixed.Case"} {
		t.Run(host, func(t *testing.T) {
			h := newHarness(t, running, runningStart)
			h.topic = "homedrive/" + host + "/fix/status"
			h.run(time.Minute, true, func(m map[string]any, _ time.Time) { set(m, "auth.state", "token_invalid") })
			if h.pub.count(RuleAuthBroken, alert.Firing) != 1 {
				t.Fatalf("docs %+v", h.pub.docs)
			}
			d := h.pub.docs[0]
			if d.Key != alertKey(Key{host, "fix"}) || d.Key != strings.ToLower(d.Key) {
				t.Errorf("key = %q", d.Key)
			}
			if strings.Contains(host, "1") && !strings.Contains(d.Summary, "on an instance") {
				t.Errorf("summary %q should not carry the host", d.Summary)
			}
		})
	}
}

func TestAlertRules_NamesAndPolicy(t *testing.T) {
	want := map[string]struct {
		sev alert.Severity
		f   time.Duration
	}{
		RuleOffline: {alert.Critical, 0}, RuleRestartLoop: {alert.Critical, 0}, RuleAuthBroken: {alert.Critical, 0},
		RuleAuthRefresh: {alert.Critical, 10 * time.Minute}, RuleAuthUnknown: {alert.Warning, time.Hour},
		RuleSyncFailing: {alert.Critical, 5 * time.Minute}, RuleBisync: {alert.Warning, 0},
	}
	rules := AlertRules()
	if len(rules) != len(want) {
		t.Fatalf("%d rules, want %d", len(rules), len(want))
	}
	for _, r := range rules {
		w, ok := want[r.Name]
		if !ok || r.Severity != w.sev || r.For != w.f || r.Source != AlertSource {
			t.Errorf("rule %+v, want %+v", r.Rule, w)
		}
	}
}
