package ui

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// Read-only room page (#764, #684 PR 3b): GET /htmx/room/{id}. It renders from
// daemon memory and SQLite only (device cache, temperature.*, occupancy.*,
// vacuum.*); no device is contacted. There is deliberately no heater toggle and
// heater control (#684 addendum §F); the vacuum block can start a clean of the
// room's mapped areas (#765), the one action on the page.

// roomNow is the page's clock; tests replace it.
var roomNow = time.Now

// tempAgeThreshold: a temperature older than this shows how old it is.
const tempAgeThreshold = 30 * time.Minute

// roomTempView is the climate block's headline temperature.
type roomTempView struct {
	Value   string // "21.4" or "--"
	Age     string // "· 2 h ago", "· age --" when the value has no update time, or ""
	Warning string // configuration error (2+ sensors); then Value is not shown
}

type roomHeaterView struct {
	Name  string
	State string // Heating, Idle or --
}

type roomVacuumView struct {
	RoomID, RoomName string
	Show             bool // a robot is configured
	Areas            []string
	CanClean         bool   // the room's areas resolve to one map and the robot is connected and idle
	Reason           string // why Clean this room is disabled; "" when CanClean or when no area is assigned
	Vals             string // hx-vals of the confirmed request (JSON)
	Notice           string // outcome of the last clean ("Cleaning <room>")
	Error            string // failure of the last clean
	Label            string // the Clean button's text
	Target           string // hx-target of the confirmed request: where the response goes
}

type roomPageView struct {
	ID, Name  string
	DeviceIDs string // space separated, for the SSE refresh filter
	Temp      roomTempView
	Target    string
	Heaters   []roomHeaterView
	Devices   []DeviceView
	Vacuum    roomVacuumView
}

// tempAgeText is "" for a fresh reading, otherwise "· 45 min ago" / "· 2 h ago" / "· 3 d ago".
func tempAgeText(age time.Duration) string {
	switch {
	case age < tempAgeThreshold:
		return ""
	case age < time.Hour:
		return fmt.Sprintf("· %d min ago", int(age.Minutes()))
	case age < 48*time.Hour:
		return fmt.Sprintf("· %d h ago", int(age.Hours()))
	}
	return fmt.Sprintf("· %d d ago", int(age.Hours()/24))
}

func roomTemperature(sensors []*myhome.Device, now time.Time) roomTempView {
	switch len(sensors) {
	case 0:
		return roomTempView{Value: "--"}
	case 1:
	default:
		return roomTempView{Warning: fmt.Sprintf("Configuration error: %d temperature sensors in this room; keep one", len(sensors))}
	}
	d := sensors[0]
	if d.Status == nil || d.Status.Sensors == nil || d.Status.Sensors.Temperature == nil {
		return roomTempView{Value: "--"}
	}
	v := roomTempView{Value: fmt.Sprintf("%.1f", *d.Status.Sensors.Temperature)}
	if t := d.Status.TemperatureUpdatedAt; t != nil {
		v.Age = tempAgeText(now.Sub(*t))
	} else {
		// Should not occur in practice: value and time are set together.
		v.Age = "· age --"
	}
	return v
}

// roomTarget renders the heater script's current target for the room.
func (h *HTMXHandler) roomTarget(roomID string, now time.Time) string {
	res, err := callVacuum(h.ctx, myhome.TemperatureGetSchedule, &myhome.TemperatureGetScheduleParams{RoomID: roomID})
	if err != nil {
		return "Target --"
	}
	sched, ok := res.(*myhome.TemperatureScheduleResult)
	if !ok {
		return "Target --"
	}
	var occupied *bool
	if r, err := callVacuum(h.ctx, myhome.OccupancyGetStatus, nil); err == nil {
		if st, ok := r.(*myhome.OccupancyStatusResult); ok {
			occupied = &st.Occupied
		}
	}
	level, celsius, until, ok := myhome.HeatingTarget(sched.Levels, sched.ComfortRanges, occupied, now)
	if !ok {
		return "Target --"
	}
	s := fmt.Sprintf("Target %.1f °C · %s", celsius, level)
	switch {
	case level == myhome.HeatingLevelAway && occupied == nil:
		s += " (no presence data)"
	case level == myhome.HeatingLevelAway:
		s += " (nobody home)"
	case until != nil:
		s += " until " + until.Format("15:04")
	}
	return s
}

// roomDeviceRows is deviceRows with heater rows made read-only: the heater
// script decides, the page only shows state.
func roomDeviceRows(d DeviceView) []deviceRow {
	rows := deviceRows(d)
	if d.HasHeaterScript {
		for i := range rows {
			rows[i].NoToggle = true
			rows[i].Caption = "set by heater script"
		}
	}
	return rows
}

var roomTmpl = template.Must(template.New("room").Funcs(rowTemplateFuncs()).Funcs(template.FuncMap{"rows": roomDeviceRows}).
	Parse(deviceRowsTemplate + roomCleanTemplate + strings.ReplaceAll(roomPageTemplate, "@XDATA@", roomCleanXData)))

func (h *HTMXHandler) roomRoute(w http.ResponseWriter, r *http.Request, part string) {
	id := r.PathValue("id")
	now := roomNow()

	notFound := func(msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `<p class="has-text-grey" data-room-not-found>%s</p>`, template.HTMLEscapeString(msg))
	}
	mh, err := myhome.Methods(myhome.TemperatureList)
	if err != nil {
		notFound("Rooms are not enabled on this daemon.")
		return
	}
	res, err := mh.ActionE(h.ctx, mh.Signature.NewParams())
	rooms, ok := res.(*myhome.TemperatureRoomList)
	if err != nil || !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `<p class="has-text-danger" data-room-load-error>Couldn't load the room, try again.</p>`)
		return
	}
	rc, known := (*rooms)[id]
	if !known {
		notFound("Room not found")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if part == "vacuum" {
		if err := roomTmpl.ExecuteTemplate(w, "room-vacuum", h.roomVacuum(id, rc.Name, "", "")); err != nil {
			h.log.Error(err, "failed to render room vacuum block")
		}
		return
	}

	var all []*myhome.Device
	if h.db != nil {
		if all, err = h.db.GetAllDevices(h.ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	var sensors []*myhome.Device
	var views []DeviceView
	for _, d := range all {
		if d.RoomId != id {
			continue
		}
		if myhome.IsTemperatureSensor(d) {
			sensors = append(sensors, d)
		}
		views = append(views, DeviceToView(h.ctx, d))
	}
	sort.Slice(views, func(i, j int) bool { return strings.ToLower(views[i].Name) < strings.ToLower(views[j].Name) })
	applyPoolStatus(h.ctx, views)

	p := roomPageView{ID: id, Name: rc.Name, Devices: views, Temp: roomTemperature(sensors, now), Target: h.roomTarget(id, now), Vacuum: h.roomVacuum(id, rc.Name, "", "")}
	if p.Name == "" {
		p.Name = id
	}
	ids := make([]string, 0, len(views))
	for _, v := range views {
		ids = append(ids, v.Id)
		if !v.HasHeaterScript {
			continue
		}
		state := "--"
		if sw, ok := v.Switches[0]; ok && !v.SwitchUnknown[0] {
			state = "Idle"
			if sw.On {
				state = "Heating"
			}
		}
		p.Heaters = append(p.Heaters, roomHeaterView{Name: v.Name, State: state})
	}
	p.DeviceIDs = strings.Join(ids, " ")
	if err := roomTmpl.ExecuteTemplate(w, "room-page", p); err != nil {
		h.log.Error(err, "failed to render room page")
	}
}

// RoomPage renders GET /htmx/room/{id}.
func (h *HTMXHandler) RoomPage(w http.ResponseWriter, r *http.Request) { h.roomRoute(w, r, "") }

// RoomVacuum renders GET /htmx/room/{id}/vacuum, the block that re-polls at 5 s.
func (h *HTMXHandler) RoomVacuum(w http.ResponseWriter, r *http.Request) { h.roomRoute(w, r, "vacuum") }

// The page root is the refresh target (outerHTML) of the shell's coalesced SSE
// refresh. Back and Edit are 48 px tap targets; nothing is hover-only.
const roomPageTemplate = `
{{define "room-vacuum"}}{{if .Show}}<div id="room-vacuum" class="mh-room-block"
     hx-get="/htmx/room/{{.RoomID | urlquery}}/vacuum" hx-swap="outerHTML"
     hx-trigger="every 5s [kiosk.visible('rooms') && kiosk.roomOpen && kiosk.online && !document.querySelector('#room-vacuum[data-confirming]')]"
     x-data="@XDATA@"
     :data-confirming="ask ? 'true' : null">
  <h3 class="mh-room-label">Vacuum</h3>
  {{if .Areas}}<p>Areas: {{range $i, $a := .Areas}}{{if $i}}, {{end}}{{$a}}{{end}}</p>
  {{else}}<p class="has-text-grey">No robot area assigned (<code>myhome ctl vacuum rooms assign</code>)</p>{{end}}
  {{if .Notice}}<p class="mh-vac-note" data-room-clean-notice>{{.Notice}}</p>{{end}}
  {{if .Error}}<p class="mh-vac-note is-bad" data-room-clean-error>{{.Error}}</p>{{end}}
  {{template "room-clean-control" .}}
</div>{{else}}<div id="room-vacuum" hidden></div>{{end}}{{end}}

{{define "room-page"}}<div class="mh-room" data-room="{{.ID}}" data-devices="{{.DeviceIDs}}" data-refresh
     hx-get="/htmx/room/{{.ID | urlquery}}" hx-trigger="refresh" hx-swap="outerHTML">
  <header class="mh-room-head">
    <button type="button" class="button mh-room-back" @click="closeRoom()">&lsaquo; Back</button>
    <h2 class="title is-4">{{.Name}}</h2>
    <button type="button" class="button mh-room-edit" data-room-id="{{.ID}}" @click="$dispatch('edit-room', {roomId: $el.dataset.roomId})">Edit</button>
  </header>
  <div class="mh-room-body">
    <div class="mh-room-side">
      <div class="mh-room-block" id="room-climate">
        <h3 class="mh-room-label">Climate</h3>
        {{if .Temp.Warning}}<p class="has-text-danger" data-config-error>{{.Temp.Warning}}</p>
        {{else}}<p class="mh-room-temp"><span id="room-temp">{{.Temp.Value}}{{if ne .Temp.Value "--"}} °C{{end}}</span>{{if .Temp.Age}} <span class="has-text-grey" id="room-temp-age">{{.Temp.Age}}</span>{{end}}</p>{{end}}
        <p id="room-target">{{.Target}}</p>
        {{range .Heaters}}<p>Heater {{.Name}}: <strong>{{.State}}</strong></p>{{end}}
      </div>
      {{template "room-vacuum" .Vacuum}}
    </div>
    <div class="mh-room-devices">
      <h3 class="mh-room-label">Devices</h3>
      {{if .Devices}}<div class="mh-grid">{{template "devices" .Devices}}</div>{{else}}<p class="has-text-grey">No devices</p>{{end}}
    </div>
  </div>
</div>{{end}}
`
