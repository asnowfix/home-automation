package ui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// Vacuum panel (#606): one card per configured robot, refreshed by polling. The
// robot pushes its state to the daemon only on change, so polling the daemon's
// cached state is cheap and never reaches the robot.

// vacuumCard is the template view of one robot.
type vacuumCard struct {
	myhome.VacuumRobot
	PhaseText string
	State     vacuumState
	Ctl       vacuumControls
	Rooms     []myhome.VacuumRegion
	Targets   []string // names of the rooms being cleaned, while a mission is open; empty for a whole-home clean
	Error     string   // last action's failure, shown on the card
	CycleText string   // the robot's cycle in words; empty when idle
	ErrorText string   // the robot's error code in words; empty when there is none
	NotReady  string   // why the robot cannot start, in words; empty when ready
	Updated   string   // when the robot last reported, e.g. "12:03:05 (2 min ago)"
}

// vacuumState is what the robot is doing, as far as the controls care.
type vacuumState string

const (
	vacOffline vacuumState = "offline" // not connected to the daemon
	vacIdle    vacuumState = "idle"    // docked or standing, no open mission
	vacRunning vacuumState = "running" // cleaning or returning
	vacPaused  vacuumState = "paused"  // mission open, robot stopped
	vacStuck   vacuumState = "stuck"   // needs help
	vacEvac    vacuumState = "evac"    // emptying its bin
)

// deriveVacuumState maps a robot's connection and phase/cycle to a vacuumState.
// An empty or "none" cycle means no mission is open, so a stopped robot is idle.
func deriveVacuumState(connected bool, st *myhome.VacuumStatus) vacuumState {
	if !connected {
		return vacOffline
	}
	if st == nil {
		return vacIdle
	}
	switch st.Phase {
	case "run", "hmUsrDock", "hmMidMsn", "hmPostMsn":
		return vacRunning
	case "stuck":
		return vacStuck
	case "evac":
		return vacEvac
	case "stop":
		if st.Cycle != "" && st.Cycle != "none" {
			return vacPaused
		}
	}
	return vacIdle
}

// vacuumControls says which global buttons are usable in a state.
type vacuumControls struct {
	CleanAll, Pause, Resume, Stop, Dock, Find, Evac bool
}

func controlsFor(s vacuumState) vacuumControls {
	switch s {
	case vacIdle:
		return vacuumControls{CleanAll: true, Dock: true, Find: true, Evac: true}
	case vacRunning:
		return vacuumControls{Pause: true, Stop: true, Dock: true, Find: true}
	case vacPaused:
		return vacuumControls{Resume: true, Stop: true, Dock: true, Find: true}
	case vacStuck:
		return vacuumControls{Stop: true, Dock: true, Find: true}
	case vacEvac:
		return vacuumControls{Dock: true, Find: true}
	}
	return vacuumControls{}
}

// derive fills the presentation fields from the robot's connection and status.
func (c *vacuumCard) derive(now time.Time) {
	c.State = deriveVacuumState(c.Connected, c.Status)
	c.Ctl = controlsFor(c.State)
	if st := c.Status; st != nil {
		c.PhaseText = myhome.VacuumPhaseText(st.Phase)
		c.CycleText = vacuumCycleText(st.Cycle)
		c.ErrorText = vacuumErrorText(st.Error)
		c.NotReady = vacuumNotReadyText(st.NotReady)
		c.Updated = vacuumUpdatedText(st.UpdatedAt, now)
	}
}

// vacuumCycleText names a robot cycle in words; unknown cycles are shown as the robot sent them.
func vacuumCycleText(cycle string) string {
	switch cycle {
	case "", "none":
		return ""
	case "clean":
		return "cleaning cycle"
	case "dock":
		return "returning to the dock"
	case "quick":
		return "quick clean"
	case "spot":
		return "spot clean"
	case "evac":
		return "emptying the bin"
	}
	return "cycle " + cycle
}

// vacuumErrorText turns a robot error code into words. The repository documents only
// code 224 (see myhome.VacuumErrorHint); every other code is shown as "code N" rather
// than guessed at (#685).
func vacuumErrorText(code int) string {
	if code == 0 {
		return ""
	}
	text := fmt.Sprintf("code %d", code)
	if hint := strings.TrimPrefix(myhome.VacuumErrorHint(code), fmt.Sprintf("error %d: ", code)); hint != "" {
		text += ": " + hint
	}
	return text
}

// vacuumNotReadyText words the robot's not-ready code; none is documented in the
// repository, so a non-zero code is shown as "code N".
func vacuumNotReadyText(code int) string {
	if code == 0 {
		return ""
	}
	return fmt.Sprintf("code %d", code)
}

// vacuumUpdatedText shows when the robot last reported, as a clock time and an age.
// A robot pushes only on change, so an old report is legitimate but is flagged.
func vacuumUpdatedText(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	age := now.Sub(t)
	clock := t.Local().Format("15:04:05")
	switch {
	case age < time.Minute:
		return clock + " (just now)"
	case age < time.Hour:
		return fmt.Sprintf("%s (%d min ago)", clock, int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%s (%d h ago, may be out of date)", clock, int(age.Hours()))
	}
	return fmt.Sprintf("%s (%d d ago, may be out of date)", t.Local().Format("2006-01-02 15:04:05"), int(age.Hours()/24))
}

type vacuumPanel struct {
	Cards []vacuumCard
}

func callVacuum(ctx context.Context, verb myhome.Verb, params any) (any, error) {
	mh, err := myhome.Methods(verb)
	if err != nil {
		return nil, err // vacuum service not registered
	}
	return mh.ActionE(ctx, params)
}

// targetNames resolves the region ids of the robot's last command to room names.
func targetNames(ids []string, rooms []myhome.VacuumRegion) []string {
	var names []string
	for _, id := range ids {
		name := "region " + id
		for _, reg := range rooms {
			if reg.ID == id && reg.Name != "" {
				name = reg.Name
				break
			}
		}
		names = append(names, name)
	}
	return names
}

func (h *HTMXHandler) vacuumPanel(actionErr map[string]string) (vacuumPanel, bool) {
	res, err := callVacuum(h.ctx, myhome.VacuumList, nil)
	if err != nil {
		return vacuumPanel{}, false
	}
	list, ok := res.(*myhome.VacuumListResult)
	if !ok || len(list.Robots) == 0 {
		return vacuumPanel{}, false
	}
	var p vacuumPanel
	for _, r := range list.Robots {
		c := vacuumCard{VacuumRobot: r, Error: actionErr[r.ID]}
		c.derive(time.Now())
		if rooms, err := callVacuum(h.ctx, myhome.VacuumRooms, &myhome.VacuumParams{Robot: r.ID}); err == nil {
			for _, reg := range rooms.(*myhome.VacuumRoomsResult).Regions {
				if !reg.Stale {
					c.Rooms = append(c.Rooms, reg)
				}
			}
		}
		if r.Status != nil && (c.State == vacRunning || c.State == vacPaused) {
			c.Targets = targetNames(r.Status.Regions, c.Rooms)
		}
		p.Cards = append(p.Cards, c)
	}
	sort.Slice(p.Cards, func(i, j int) bool { return strings.ToLower(p.Cards[i].Name) < strings.ToLower(p.Cards[j].Name) })
	return p, true
}

func (h *HTMXHandler) renderVacuum(w http.ResponseWriter, actionErr map[string]string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	p, ok := h.vacuumPanel(actionErr)
	if !ok {
		fmt.Fprint(w, `<p class="has-text-grey">No vacuum robot is configured.</p>`)
		return
	}
	tmpl := template.Must(template.New("vacuum-panel").Parse(vacuumPanelTemplate))
	if err := tmpl.Execute(w, p); err != nil {
		h.log.Error(err, "failed to render vacuum panel")
	}
}

// VacuumPanel renders the robot cards (GET /htmx/vacuum).
func (h *HTMXHandler) VacuumPanel(w http.ResponseWriter, r *http.Request) {
	h.renderVacuum(w, nil)
}

// vacuumStillApplies re-checks the daemon's cached state before a confirmed start or
// stop: the card may be up to 5 s old, or a confirm may have sat open. It returns a
// readable reason when the action no longer applies.
func (h *HTMXHandler) vacuumStillApplies(robot, command string) error {
	res, err := callVacuum(h.ctx, myhome.VacuumList, nil)
	if err != nil {
		return err
	}
	list, ok := res.(*myhome.VacuumListResult)
	if !ok {
		return fmt.Errorf("robot state unavailable")
	}
	for _, r := range list.Robots {
		if r.ID != robot && r.Name != robot {
			continue
		}
		ctl := controlsFor(deriveVacuumState(r.Connected, r.Status))
		switch {
		case !r.Connected:
			return fmt.Errorf("the robot is offline")
		case command == "start" && !ctl.CleanAll:
			return fmt.Errorf("the robot is busy (%s), nothing was started", myhome.VacuumPhaseText(r.Status.Phase))
		case command == "stop" && !ctl.Stop:
			return fmt.Errorf("the robot is not on a mission, nothing was stopped")
		}
		return nil
	}
	return fmt.Errorf("unknown robot %q", robot)
}

// VacuumAction runs a command or a room clean and re-renders the panel (POST /htmx/vacuum/action).
// With room_id (and command=clean-rooms) it cleans that daemon room's mapped areas and
// returns the room page's vacuum block instead (cleanRoom).
// A failure (e.g. the robot did not acknowledge) is shown on that robot's card.
func (h *HTMXHandler) VacuumAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	robot, command := r.FormValue("robot"), r.FormValue("command")
	if robot == "" || command == "" {
		http.Error(w, "robot and command required", http.StatusBadRequest)
		return
	}
	if roomID := r.FormValue("room_id"); roomID != "" {
		// Clean this room (#765). Every `room` field is ignored: the regions are
		// resolved on the server from the room's mapping.
		if command != "clean-rooms" {
			http.Error(w, "room_id goes with command=clean-rooms", http.StatusBadRequest)
			return
		}
		h.cleanRoom(w, robot, roomID)
		return
	}
	var err error
	switch {
	case command == "start" || command == "stop":
		if err = h.vacuumStillApplies(robot, command); err == nil {
			_, err = callVacuum(h.ctx, myhome.VacuumCommand, &myhome.VacuumCommandParams{Robot: robot, Command: command})
		}
	case command == "clean-rooms":
		if rooms := r.Form["room"]; len(rooms) == 0 {
			err = fmt.Errorf("select at least one room")
		} else {
			_, err = callVacuum(h.ctx, myhome.VacuumClean, &myhome.VacuumCleanParams{Robot: robot, Rooms: rooms})
		}
	default:
		_, err = callVacuum(h.ctx, myhome.VacuumCommand, &myhome.VacuumCommandParams{Robot: robot, Command: command})
	}
	var actionErr map[string]string
	if err != nil {
		h.log.Info("Vacuum action failed", "robot", robot, "command", command, "error", err.Error())
		actionErr = map[string]string{robot: fmt.Sprintf("%s failed: %v", command, err)}
	}
	h.renderVacuum(w, actionErr)
}

const vacuumPanelTemplate = `
{{range .Cards}}
{{$id := .ID}}{{$c := .Ctl}}
<div class="mh-vac" data-state="{{.State}}"
     x-data="{ask: '', t: 0, open(k) { this.ask = k; clearTimeout(this.t); this.t = setTimeout(() => { this.ask = '' }, 20000) }, close() { clearTimeout(this.t); this.ask = '' }}"
     :data-confirming="ask ? 'true' : null">
  <div class="mh-vac-head">
    <p class="mh-vac-name">{{.Name}}</p>
    {{if .Connected}}<span class="mh-vac-tag is-ok">connected</span>{{else}}<span class="mh-vac-tag is-bad">offline</span>{{end}}
  </div>
  {{if not .Connected}}<p class="mh-vac-note is-warn">The robot is not connected to the daemon{{if .Status}}; the figures below are the last it reported{{end}}. Controls are disabled.</p>{{end}}
  {{if .Status}}
  <p class="mh-vac-phase">{{.PhaseText}}{{if .CycleText}}<span class="mh-vac-sub"> - {{.CycleText}}</span>{{end}}</p>
  {{if .Targets}}<p class="mb-2">Cleaning: {{range $i, $t := .Targets}}{{if $i}}, {{end}}{{$t}}{{end}}</p>{{end}}
  <progress class="progress {{if lt .Status.Battery 20}}is-danger{{else if lt .Status.Battery 50}}is-warning{{else}}is-success{{end}}"
            value="{{.Status.Battery}}" max="100">{{.Status.Battery}}%</progress>
  <div class="mh-vac-tags">
    <span class="mh-vac-tag">battery {{.Status.Battery}}%</span>
    {{if not .Status.BinPresent}}<span class="mh-vac-tag is-bad">bin missing</span>
    {{else if .Status.BinFull}}<span class="mh-vac-tag is-warn">bin full</span>
    {{else}}<span class="mh-vac-tag">bin ok</span>{{end}}
    {{if gt .Status.MissionMinutes 0}}<span class="mh-vac-tag">mission {{.Status.MissionMinutes}} min</span>{{end}}
  </div>
  {{if .ErrorText}}<p class="mh-vac-note is-bad">Error: {{.ErrorText}}</p>{{end}}
  {{if .NotReady}}<p class="mh-vac-note is-warn">Not ready to start: {{.NotReady}}</p>{{end}}
  <p class="mh-vac-sub mb-3">Last update: {{.Updated}}</p>
  {{else}}
  <p class="has-text-grey mb-3">No status received yet{{if .Host}} (last seen at {{.Host}}){{end}}.</p>
  {{end}}
  {{if .Error}}<p class="mh-vac-note is-bad">{{.Error}}</p>{{end}}

  <div class="mh-vac-controls">
    <button type="button" class="mh-vac-btn is-primary" {{if not $c.CleanAll}}disabled{{end}} @click="open('start')">Clean all</button>
    <button type="button" class="mh-vac-btn" {{if not $c.Pause}}disabled{{end}} hx-disabled-elt="this"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"pause"}' hx-target="#vacuum-container">Pause</button>
    <button type="button" class="mh-vac-btn" {{if not $c.Resume}}disabled{{end}} hx-disabled-elt="this"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"resume"}' hx-target="#vacuum-container">Resume</button>
    <button type="button" class="mh-vac-btn is-danger" {{if not $c.Stop}}disabled{{end}} @click="open('stop')">Stop</button>
    <button type="button" class="mh-vac-btn" {{if not $c.Dock}}disabled{{end}} hx-disabled-elt="this"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"dock"}' hx-target="#vacuum-container">Dock</button>
    <button type="button" class="mh-vac-btn" {{if not $c.Find}}disabled{{end}} hx-disabled-elt="this"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"find"}' hx-target="#vacuum-container">Find</button>
    <button type="button" class="mh-vac-btn" {{if not $c.Evac}}disabled{{end}} hx-disabled-elt="this"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"evac"}' hx-target="#vacuum-container">Empty bin</button>
  </div>

  {{/* In-page confirm for clean-all and stop. It cancels itself after 20 s so it cannot hold the poll
       forever. Self-contained so the shared dialog (#683) can replace it. */}}
  <div class="mh-vac-confirm" x-show="ask" x-cloak>
    <p class="mh-vac-ask" x-show="ask === 'start'">Start a whole-home clean?</p>
    <p class="mh-vac-ask" x-show="ask === 'stop'">Stop the mission? The robot will not resume it.</p>
    <button type="button" class="mh-vac-btn" @click="close()">Cancel</button>
    <button type="button" class="mh-vac-btn is-primary" data-vac-confirm="start" x-show="ask === 'start'" hx-disabled-elt="this"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"start"}' hx-target="#vacuum-container">Confirm</button>
    <button type="button" class="mh-vac-btn is-danger" data-vac-confirm="stop" x-show="ask === 'stop'" hx-disabled-elt="this"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"stop"}' hx-target="#vacuum-container">Confirm</button>
  </div>
</div>
{{end}}
`
