package ui

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/myhome/homedrive"
)

// HomedriveSource is what the Drive sync panel reads: the classified state of
// every known homedrive instance, from daemon memory. Narrow on purpose: the
// handler makes no RPC and no MQTT call (AGENTS.md rule 3).
type HomedriveSource interface {
	Snapshot(now time.Time) []homedrive.View
}

// noValue is shown for an unknown or null value.
const noValue = "--"

// SetHomedriveSource wires the panel's data source (nil renders "not available").
func (h *HTMXHandler) SetHomedriveSource(src HomedriveSource) { h.homedrive = src }

// driveLine is one row of the windows table: a metric over 1 h / 24 h / 7 d.
type driveLine struct {
	Label string
	Cells [3]string
}

// driveCard is the template view of one instance, with every display value
// pre-formatted on the server.
type driveCard struct {
	Name      string // user@host
	PillClass string
	PillText  string
	Version   string
	Uptime    string
	DryRun    bool
	ClockSkew bool
	Problem   string

	Unsupported bool
	Schema      string

	Seen        string // "12 s ago (14:32)"
	State       string
	Auth        string
	AuthProblem bool
	Lines       []driveLine
	Pending     string
	LastPush    string
	LastPull    string
	LastPoll    string
	LastError   string
	Bisync      string
	Local       string
	Quota       string
}

// HomedrivePanel renders the Drive sync block of the System status sheet.
func (h *HTMXHandler) HomedrivePanel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if h.homedrive == nil {
		fmt.Fprint(w, `<p class="has-text-grey">Drive sync status not available.</p>`)
		return
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	views := h.homedrive.Snapshot(now)
	cards := make([]driveCard, len(views))
	for i, v := range views {
		cards[i] = newDriveCard(v, now)
	}
	var buf bytes.Buffer
	if err := homedrivePanelTmpl.Execute(&buf, cards); err != nil {
		h.log.Error(err, "failed to render homedrive panel")
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func newDriveCard(v homedrive.View, now time.Time) driveCard {
	c := driveCard{
		Name:        v.User + "@" + v.Host,
		Version:     noValue,
		Uptime:      noValue,
		DryRun:      false,
		ClockSkew:   v.ClockSkew,
		Problem:     v.Problem,
		Unsupported: v.Unsupported,
		State:       noValue, Auth: noValue, Pending: noValue,
		LastPush: noValue, LastPull: noValue, LastPoll: noValue,
		LastError: noValue, Bisync: noValue, Local: noValue, Quota: noValue,
		AuthProblem: v.AuthProblem,
	}
	c.PillClass, c.PillText = drivePill(v)
	c.Seen = noValue
	if !v.LastTs.IsZero() {
		c.Seen = fmt.Sprintf("%s (%s)", ago(v.Age), clock(v.LastTs, now))
	}
	st := v.Status
	if st == nil {
		return c
	}
	c.Schema = st.Schema
	if st.Unsupported {
		return c
	}
	c.fill(st, v, now)
	return c
}

// fill sets the fields that come from a readable (schema 2.x) document.
func (c *driveCard) fill(st *homedrive.Status, v homedrive.View, now time.Time) {
	c.DryRun = st.DryRun
	if st.Version != "" {
		c.Version = st.Version
	}
	if st.State != "" {
		c.State = st.State
	}
	if v.Liveness != homedrive.Offline {
		if up, ok := uptime(st); ok {
			c.Uptime = "up " + duration(up)
		}
	}
	c.Auth = authText(st.Auth, now)
	c.Lines = windowLines(st.Counts)
	if st.PendingUp != nil {
		c.Pending = strconv.FormatInt(*st.PendingUp, 10)
	}
	c.LastPush = optClock(st.LastPush, now)
	c.LastPull = optClock(st.LastPull, now)
	c.LastPoll = optClock(st.LastPoll, now)
	c.LastError = lastErrorText(st.LastError, now)
	c.Bisync = bisyncText(st.Bisync, now)
	if st.Local != nil {
		c.Local = fmt.Sprintf("%s files · %s", group(st.Local.Files), bytesText(st.Local.Bytes))
	}
	if st.QuotaUsedPct != nil {
		c.Quota = strconv.FormatFloat(*st.QuotaUsedPct, 'f', 0, 64) + " %"
	}
}

// drivePill maps an instance to the accounts tag classes (design section 2).
func drivePill(v homedrive.View) (class, text string) {
	switch {
	case v.Liveness == homedrive.Offline:
		return "is-danger", "Offline"
	case v.Unsupported:
		return "is-light", "Unsupported schema"
	case v.Liveness == homedrive.Stale:
		return "is-light", "Stale"
	case v.Status == nil:
		return "is-light", "Unknown"
	case v.StateError:
		return "is-danger", "Error"
	case v.AuthProblem:
		return "is-danger", "Auth problem"
	case v.Degraded:
		return "is-warning", v.Status.State
	}
	switch v.Status.State {
	case homedrive.StateStarting:
		return "is-light", "Starting"
	case homedrive.StatePaused:
		return "is-light", "Paused"
	}
	return "is-success", "Online"
}

func windowLines(counts map[string]*homedrive.Counts) []driveLine {
	type metric struct {
		label string
		cell  func(*homedrive.Counts) string
	}
	metrics := []metric{
		{"Uploaded", func(c *homedrive.Counts) string { return group(c.FilesUp) + " · " + bytesText(c.BytesUp) }},
		{"Downloaded", func(c *homedrive.Counts) string { return group(c.FilesDown) + " · " + bytesText(c.BytesDown) }},
		{"Errors", func(c *homedrive.Counts) string { return group(c.Errors) }},
		{"Conflicts", func(c *homedrive.Counts) string { return group(c.Conflicts) }},
		{"Starts", func(c *homedrive.Counts) string { return group(c.Starts) }},
	}
	lines := make([]driveLine, len(metrics))
	for i, m := range metrics {
		lines[i].Label = m.label
		for j, w := range homedrive.Windows {
			lines[i].Cells[j] = noValue
			if c := counts[w]; c != nil {
				lines[i].Cells[j] = m.cell(c)
			}
		}
	}
	return lines
}

func authText(a *homedrive.Auth, now time.Time) string {
	if a == nil || a.State == "" {
		return noValue
	}
	s := a.State
	if a.LastRefreshOK != nil && !a.LastRefreshOK.IsZero() {
		s += " (refreshed " + clock(a.LastRefreshOK.Time, now) + ")"
	}
	if e := a.LastRefreshError; e != nil && !e.Ts.IsZero() {
		s += ", refresh failed " + clock(e.Ts.Time, now)
	}
	return s
}

// lastErrorText is "op · time · message", the message truncated for the wall
// screen. It is shown on the kiosk only: it may contain a file path and must
// never go into a notification.
func lastErrorText(e *homedrive.LastError, now time.Time) string {
	if e == nil {
		return "none"
	}
	parts := []string{}
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	if !e.Ts.IsZero() {
		parts = append(parts, clock(e.Ts.Time, now))
	}
	if e.Message != "" {
		parts = append(parts, truncateError(e.Message))
	}
	if len(parts) == 0 {
		return noValue
	}
	return strings.Join(parts, " · ")
}

func bisyncText(b *homedrive.Bisync, now time.Time) string {
	if b == nil {
		return noValue
	}
	parts := []string{clock(b.At.Time, now)}
	if b.At.IsZero() {
		parts[0] = noValue
	}
	result := b.Result
	if result == "" {
		result = noValue
	}
	parts = append(parts, result, duration(time.Duration(b.DurationMs)*time.Millisecond), group(b.FilesChanged)+" files")
	if b.Result == "error" && b.Error != "" {
		parts = append(parts, truncateError(b.Error))
	}
	return strings.Join(parts, " · ")
}

func optClock(t *homedrive.Time, now time.Time) string {
	if t == nil || t.IsZero() {
		return noValue
	}
	return clock(t.Time, now)
}

// clock is "14:32", with the date when t is not today (in now's zone).
func clock(t, now time.Time) string {
	t = t.In(now.Location())
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("2 Jan 15:04")
}

// ago is "12 s ago", "3 min ago", "2 h 10 min ago".
func ago(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return duration(d) + " ago"
}

// duration is a short human duration: "42 s", "3 min", "2 h 10 min", "3 d 4 h".
func duration(d time.Duration) string {
	switch {
	case d < time.Second && d > 0:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return fmt.Sprintf("%d h", h)
		}
		return fmt.Sprintf("%d h %d min", h, m)
	}
	days, h := int(d.Hours())/24, int(d.Hours())%24
	if h == 0 {
		return fmt.Sprintf("%d d", days)
	}
	return fmt.Sprintf("%d d %d h", days, h)
}

func uptime(st *homedrive.Status) (time.Duration, bool) {
	switch {
	case st.UptimeS != nil:
		return time.Duration(*st.UptimeS) * time.Second, true
	case st.StartedAt != nil && !st.StartedAt.IsZero():
		return st.Ts.Sub(st.StartedAt.Time), true
	}
	return 0, false
}

// bytesText is a decimal size: "0 B", "1.2 MB".
func bytesText(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	for _, u := range []string{"kB", "MB", "GB", "TB"} {
		f /= 1000
		if f < 1000 || u == "TB" {
			return fmt.Sprintf("%.1f %s", f, u)
		}
	}
	return noValue // unreachable
}

// group formats an integer with a no-break space between thousands.
func group(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(" ")
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

var homedrivePanelTmpl = template.Must(template.New("homedrive-panel").Parse(homedrivePanelTemplate))

const homedrivePanelTemplate = `
{{range .}}
<div class="box mh-drive">
  <div class="level mb-2">
    <div class="level-left"><strong class="mh-drive-name">{{.Name}}</strong></div>
    <div class="level-right">
      {{if .DryRun}}<span class="tag is-light mr-2">Dry run</span>{{end}}
      {{if .ClockSkew}}<span class="tag is-warning mr-2">Clock skew</span>{{end}}
      <span class="tag {{.PillClass}}">{{.PillText}}</span>
    </div>
  </div>
  {{if .Problem}}<p class="mh-drive-line has-text-danger">{{.Problem}}</p>{{end}}
  <p class="mh-drive-line">Status {{.Seen}}{{if .Schema}} · schema {{.Schema}}{{end}}</p>
  {{if .Unsupported}}
    <p class="mh-drive-line">Unsupported homedrive status schema. Only its age is shown.</p>
  {{else}}
    <p class="mh-drive-line">Version {{.Version}} · {{.Uptime}} · State {{.State}}</p>
    <p class="mh-drive-line{{if .AuthProblem}} has-text-danger{{end}}">Google auth {{.Auth}}</p>
    <table class="mh-drive-table">
      <thead><tr><th scope="col"></th><th scope="col">1 h</th><th scope="col">24 h</th><th scope="col">7 d</th></tr></thead>
      <tbody>
        {{range .Lines}}<tr><th scope="row">{{.Label}}</th><td>{{index .Cells 0}}</td><td>{{index .Cells 1}}</td><td>{{index .Cells 2}}</td></tr>
        {{end}}
      </tbody>
    </table>
    <p class="mh-drive-line">Pending uploads {{.Pending}} · Last push {{.LastPush}} · Last pull {{.LastPull}} · Last poll {{.LastPoll}}</p>
    <p class="mh-drive-line mh-drive-error">Last error {{.LastError}}</p>
    <p class="mh-drive-line">Last bisync {{.Bisync}}</p>
    <p class="mh-drive-line">Local {{.Local}} · Quota {{.Quota}}</p>
  {{end}}
</div>
{{else}}
<p class="has-text-grey mh-drive-line">Never seen: no homedrive status has been received yet.</p>
{{end}}
`
