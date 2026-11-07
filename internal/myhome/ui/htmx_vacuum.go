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
	Running   bool // cleaning or returning
	Paused    bool
	Rooms     []myhome.VacuumRegion
	Targets   []string // names of the rooms being cleaned, while a mission is open; empty for a whole-home clean
	Error     string   // last action's failure, shown on the card
	ErrorHint string   // explanation of the robot's error code, when one is known
	CycleText string   // the robot's cycle in words; empty when idle
	ErrorText string   // the robot's error code in words; empty when there is none
	NotReady  string   // why the robot cannot start, in words; empty when ready
	Updated   string   // when the robot last reported, e.g. "12:03:05 (2 min ago)"
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
	if hint := myhome.VacuumErrorHint(code); hint != "" {
		return hint
	}
	return fmt.Sprintf("code %d", code)
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
func vacuumUpdatedText(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	age := now.Sub(t).Round(time.Minute)
	switch {
	case age < time.Minute:
		return t.Local().Format("15:04:05") + " (just now)"
	case age < time.Hour:
		return fmt.Sprintf("%s (%d min ago)", t.Local().Format("15:04:05"), int(age.Minutes()))
	}
	return t.Local().Format("2006-01-02 15:04:05")
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
		if r.Status != nil {
			c.PhaseText = myhome.VacuumPhaseText(r.Status.Phase)
			c.ErrorHint = myhome.VacuumErrorHint(r.Status.Error)
			c.CycleText = vacuumCycleText(r.Status.Cycle)
			c.ErrorText = vacuumErrorText(r.Status.Error)
			c.NotReady = vacuumNotReadyText(r.Status.NotReady)
			c.Updated = vacuumUpdatedText(r.Status.UpdatedAt, time.Now())
			switch r.Status.Phase {
			case "run", "hmUsrDock", "hmMidMsn", "hmPostMsn":
				c.Running = true
			case "stop":
				c.Paused = r.Status.Cycle != "none"
			}
		}
		if rooms, err := callVacuum(h.ctx, myhome.VacuumRooms, &myhome.VacuumParams{Robot: r.ID}); err == nil {
			for _, reg := range rooms.(*myhome.VacuumRoomsResult).Regions {
				if !reg.Stale {
					c.Rooms = append(c.Rooms, reg)
				}
			}
		}
		if r.Status != nil && (c.Running || c.Paused) {
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

// VacuumAction runs a command or a room clean and re-renders the panel (POST /htmx/vacuum/action).
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
	var err error
	if command == "clean-rooms" {
		rooms := r.Form["room"]
		if len(rooms) == 0 {
			err = fmt.Errorf("select at least one room")
		} else {
			_, err = callVacuum(h.ctx, myhome.VacuumClean, &myhome.VacuumCleanParams{Robot: robot, Rooms: rooms})
		}
	} else {
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
{{$id := .ID}}{{$on := .Connected}}
<div class="mh-vac" x-data="{ask: ''}" :data-confirming="ask ? 'true' : null">
  <div class="mh-vac-head">
    <p class="mh-vac-name">{{.Name}}</p>
    {{if .Connected}}<span class="tag is-success is-medium">connected</span>{{else}}<span class="tag is-danger is-medium">offline</span>{{end}}
  </div>
  {{if not .Connected}}<p class="notification is-warning is-light py-2 px-3">The robot is not connected to the daemon{{if .Status}}; the figures below are the last it reported{{end}}. Controls are disabled.</p>{{end}}
  {{if .Status}}
  <p class="mh-vac-phase">{{.PhaseText}}{{if .CycleText}}<span class="mh-vac-sub"> - {{.CycleText}}</span>{{end}}</p>
  {{if .Targets}}<p class="mb-2">Cleaning: {{range $i, $t := .Targets}}{{if $i}}, {{end}}{{$t}}{{end}}</p>{{end}}
  <progress class="progress {{if lt .Status.Battery 20}}is-danger{{else if lt .Status.Battery 50}}is-warning{{else}}is-success{{end}}"
            value="{{.Status.Battery}}" max="100">{{.Status.Battery}}%</progress>
  <div class="tags are-medium">
    <span class="tag">battery {{.Status.Battery}}%</span>
    {{if not .Status.BinPresent}}<span class="tag is-danger">bin missing</span>
    {{else if .Status.BinFull}}<span class="tag is-warning">bin full</span>
    {{else}}<span class="tag is-light">bin ok</span>{{end}}
    {{if gt .Status.MissionMinutes 0}}<span class="tag is-light">mission {{.Status.MissionMinutes}} min</span>{{end}}
  </div>
  {{if .ErrorText}}<p class="notification is-danger is-light py-2 px-3">Error: {{.ErrorText}}</p>{{end}}
  {{if .NotReady}}<p class="notification is-warning is-light py-2 px-3">Not ready to start: {{.NotReady}}</p>{{end}}
  <p class="mh-vac-sub mb-3">Last update: {{.Updated}}</p>
  {{else}}
  <p class="has-text-grey mb-3">No status received yet{{if .Host}} (last seen at {{.Host}}){{end}}.</p>
  {{end}}
  {{if .Error}}<p class="notification is-danger is-light py-2 px-3">{{.Error}}</p>{{end}}

  <div class="mh-vac-controls">
    <button type="button" class="mh-vac-btn is-primary" {{if or (not $on) .Running .Paused}}disabled{{end}} @click="ask = 'start'">Clean all</button>
    <button type="button" class="mh-vac-btn" {{if or (not $on) (not .Running)}}disabled{{end}}
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"pause"}' hx-target="#vacuum-container">Pause</button>
    <button type="button" class="mh-vac-btn" {{if or (not $on) (not .Paused)}}disabled{{end}}
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"resume"}' hx-target="#vacuum-container">Resume</button>
    <button type="button" class="mh-vac-btn is-danger" {{if or (not $on) (and (not .Running) (not .Paused))}}disabled{{end}} @click="ask = 'stop'">Stop</button>
    <button type="button" class="mh-vac-btn" {{if not $on}}disabled{{end}}
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"dock"}' hx-target="#vacuum-container">Dock</button>
    <button type="button" class="mh-vac-btn" {{if not $on}}disabled{{end}}
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"find"}' hx-target="#vacuum-container">Find</button>
    <button type="button" class="mh-vac-btn" {{if or (not $on) .Running .Paused}}disabled{{end}}
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"evac"}' hx-target="#vacuum-container">Empty bin</button>
  </div>

  {{/* In-page confirm for clean-all and stop. Self-contained so the shared dialog (#683) can replace it. */}}
  <div class="mh-vac-confirm" x-show="ask" x-cloak>
    <p class="mh-vac-ask" x-show="ask === 'start'">Start a whole-home clean?</p>
    <p class="mh-vac-ask" x-show="ask === 'stop'">Stop the mission? The robot will not resume it.</p>
    <button type="button" class="mh-vac-btn" @click="ask = ''">Cancel</button>
    <button type="button" class="mh-vac-btn is-primary" data-vac-confirm="start" x-show="ask === 'start'"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"start"}' hx-target="#vacuum-container">Confirm</button>
    <button type="button" class="mh-vac-btn is-danger" data-vac-confirm="stop" x-show="ask === 'stop'"
            hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"stop"}' hx-target="#vacuum-container">Confirm</button>
  </div>
</div>
{{end}}
`
