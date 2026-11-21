package homedrive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asnowfix/home-automation/myhome/alert"
)

// Alert source and rule names (#843: lowercase [a-z0-9._-]). The rule names are part of the topic
// <instance>/alert/<rule>/<key>, so renaming one orphans its open alerts.
const (
	AlertSource = "homedrive"

	RuleOffline     = "homedrive.offline"      // R1
	RuleRestartLoop = "homedrive.restart-loop" // R2
	RuleAuthBroken  = "homedrive.auth-broken"  // R3
	RuleAuthRefresh = "homedrive.auth-refresh" // R3b
	RuleAuthUnknown = "homedrive.auth-unknown" // R3c
	RuleSyncFailing = "homedrive.sync-failing" // R4
	RuleBisync      = "homedrive.bisync"       // R5

	RuleRootBlocked    = "homedrive.root-blocked"    // R6, schema 2.1 guards.local_root
	RuleDeletesBlocked = "homedrive.deletes-blocked" // R7, schema 2.1 guards.bisync_deletes
	RuleSchema         = "homedrive.schema"          // R8, a status schema major myhome does not understand
)

// Thresholds of the rules (design section 3; contract section 7). Constants, not configuration.
const (
	restartLoopStarts = 3
	syncErrors1h      = 5
	pollStuckAfter    = 15 * time.Minute
	bisyncStaleAfter  = 3 * time.Hour
)

// alertRule couples the core's per-rule policy with the condition.
type alertRule struct {
	alert.Rule
	// freeze reports that the instance's document cannot be trusted for this rule: the core then
	// neither fires nor resolves its key. nil never freezes (R1 reads liveness, not content).
	freeze  func(v View) bool
	eval    func(v View, now time.Time) reading
	cleared string // detail of the resolved document
}

// reading is what one rule concludes about one instance. detail is built from fixed templates with
// names, times and counts only: never last_error.message, never a path or file name.
type reading struct {
	active bool
	detail string
	noun   string // summary template: "homedrive <noun>: <user>@<host>"
}

// untrusted is the freeze of the content rules (R2-R7): they read the status document, so they must
// not fire or resolve from one that is stale, offline, of an unknown major, or unparsable. R1 owns
// that case.
func untrusted(v View) bool {
	return v.Liveness != Online || v.Status == nil || v.Unsupported
}

func offlineOnly(v View) bool { return v.Liveness != Online }

// alertRules is the homedrive rule set (design section 3).
func alertRules() []alertRule {
	crit := func(name string, f time.Duration) alert.Rule {
		return alert.Rule{Name: name, Source: AlertSource, Severity: alert.Critical, For: f}
	}
	warn := func(name string, f time.Duration) alert.Rule {
		return alert.Rule{Name: name, Source: AlertSource, Severity: alert.Warning, For: f}
	}
	return []alertRule{
		{Rule: crit(RuleOffline, 0), eval: evalOffline, cleared: "homedrive is reporting again."},
		{Rule: crit(RuleRestartLoop, 0), freeze: untrusted, eval: evalRestartLoop, cleared: "homedrive is no longer restarting repeatedly."},
		{Rule: crit(RuleAuthBroken, 0), freeze: untrusted, eval: evalAuthBroken, cleared: "Google sign-in is working again."},
		// refresh_failing may be a transient Google 5xx, hence 10 min where the contract says immediately.
		{Rule: crit(RuleAuthRefresh, 10*time.Minute), freeze: untrusted, eval: evalAuthRefresh, cleared: "Google token refresh is working again."},
		{Rule: warn(RuleAuthUnknown, time.Hour), freeze: untrusted, eval: evalAuthUnknown, cleared: "Google sign-in state is known again."},
		// 5 min on top of the contract's rule absorbs one bad poll cycle.
		{Rule: crit(RuleSyncFailing, 5*time.Minute), freeze: untrusted, eval: evalSyncFailing, cleared: "Sync is working again."},
		{Rule: warn(RuleBisync, 0), freeze: untrusted, eval: evalBisync, cleared: "The bisync safety net is healthy again."},
		// R6 and R7 are the schema 2.1 guards (home-drive#144): sync is deliberately not making progress
		// and needs a human. 5 min absorbs a mount that is not ready yet at boot.
		{Rule: crit(RuleRootBlocked, 5*time.Minute), freeze: untrusted, eval: evalRootBlocked, cleared: "The local root is available again."},
		{Rule: crit(RuleDeletesBlocked, 0), freeze: untrusted, eval: evalDeletesBlocked, cleared: "Bisync deletions are no longer held back."},
		// Open content alerts of an instance on an unknown major stay frozen (contract 7.8: no alert from
		// contents we cannot read); this warning says why, so they never hang silently.
		{Rule: warn(RuleSchema, time.Hour), freeze: offlineOnly, eval: evalSchema, cleared: "The homedrive status schema is supported again."},
	}
}

// evalOffline decides from the age of the last document, even right after a myhome restart: an
// instance whose last document is old stays offline across the restart (the core's startup grace
// holds a new firing, and a fresh document arrives within a publish interval if it is alive).
// Classify's "count from consumer start" is only for an instance that has not sent anything yet.
func evalOffline(v View, now time.Time) reading {
	r := reading{noun: "offline", active: v.Liveness == Offline}
	if v.LastTs.IsZero() {
		r.detail = "No status received since myhome started."
		return r
	}
	age := now.Sub(v.LastTs)
	r.active = age > OfflineAfter
	r.detail = fmt.Sprintf("No status since %s (%d min).", clock(v.LastTs), int(age.Minutes()))
	return r
}

func evalRestartLoop(v View, _ time.Time) reading {
	st := v.Status
	starts := windowStarts(st)
	r := reading{noun: "restart loop", active: starts >= restartLoopStarts || st.State == StateError}
	r.detail = fmt.Sprintf("%d starts in the last hour", starts)
	if st.State == StateError {
		r.detail += "; state error"
	}
	r.detail += "." + failingStep(st.LastError)
	return r
}

func evalAuthBroken(v View, _ time.Time) reading {
	a := authState(v.Status)
	// Contract section 7.5: anything outside {ok, unknown} is an alert; refresh_failing has its own rule.
	return reading{noun: "needs Google sign-in", active: !authFine(a) && a != AuthRefreshFailing,
		detail: fmt.Sprintf("Auth state %s. Re-authorise homedrive on its host.", knownAuth(a))}
}

func evalAuthRefresh(v View, _ time.Time) reading {
	return reading{noun: "cannot refresh its Google token", active: authState(v.Status) == AuthRefreshFailing,
		detail: "Auth state refresh_failing. Check the network and the Google account."}
}

// evalAuthUnknown fires only on an explicit auth.state "unknown". An absent or null `auth` means the
// producer does not report it yet (status-v2 section 12, until home-drive#142): that is not "unknown",
// and raising a warning an hour after every deployment would be noise. starting counts as running so a
// restart does not reset the 1 h clock.
func evalAuthUnknown(v View, _ time.Time) reading {
	st := v.Status
	up := st.State == StateRunning || st.State == StateStarting
	return reading{noun: "Google sign-in unverified", active: up && st.Auth != nil && st.Auth.State == AuthUnknown,
		detail: "Auth state still unknown while running."}
}

func evalSyncFailing(v View, now time.Time) reading {
	st := v.Status
	r := reading{noun: "sync failing"}
	if side, n := failingByCounts(st); n > 0 {
		r.active = true
		r.detail = fmt.Sprintf("%d %sfailures in the last hour, none succeeded since.%s", n, side, failingStep(st.LastError))
	}
	// The pull loop is stuck or backed off: no successful Changes poll for pollStuckAfter.
	if ref := pollReference(st); st.State == StateRunning && !ref.IsZero() && now.Sub(ref) > pollStuckAfter {
		r.active = true
		if r.detail == "" {
			r.detail = fmt.Sprintf("No successful Drive poll since %s (%d min).", clock(ref), int(now.Sub(ref).Minutes()))
		}
	}
	return r
}

func evalBisync(v View, now time.Time) reading {
	st := v.Status
	r := reading{noun: "bisync not healthy"}
	if st.State == StatePaused || st.State == StateRootBlocked { // bisync is expected to be idle
		return r
	}
	if st.Bisync != nil && st.Bisync.Result == "error" {
		r.active = true
		r.detail = fmt.Sprintf("Last bisync at %s failed.", clock(st.Bisync.At.Time))
		return r
	}
	ref := time.Time{}
	if st.Bisync != nil {
		ref = st.Bisync.At.Time
	} else if st.StartedAt != nil {
		ref = st.StartedAt.Time
	}
	if !ref.IsZero() && now.Sub(ref) > bisyncStaleAfter {
		r.active = true
		r.detail = fmt.Sprintf("No bisync completed since %s (%d h).", clock(ref), int(now.Sub(ref).Hours()))
	}
	return r
}

func evalRootBlocked(v View, _ time.Time) reading {
	st := v.Status
	var g *RootGuard
	if st.Guards != nil {
		g = st.Guards.LocalRoot
	}
	reason := "unrecognised"
	if g != nil && inSet(g.Reason, RootReasonMissing, RootReasonMarkerMissing, RootReasonIdentityMismatch, RootReasonUnreadable) {
		reason = g.Reason
	}
	return reading{noun: "local root blocked", active: g != nil || st.State == StateRootBlocked,
		detail: fmt.Sprintf("Sync is paused: the local root is not the synced tree (%s).", reason)}
}

func evalDeletesBlocked(v View, _ time.Time) reading {
	st := v.Status
	r := reading{noun: "bisync deletes blocked"}
	if st.Guards == nil || st.Guards.BisyncDeletes == nil {
		return r
	}
	g := st.Guards.BisyncDeletes
	reason := "unrecognised"
	if inSet(g.Reason, DeletesReasonThreshold, DeletesReasonEmptyLocal, DeletesReasonEmptyRemote) {
		reason = g.Reason
	}
	r.active = true
	r.detail = fmt.Sprintf("Bisync held back %d deletions (limit %d, %s). Review them, then release with allow-deletes.", g.Planned, g.Threshold, reason)
	return r
}

func evalSchema(v View, _ time.Time) reading {
	return reading{noun: "status schema unsupported", active: v.Unsupported,
		detail: "myhome does not understand this homedrive status schema, so its other alerts are paused. Update myhome."}
}

func inSet(s string, set ...string) bool {
	for _, x := range set {
		if s == x {
			return true
		}
	}
	return false
}

// authState is the contract's auth.state; "" when the producer does not report auth yet.
func authState(st *Status) string {
	if st.Auth == nil {
		return ""
	}
	return st.Auth.State
}

// knownAuth echoes only states the contract defines: the value comes off the wire.
func knownAuth(s string) string {
	if inSet(s, AuthClientMissing, AuthClientMisconfigured, AuthTokenInvalid, AuthRefreshFailing) {
		return s
	}
	return "unrecognised"
}

func windowStarts(st *Status) int64 {
	if c := st.Counts[Window1h]; c != nil {
		return c.Starts
	}
	return 0
}

// failingByCounts is contract section 7.6's first signal, "failing with no success since". It returns
// the side that matched ("" for the total, "push " or "pull ") and the failure count, 0 for none.
//
// The contract's total rule stays the floor, so errors with no direction (a failed bisync pass, a
// conflict) and a 3 + 3 split still count. On top of it, a direction whose last failure is the
// document's last_error is judged against its own success clock, so steady push failures are not
// masked by pull successes, and steady download failures are not masked by successful polls.
func failingByCounts(st *Status) (string, int64) {
	c := st.Counts[Window1h]
	le := st.LastError
	if c == nil || le == nil {
		return "", 0
	}
	t := le.Ts.Time
	if c.Errors >= syncErrors1h && t.After(latest(st.LastActivity)) {
		return "", c.Errors
	}
	switch le.Op {
	case OpPush, OpPushDelete, OpDirRename, OpBisyncPush:
		if c.ErrorsUp != nil && *c.ErrorsUp >= syncErrors1h && t.After(latest(st.LastPush)) {
			return "push ", *c.ErrorsUp
		}
	case OpPoll:
		if c.ErrorsDown != nil && *c.ErrorsDown >= syncErrors1h && t.After(latest(st.LastPoll)) {
			return "pull ", *c.ErrorsDown
		}
	case OpPull, OpPullDelete, OpBisyncPull:
		if c.ErrorsDown != nil && *c.ErrorsDown >= syncErrors1h && t.After(latest(st.LastPull)) {
			return "pull ", *c.ErrorsDown
		}
	}
	return "", 0
}

func latest(ts ...*Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if t != nil && t.After(out) {
			out = t.Time
		}
	}
	return out
}

// pollReference is the last successful poll, or the process start when none happened yet.
func pollReference(st *Status) time.Time {
	if st.LastPoll != nil && !st.LastPoll.IsZero() {
		return st.LastPoll.Time
	}
	if st.StartedAt != nil {
		return st.StartedAt.Time
	}
	return time.Time{}
}

var opRE = regexp.MustCompile(`^[a-z_]{1,24}$`)

// failingStep names the failing step (the op only; the message may hold a file path and is never used).
func failingStep(le *LastError) string {
	if le == nil || !opRE.MatchString(le.Op) {
		return ""
	}
	return " Last failing step: " + le.Op + "."
}

func clock(t time.Time) string { return t.UTC().Format("15:04") + " UTC" }

// alertKey is the <key> of the alert topic: a readable [a-z0-9._-] prefix plus a hash of the exact
// (host, user), so two instances never share a fingerprint ("a-b"/"c" vs "a"/"b-c", "Pi" vs "pi").
func alertKey(k Key) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(k.Host+"-"+k.User))
	sum := sha256.Sum256([]byte(k.Host + "\x00" + k.User))
	return truncate(name, 40) + "-" + hex.EncodeToString(sum[:6])
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// summaryFor builds "homedrive <noun>: <user>@<host>"; if the name would not pass the alert
// privacy check (a host named like an IP or a MAC, say) it falls back to a generic one.
func summaryFor(noun string, k Key) string {
	named := fmt.Sprintf("homedrive %s: %s", noun, truncate(k.User+"@"+k.Host, 40))
	probe := alert.Alert{V: alert.Version, Fingerprint: "a/b", Rule: "a", Key: "b", Severity: alert.Warning,
		State: alert.Firing, Source: "x", Since: 1, Ts: 1, Summary: named}
	if probe.Validate() != nil {
		return fmt.Sprintf("homedrive %s on an instance", noun)
	}
	return named
}
