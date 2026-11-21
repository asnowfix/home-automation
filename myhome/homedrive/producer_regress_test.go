package homedrive

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/alert"
)

// statusAt parses the running fixture made healthy at now, then mutated: the unit-level twin of harness.feed.
func statusAt(t *testing.T, now time.Time, mut mutator) *Status {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(fixture(t, running), &m); err != nil {
		t.Fatal(err)
	}
	healthy(m, now)
	if mut != nil {
		mut(m, now)
	}
	b, _ := json.Marshal(m)
	st, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func onlineView(st *Status, now time.Time) View {
	return View{Host: "testhost", User: "testuser", Liveness: Online, LastTs: now, Status: st}
}

// Blocker 1: a daemon restart during an outage must not resolve R1 and open a second episode.
// Production grace, not WithGrace(0): the grace gates new firings only.
func TestRestart_OfflineAcrossRestartKeepsOneEpisode(t *testing.T) {
	h := newHarness(t, running, runningStart)
	h.prodGrace = true
	h.boot()
	h.run(time.Minute, true, nil)
	h.run(25*time.Minute, false, nil) // silent: R1 fires once the grace has passed
	if h.pub.count(RuleOffline, alert.Firing) != 1 {
		t.Fatalf("offline not firing: %+v", h.pub.docs)
	}
	since := h.pub.docs[0].Since
	h.shutdown()
	h.boot()                       // the instance is still down
	h.run(2*time.Hour, false, nil) // well past the 15 min "count from consumer start" window and the hold
	if len(h.pub.docs) != 1 {
		t.Fatalf("restart during an outage changed the alert: %+v", h.pub.docs)
	}
	h.run(time.Minute, true, nil) // it comes back
	h.run(6*time.Minute, true, nil)
	if h.pub.count(RuleOffline, alert.Resolved) != 1 || h.pub.docs[1].Since != since {
		t.Fatalf("resolve must close the original episode: %+v", h.pub.docs)
	}
}

// Blocker 2: Prune after 30 days must not silently resolve an alert of an instance still offline.
func TestPrune_KeepsInstanceWithOpenAlert(t *testing.T) {
	h := newHarness(t, running, runningStart)
	h.run(time.Minute, true, nil)
	h.run(20*time.Minute, false, nil)
	if h.pub.count(RuleOffline, alert.Firing) != 1 {
		t.Fatalf("offline not firing: %+v", h.pub.docs)
	}
	for i := 0; i < 4; i++ {
		h.clk.Add(10 * 24 * time.Hour) // 40 days of silence
		h.prod.Evaluate(context.Background())
		h.cache.Prune(h.clk.Now())
	}
	if len(h.cache.Snapshot(h.clk.Now())) != 1 {
		t.Fatal("an instance with an open alert was pruned")
	}
	h.run(10*time.Minute, false, nil)
	if len(h.pub.docs) != 1 {
		t.Fatalf("pruning resolved the outage: %+v", h.pub.docs)
	}
}

// Without an open alert the 30-day prune still applies.
func TestPrune_ForgetsInstanceWithoutOpenAlert(t *testing.T) {
	h := newHarness(t, running, runningStart)
	h.run(time.Minute, true, nil)
	h.clk.Add(31 * 24 * time.Hour)
	h.cache.Prune(h.clk.Now())
	if len(h.cache.Snapshot(h.clk.Now())) != 0 {
		t.Fatal("an instance with no open alert should be forgotten after 30 days")
	}
}

func TestFailingByCounts_Boundaries(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 33, 23, 0, time.UTC)
	old := ts(now.Add(-50 * time.Minute))
	counts := func(m map[string]any, total, up, down int) {
		set(m, "counts.1h.errors", total)
		set(m, "counts.1h.errors_up", up)
		set(m, "counts.1h.errors_down", down)
	}
	tests := []struct {
		name string
		mut  mutator
		want string // "" none, else the side
		n    int64
	}{
		{"4 total", func(m map[string]any, now time.Time) { counts(m, 4, 0, 0); set(m, "last_error.ts", ts(now)) }, "", 0},
		{"5 total", func(m map[string]any, now time.Time) { counts(m, 5, 0, 0); set(m, "last_error.ts", ts(now)) }, "", 5},
		{"3 up + 3 down (total floor)", func(m map[string]any, now time.Time) {
			counts(m, 6, 3, 3)
			set(m, "last_error.ts", ts(now))
			set(m, "last_error.op", OpPoll)
		}, "", 6},
		{"no direction (failed bisync pass)", func(m map[string]any, now time.Time) {
			counts(m, 6, 0, 0)
			set(m, "last_error.ts", ts(now))
			set(m, "last_error.op", OpBisync)
		}, "", 6},
		{"4 up, push op", func(m map[string]any, now time.Time) {
			counts(m, 4, 4, 0)
			set(m, "last_error.ts", ts(now))
			set(m, "last_activity", ts(now))
		}, "", 0},
		{"5 up, push op, pulls succeeding", func(m map[string]any, now time.Time) {
			counts(m, 5, 5, 0)
			set(m, "last_error.ts", ts(now))
			set(m, "last_push", old)
			set(m, "last_activity", ts(now)) // a pull succeeded just now
			set(m, "last_error.op", OpPush)
		}, "push ", 5},
		{"4 down, pull op", func(m map[string]any, now time.Time) {
			counts(m, 4, 0, 4)
			set(m, "last_error.ts", ts(now))
			set(m, "last_error.op", OpPull)
			set(m, "last_pull", old)
			set(m, "last_activity", ts(now))
		}, "", 0},
		{"5 down, download op, polls succeeding", func(m map[string]any, now time.Time) {
			counts(m, 5, 0, 5)
			set(m, "last_error.ts", ts(now))
			set(m, "last_error.op", OpPull)
			set(m, "last_pull", old)
			set(m, "last_poll", ts(now)) // the Changes poll keeps working
			set(m, "last_activity", ts(now))
		}, "pull ", 5},
		{"5 down, poll op, last poll after the error", func(m map[string]any, now time.Time) {
			counts(m, 5, 0, 5)
			set(m, "last_error.ts", ts(now.Add(-time.Minute)))
			set(m, "last_error.op", OpPoll)
			set(m, "last_poll", ts(now))
			set(m, "last_activity", ts(now))
		}, "", 0},
		{"5 down, poll op, no poll since", func(m map[string]any, now time.Time) {
			counts(m, 5, 0, 5)
			set(m, "last_error.ts", ts(now))
			set(m, "last_error.op", OpPoll)
			set(m, "last_poll", old)
			set(m, "last_activity", ts(now))
		}, "pull ", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := statusAt(t, now, tt.mut)
			side, n := failingByCounts(st)
			if side != tt.want || n != tt.n {
				t.Errorf("failingByCounts = (%q, %d), want (%q, %d)", side, n, tt.want, tt.n)
			}
		})
	}
}

func TestEval_ThresholdBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 33, 23, 0, time.UTC)
	poll := func(age time.Duration) mutator {
		return func(m map[string]any, now time.Time) { set(m, "last_poll", ts(now.Add(-age))) }
	}
	bisync := func(age time.Duration) mutator {
		return func(m map[string]any, now time.Time) { set(m, "bisync.at", ts(now.Add(-age))) }
	}
	starts := func(n int) mutator { return func(m map[string]any, _ time.Time) { set(m, "counts.1h.starts", n) } }
	tests := []struct {
		name string
		eval func(View, time.Time) reading
		mut  mutator
		want bool
	}{
		{"poll 14m59", evalSyncFailing, poll(14*time.Minute + 59*time.Second), false},
		{"poll 15m01", evalSyncFailing, poll(15*time.Minute + time.Second), true},
		{"bisync 2h59", evalBisync, bisync(2*time.Hour + 59*time.Minute), false},
		{"bisync 3h01", evalBisync, bisync(3*time.Hour + time.Minute), true},
		{"starts 2", evalRestartLoop, starts(2), false},
		{"starts 3", evalRestartLoop, starts(3), true},
		{"R5 skipped while root_blocked", evalBisync, func(m map[string]any, now time.Time) {
			set(m, "state", StateRootBlocked)
			bisync(5*time.Hour)(m, now)
		}, false},
		{"R5 skipped while paused", evalBisync, func(m map[string]any, now time.Time) {
			set(m, "state", StatePaused)
			bisync(5*time.Hour)(m, now)
		}, false},
		{"R3c explicit unknown", evalAuthUnknown, func(m map[string]any, _ time.Time) { set(m, "auth.state", AuthUnknown) }, true},
		{"R3c unknown while starting", evalAuthUnknown, func(m map[string]any, _ time.Time) {
			set(m, "auth.state", AuthUnknown)
			set(m, "state", StateStarting)
		}, true},
		{"R3c absent auth", evalAuthUnknown, func(m map[string]any, _ time.Time) { delete(m, "auth") }, false},
		{"R3c null auth", evalAuthUnknown, func(m map[string]any, _ time.Time) { set(m, "auth", nil) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.eval(onlineView(statusAt(t, now, tt.mut), now), now).active; got != tt.want {
				t.Errorf("active = %v, want %v", got, tt.want)
			}
		})
	}
}

// Wire values are echoed only when the contract defines them: anything else must not reach a document.
func TestEval_HostileWireValuesAreNotEchoed(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 33, 23, 0, time.UTC)
	const hostile = "../etc/passwd user"
	tests := []struct {
		name string
		eval func(View, time.Time) reading
		mut  mutator
		must string
	}{
		{"auth.state", evalAuthBroken, func(m map[string]any, _ time.Time) { set(m, "auth.state", hostile) }, "Auth state unrecognised."},
		{"guards.local_root.reason", evalRootBlocked, func(m map[string]any, now time.Time) {
			set(m, "guards.local_root", map[string]any{"reason": hostile, "since": ts(now)})
		}, "(unrecognised)"},
		{"guards.bisync_deletes.reason", evalDeletesBlocked, func(m map[string]any, now time.Time) {
			set(m, "guards.bisync_deletes", map[string]any{"reason": hostile, "planned": 3, "threshold": 1, "since": ts(now)})
		}, "unrecognised)"},
		{"last_error.op with a path", evalRestartLoop, func(m map[string]any, _ time.Time) {
			set(m, "state", "error")
			set(m, "last_error.op", "docs/a.txt")
		}, "state error."},
		{"last_error.op with spaces", evalRestartLoop, func(m map[string]any, _ time.Time) {
			set(m, "state", "error")
			set(m, "last_error.op", "push a file")
		}, "state error."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := tt.eval(onlineView(statusAt(t, now, tt.mut), now), now)
			if !rd.active {
				t.Fatal("rule should be active")
			}
			if !strings.Contains(rd.detail, tt.must) {
				t.Errorf("detail %q does not contain %q", rd.detail, tt.must)
			}
			for _, leak := range []string{"/", "passwd", "docs", "a file", "etc"} {
				if strings.Contains(rd.detail, leak) {
					t.Errorf("detail %q echoes the wire value (%q)", rd.detail, leak)
				}
			}
		})
	}
}

// Blocker/should-fix: keys are injective, so two instances never share a fingerprint.
func TestAlertKey_Injective(t *testing.T) {
	re := regexp.MustCompile(`^[a-z0-9._-]+$`)
	keys := map[string]Key{}
	for _, k := range []Key{{"a-b", "c"}, {"a", "b-c"}, {"Pi", "fix"}, {"pi", "fix"}, {"a b", "c"}, {"a.b", "c"}, {"203.0.113.20", "x"}} {
		key := alertKey(k)
		if !re.MatchString(key) || len(key) > 64 {
			t.Errorf("alertKey(%v) = %q is not a valid short key", k, key)
		}
		if other, dup := keys[key]; dup {
			t.Errorf("%v and %v share key %q", k, other, key)
		}
		keys[key] = k
	}
	if alertKey(Key{"a", "b"}) != alertKey(Key{"a", "b"}) {
		t.Error("alertKey must be stable across calls (it names the topic across restarts)")
	}
}

// Two colliding-looking instances stay independent: one offline must not be resolved by the other.
func TestRules_CollidingNamesStayIndependent(t *testing.T) {
	h := newHarness(t, running, runningStart)
	feedTo := func(topic string) {
		h.topic = topic
		h.feed(nil)
	}
	for i := 0; i < 80; i++ { // 40 min; instance A reports for the first minute only
		h.clk.Add(30 * time.Second)
		if i < 2 {
			feedTo("homedrive/a-b/c/status")
		}
		feedTo("homedrive/a/b-c/status")
		h.prod.Evaluate(context.Background())
	}
	if h.pub.count(RuleOffline, alert.Firing) != 1 || h.pub.count(RuleOffline, alert.Resolved) != 0 {
		t.Fatalf("docs %+v", h.pub.docs)
	}
}

// Content rules neither fire nor resolve from a document that is stale or of an unknown major.
func TestRules_FrozenOnStaleAndUnsupportedDocuments(t *testing.T) {
	t.Run("stale healthy document does not resolve R4", func(t *testing.T) {
		h := newHarness(t, running, runningStart)
		stuck := func(m map[string]any, now time.Time) { set(m, "last_poll", ts(now.Add(-20*time.Minute))) }
		h.run(8*time.Minute, true, stuck)
		if h.pub.count(RuleSyncFailing, alert.Firing) != 1 {
			t.Fatalf("R4 should be firing: %+v", h.pub.docs)
		}
		// A healthy-looking document that is already 5 min old on arrival: stale, so frozen.
		h.run(8*time.Minute, true, func(m map[string]any, now time.Time) { set(m, "ts", ts(now.Add(-5*time.Minute))) })
		if h.pub.count(RuleSyncFailing, alert.Resolved) != 0 {
			t.Fatalf("a stale document resolved R4: %+v", h.pub.docs)
		}
		if st := h.cache.Snapshot(h.clk.Now())[0]; st.Liveness != Stale {
			t.Fatalf("test precondition: liveness = %s, want stale", st.Liveness)
		}
	})
	t.Run("open alert survives an unsupported major, one warning says why", func(t *testing.T) {
		h := newHarness(t, running, runningStart)
		h.run(2*time.Minute, true, func(m map[string]any, _ time.Time) { set(m, "auth.state", "token_invalid") })
		h.base = "schema_3_0.json"
		h.run(70*time.Minute, true, nil)
		if h.pub.count(RuleAuthBroken, alert.Resolved) != 0 || h.pub.count(RuleSchema, alert.Firing) != 1 {
			t.Fatalf("docs %+v", h.pub.docs)
		}
		h.base = running // supported again, and healthy
		h.run(8*time.Minute, true, nil)
		if h.pub.count(RuleAuthBroken, alert.Resolved) != 1 || h.pub.count(RuleSchema, alert.Resolved) != 1 {
			t.Fatalf("both should resolve: %+v", h.pub.docs)
		}
	})
}

// A homedrive that does not report auth yet (NAS today, until home-drive#142) never raises R3c.
func TestRules_AbsentAuthNeverRaisesR3c(t *testing.T) {
	h := newHarness(t, running, runningStart)
	h.run(3*time.Hour, true, func(m map[string]any, _ time.Time) { set(m, "auth", nil) })
	if len(h.pub.docs) != 0 {
		t.Fatalf("docs %+v", h.pub.docs)
	}
}

func TestProducer_RunReturnsWhenCancelled(t *testing.T) {
	h := newHarness(t, running, runningStart)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { h.prod.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

var _ alert.Publisher = (*recPub)(nil)
