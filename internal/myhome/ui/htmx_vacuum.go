package ui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// Vacuum panel (#606): one card per paired robot, refreshed by polling. The
// robot pushes its state to the daemon only on change, so polling the daemon's
// cached state is cheap and never reaches the robot.

// vacuumCard is the template view of one robot.
type vacuumCard struct {
	myhome.VacuumRobot
	PhaseText string
	Running   bool // cleaning or returning
	Paused    bool
	Rooms     []myhome.VacuumRegion
	Error     string // last action's failure, shown on the card
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
		p.Cards = append(p.Cards, c)
	}
	sort.Slice(p.Cards, func(i, j int) bool { return strings.ToLower(p.Cards[i].Name) < strings.ToLower(p.Cards[j].Name) })
	return p, true
}

func (h *HTMXHandler) renderVacuum(w http.ResponseWriter, actionErr map[string]string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	p, ok := h.vacuumPanel(actionErr)
	if !ok {
		return // no robot paired: the section stays empty
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
<h2 class="title is-4">🧹 Vacuum</h2>
<div class="columns is-multiline">
{{range .Cards}}
  <div class="column is-12-mobile is-6-tablet is-4-desktop">
    <div class="card">
      <header class="card-header">
        <p class="card-header-title">{{.Name}}</p>
        <span class="card-header-icon">
          {{if .Connected}}<span class="tag is-success is-light">connected</span>{{else}}<span class="tag is-danger is-light">offline</span>{{end}}
        </span>
      </header>
      <div class="card-content">
        {{if .Status}}
        <p class="is-size-5 mb-2">{{.PhaseText}}</p>
        <progress class="progress is-small {{if lt .Status.Battery 20}}is-danger{{else if lt .Status.Battery 50}}is-warning{{else}}is-success{{end}}"
                  value="{{.Status.Battery}}" max="100" title="battery {{.Status.Battery}}%">{{.Status.Battery}}%</progress>
        <div class="tags">
          <span class="tag">battery {{.Status.Battery}}%</span>
          {{if not .Status.BinPresent}}<span class="tag is-danger">bin missing</span>
          {{else if .Status.BinFull}}<span class="tag is-warning">bin full</span>
          {{else}}<span class="tag is-light">bin ok</span>{{end}}
          {{if ne .Status.Error 0}}<span class="tag is-danger">error {{.Status.Error}}</span>{{end}}
          {{if gt .Status.MissionMinutes 0}}<span class="tag is-light">{{.Status.MissionMinutes}} min</span>{{end}}
        </div>
        {{else}}
        <p class="has-text-grey">No status received yet{{if .Host}} (last seen at {{.Host}}){{end}}.</p>
        {{end}}
        {{if .Error}}<p class="notification is-danger is-light py-2 px-3">{{.Error}}</p>{{end}}
      </div>
      <footer class="card-footer">
        {{$id := .ID}}
        {{if .Running}}
          <a class="card-footer-item" hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"pause"}' hx-target="#vacuum-container">Pause</a>
          <a class="card-footer-item" hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"stop"}' hx-target="#vacuum-container">Stop</a>
        {{else if .Paused}}
          <a class="card-footer-item" hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"resume"}' hx-target="#vacuum-container">Resume</a>
          <a class="card-footer-item" hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"stop"}' hx-target="#vacuum-container">Stop</a>
        {{else}}
          <a class="card-footer-item" hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"start"}' hx-target="#vacuum-container"
             hx-confirm="Start a whole-home clean?">Clean all</a>
        {{end}}
        <a class="card-footer-item" hx-post="/htmx/vacuum/action" hx-vals='{"robot":"{{$id}}","command":"dock"}' hx-target="#vacuum-container">Dock</a>
      </footer>
      {{if .Rooms}}
      <form class="card-content pt-3" hx-post="/htmx/vacuum/action" hx-target="#vacuum-container">
        <input type="hidden" name="robot" value="{{.ID}}">
        <input type="hidden" name="command" value="clean-rooms">
        <div class="field is-grouped is-grouped-multiline">
          {{range .Rooms}}
          <label class="checkbox mr-3"><input type="checkbox" name="room" value="{{.ID}}"> {{if .Name}}{{.Name}}{{else}}region {{.ID}}{{end}}</label>
          {{end}}
        </div>
        <button class="button is-small is-primary" type="submit">Clean selected rooms</button>
      </form>
      {{end}}
    </div>
  </div>
{{end}}
</div>
`
