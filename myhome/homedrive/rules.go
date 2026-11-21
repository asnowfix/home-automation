package homedrive

import (
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
)

// Thresholds of the rules (design section 3; contract section 7). Constants, not configuration.
const (
	restartLoopStarts = 3
	syncErrors1h      = 5
	pollStuckAfter    = 15 * time.Minute
	bisyncStaleAfter  = 3 * time.Hour
)

// AlertRule couples the core's per-rule policy with the condition. Eval is given a View that is
// trusted (online, supported schema, parsed) for content rules; R1 sees every View.
type AlertRule struct {
	alert.Rule
	// Content rules read the status document, so they freeze while the instance is not online
	// (R1 owns that case) rather than fire or resolve from a document that may be hours old.
	Content bool
	Eval    func(v View, now time.Time) Reading
	Cleared string // detail of the resolved document
}

// Reading is what one rule concludes about one instance. Detail is built from fixed templates with
// names, times and counts only: never last_error.message, never a path or file name.
type Reading struct {
	Active bool
	Detail string
	Noun   string // summary template: "homedrive <noun>: <user>@<host>"
}

// AlertRules is the homedrive rule set (design section 3, R1 to R5).
//
// TODO(home-drive#144): the producer is gaining guard fields for "local_root blocked" and "bisync
// deletes blocked". They are not in status-v2.md on issue-141-status-v2 as of 2026-11-21, so no
// rule exists for them: add one AlertRule each here (and Status fields) once the field names are
// published. Do not guess them.
func AlertRules() []AlertRule {
	return []AlertRule{
		{Rule: alert.Rule{Name: RuleOffline, Source: AlertSource, Severity: alert.Critical}, Eval: evalOffline,
			Cleared: "homedrive is reporting again."},
		{Rule: alert.Rule{Name: RuleRestartLoop, Source: AlertSource, Severity: alert.Critical}, Content: true, Eval: evalRestartLoop,
			Cleared: "homedrive is no longer restarting repeatedly."},
		{Rule: alert.Rule{Name: RuleAuthBroken, Source: AlertSource, Severity: alert.Critical}, Content: true, Eval: evalAuthBroken,
			Cleared: "Google sign-in is working again."},
		// refresh_failing may be a transient Google 5xx, hence 10 min where the contract says immediately.
		{Rule: alert.Rule{Name: RuleAuthRefresh, Source: AlertSource, Severity: alert.Critical, For: 10 * time.Minute}, Content: true,
			Eval: evalAuthRefresh, Cleared: "Google token refresh is working again."},
		{Rule: alert.Rule{Name: RuleAuthUnknown, Source: AlertSource, Severity: alert.Warning, For: time.Hour}, Content: true,
			Eval: evalAuthUnknown, Cleared: "Google sign-in state is known again."},
		// 5 min on top of the contract's rule absorbs one bad poll cycle.
		{Rule: alert.Rule{Name: RuleSyncFailing, Source: AlertSource, Severity: alert.Critical, For: 5 * time.Minute}, Content: true,
			Eval: evalSyncFailing, Cleared: "Sync is working again."},
		{Rule: alert.Rule{Name: RuleBisync, Source: AlertSource, Severity: alert.Warning}, Content: true,
			Eval: evalBisync, Cleared: "The bisync safety net is healthy again."},
	}
}

// trusted reports whether the status document may drive the content rules.
func trusted(v View) bool {
	return v.Liveness == Online && v.Status != nil && !v.Unsupported
}

func evalOffline(v View, _ time.Time) Reading {
	r := Reading{Noun: "offline", Active: v.Liveness == Offline}
	if v.LastTs.IsZero() {
		r.Detail = "No status received since myhome started."
	} else {
		r.Detail = fmt.Sprintf("No status since %s (%d min).", clock(v.LastTs), int(v.Age.Minutes()))
	}
	return r
}

func evalRestartLoop(v View, _ time.Time) Reading {
	st := v.Status
	starts := windowStarts(st)
	r := Reading{Noun: "restart loop", Active: starts >= restartLoopStarts || st.State == StateError}
	r.Detail = fmt.Sprintf("%d starts in the last hour", starts)
	if st.State == StateError {
		r.Detail += "; state error"
	}
	r.Detail += "." + failingStep(st.LastError)
	return r
}

func evalAuthBroken(v View, _ time.Time) Reading {
	a := authState(v.Status)
	// Contract section 7.5: anything outside {ok, unknown} is an alert; refresh_failing has its own rule.
	active := !authFine(a) && a != AuthRefreshFailing
	return Reading{Noun: "needs Google sign-in", Active: active,
		Detail: fmt.Sprintf("Auth state %s. Re-authorise homedrive on its host.", knownAuth(a))}
}

func evalAuthRefresh(v View, _ time.Time) Reading {
	return Reading{Noun: "cannot refresh its Google token", Active: authState(v.Status) == AuthRefreshFailing,
		Detail: "Auth state refresh_failing. Check the network and the Google account."}
}

func evalAuthUnknown(v View, _ time.Time) Reading {
	a := authState(v.Status)
	return Reading{Noun: "Google sign-in unverified", Active: (a == AuthUnknown || a == "") && v.Status.State == StateRunning,
		Detail: "Auth state still unknown while running."}
}

func evalSyncFailing(v View, now time.Time) Reading {
	st := v.Status
	failing, n := failingByCounts(st)
	r := Reading{Noun: "sync failing"}
	if failing {
		r.Active = true
		r.Detail = fmt.Sprintf("%d failures in the last hour, none succeeded since.%s", n, failingStep(st.LastError))
	}
	// The pull loop is stuck or backed off: no successful Changes poll for pollStuckAfter.
	if ref := pollReference(st); st.State == StateRunning && !ref.IsZero() && now.Sub(ref) > pollStuckAfter {
		r.Active = true
		if r.Detail == "" {
			r.Detail = fmt.Sprintf("No successful Drive poll since %s (%d min).", clock(ref), int(now.Sub(ref).Minutes()))
		}
	}
	return r
}

func evalBisync(v View, now time.Time) Reading {
	st := v.Status
	r := Reading{Noun: "bisync not healthy"}
	if st.State == StatePaused { // bisync is expected to be idle while paused
		return r
	}
	switch {
	case st.Bisync != nil && st.Bisync.Result == "error":
		r.Active = true
		r.Detail = fmt.Sprintf("Last bisync at %s failed.", clock(st.Bisync.At.Time))
	default:
		ref := time.Time{}
		if st.Bisync != nil {
			ref = st.Bisync.At.Time
		} else if st.StartedAt != nil {
			ref = st.StartedAt.Time
		}
		if !ref.IsZero() && now.Sub(ref) > bisyncStaleAfter {
			r.Active = true
			r.Detail = fmt.Sprintf("No bisync completed since %s (%d h).", clock(ref), int(now.Sub(ref).Hours()))
		}
	}
	return r
}

// Auth states the contract names beyond ok and unknown.
const AuthRefreshFailing = "refresh_failing"

// authState is the contract's auth.state; an absent auth is unknown (status-v2 section 12).
func authState(st *Status) string {
	if st.Auth == nil {
		return AuthUnknown
	}
	return st.Auth.State
}

var knownAuthStates = map[string]bool{"client_missing": true, "client_misconfigured": true, "token_invalid": true, AuthRefreshFailing: true}

// knownAuth echoes only states the contract defines: the value comes off the wire.
func knownAuth(s string) string {
	if knownAuthStates[s] {
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

// failingByCounts implements contract section 7.6, first signal ("failing with no success since"),
// per direction when the producer reports errors_up and errors_down (so steady push failures are not
// masked by pull successes), on the total otherwise. last_error is a single value, so a direction is
// judged by whether anything succeeded after it.
func failingByCounts(st *Status) (bool, int64) {
	c := st.Counts[Window1h]
	if c == nil || st.LastError == nil {
		return false, 0
	}
	le := st.LastError.Ts.Time
	if c.ErrorsUp == nil || c.ErrorsDown == nil {
		return c.Errors >= syncErrors1h && le.After(latest(st.LastActivity)), c.Errors
	}
	up := *c.ErrorsUp >= syncErrors1h && le.After(latest(st.LastPush))
	down := *c.ErrorsDown >= syncErrors1h && le.After(latest(st.LastPull, st.LastPoll))
	n := int64(0)
	if up {
		n += *c.ErrorsUp
	}
	if down {
		n += *c.ErrorsDown
	}
	return up || down, n
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

// alertKey is the <key> of the alert topic: [a-z0-9._-], from host and user.
func alertKey(k Key) string {
	s := strings.ToLower(k.Host + "-" + k.User)
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, s)
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
