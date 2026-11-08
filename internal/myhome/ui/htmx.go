package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/myhome/accounts"
	"github.com/asnowfix/home-automation/myhome/events"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
)

// HTMXHandler returns handlers for HTMX partial HTML responses
type HTMXHandler struct {
	ctx              context.Context
	log              logr.Logger
	db               DeviceRegistry
	eventsSvc        *events.Service
	accountsRegistry *accounts.Registry
	sqlDB            *sqlx.DB // room agenda cache (room_agenda_cache); may be nil
}

// NewHTMXHandler creates a new HTMX handler
func NewHTMXHandler(ctx context.Context, log logr.Logger, db DeviceRegistry, eventsSvc *events.Service, accountsRegistry *accounts.Registry, sqlDB *sqlx.DB) *HTMXHandler {
	return &HTMXHandler{
		ctx:              ctx,
		log:              log,
		db:               db,
		eventsSvc:        eventsSvc,
		accountsRegistry: accountsRegistry,
		sqlDB:            sqlDB,
	}
}

// accountRow is the template view for one account's status row, with
// display fields pre-formatted server-side to keep the template simple.
type accountRow struct {
	Name          string
	StatusClass   string // Bulma tag color: is-success / is-danger / is-light
	StatusText    string
	LastChecked   string
	LastError     string // truncated to maxErrorDisplayLen for display
	LastErrorFull string // untruncated, shown as a hover tooltip
}

// accountDisplayNames maps internal account keys (used by Registry.Report)
// to their human-readable label in the UI.
var accountDisplayNames = map[string]string{
	"beem": "Beem Energy",
	"sfr":  "SFR Box",
	"smtp": "Email (SMTP)",
	"mqtt": "MQTT Broker",
}

// maxErrorDisplayLen caps how much of a LastError string is shown on an
// account status card. Errors like Beem's login failure embed the entire
// JSON response body, which would otherwise overflow the card.
const maxErrorDisplayLen = 120

// truncateError shortens s to maxErrorDisplayLen runes, appending an ellipsis
// if it was cut. The full text is kept separately for a hover tooltip.
func truncateError(s string) string {
	r := []rune(s)
	if len(r) <= maxErrorDisplayLen {
		return s
	}
	return string(r[:maxErrorDisplayLen]) + "…"
}

func toAccountRows(statuses []accounts.Status) []accountRow {
	rows := make([]accountRow, len(statuses))
	for i, s := range statuses {
		name := accountDisplayNames[s.Name]
		if name == "" {
			name = s.Name
		}
		row := accountRow{Name: name, LastError: truncateError(s.LastError), LastErrorFull: s.LastError}
		switch {
		case !s.Enabled:
			row.StatusClass = "is-light"
			row.StatusText = "Not configured"
		case s.LastAttempt.IsZero():
			row.StatusClass = "is-light"
			row.StatusText = "Pending"
		case s.LastOK:
			row.StatusClass = "is-success"
			row.StatusText = "Connected"
		default:
			row.StatusClass = "is-danger"
			row.StatusText = "Failed"
		}
		if !s.LastAttempt.IsZero() {
			row.LastChecked = s.LastAttempt.Format("2006-01-02 15:04:05")
		}
		rows[i] = row
	}
	return rows
}

// AccountsPanel renders the connection status of every external account
// myhome talks to (Beem Energy, SFR box, SMTP, MQTT broker).
func (h *HTMXHandler) AccountsPanel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if h.accountsRegistry == nil {
		fmt.Fprintf(w, `<p class="has-text-grey">Account status not available.</p>`)
		return
	}

	tmpl := template.Must(template.New("accounts-panel").Parse(accountsPanelTemplate))

	if err := tmpl.Execute(w, toAccountRows(h.accountsRegistry.Snapshot())); err != nil {
		h.log.Error(err, "failed to render accounts panel")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// DeviceCards renders all device cards as HTML fragments
func (h *HTMXHandler) DeviceCards(w http.ResponseWriter, r *http.Request) {
	h.log.Info("DeviceCards: request received")

	devices, err := h.db.GetAllDevices(h.ctx)
	if err != nil {
		h.log.Error(err, "DeviceCards: failed to get devices")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.log.Info("DeviceCards: got devices", "count", len(devices))

	// Convert to views
	var deviceViews []DeviceView
	for _, d := range devices {
		deviceViews = append(deviceViews, DeviceToView(h.ctx, d))
	}

	// Sort by name
	sort.Slice(deviceViews, func(i, j int) bool {
		return strings.ToLower(deviceViews[i].Name) < strings.ToLower(deviceViews[j].Name)
	})
	applyPoolStatus(h.ctx, deviceViews)

	h.log.Info("DeviceCards: rendering template", "device_count", len(deviceViews))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := deviceRowTmpl.ExecuteTemplate(w, "devices", deviceViews); err != nil {
		h.log.Error(err, "DeviceCards: failed to render device cards")
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}

	h.log.Info("DeviceCards: template rendered successfully")
}

// DeviceCard renders a single device card HTML fragment
func (h *HTMXHandler) DeviceCard(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimPrefix(r.URL.Path, "/htmx/device/")
	if deviceID == "" {
		http.Error(w, "device ID required", http.StatusBadRequest)
		return
	}

	device, err := h.db.GetDeviceById(h.ctx, deviceID)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	views := []DeviceView{DeviceToView(h.ctx, device)}
	applyPoolStatus(h.ctx, views)
	dv := views[0]

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := deviceRowTmpl.ExecuteTemplate(w, "device", dv); err != nil {
		h.log.Error(err, "failed to render device card")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// agendaSlot mirrors the JSON stored in room_agenda_cache.
type agendaSlot struct {
	S int `json:"s"` // start minutes since midnight
	E int `json:"e"` // end minutes since midnight
}

// RoomView is the template data for a single room card.
type RoomView struct {
	ID      string
	Name    string
	Kinds   []string
	Levels  map[string]float64
	ICalURL string

	// every device of the room, rendered with the Devices-section row
	Devices []DeviceView
	// today's agenda from cache (nil if not available)
	AgendaSlots []agendaSlot
	AgendaNow   bool   // is current time in a busy slot?
	AgendaNext  string // e.g. "busy at 18:00" or "free until 22:00"
}

func (h *HTMXHandler) roomAgenda(roomID string) []agendaSlot {
	if h.sqlDB == nil {
		return nil
	}
	var row struct {
		Slots string `db:"slots"`
	}
	if err := h.sqlDB.Get(&row, `SELECT slots FROM room_agenda_cache WHERE room_id = ?`, roomID); err != nil {
		return nil
	}
	var slots []agendaSlot
	if err := json.Unmarshal([]byte(row.Slots), &slots); err != nil {
		return nil
	}
	return slots
}

func formatMinutes(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func agendaStatus(slots []agendaSlot) (inBusy bool, next string) {
	now := time.Now()
	currentMin := now.Hour()*60 + now.Minute()
	for _, s := range slots {
		if currentMin >= s.S && currentMin < s.E {
			return true, "free at " + formatMinutes(s.E)
		}
	}
	// find next upcoming slot
	for _, s := range slots {
		if s.S > currentMin {
			return false, "busy at " + formatMinutes(s.S)
		}
	}
	return false, ""
}

// roomsTmpl is parsed once, from the same row templates as the Devices
// section so a room lists devices with the very same row (a template cannot be
// cloned once executed, hence a second parse rather than deviceRowTmpl.Clone).
var roomsTmpl = template.Must(template.New("rooms").Funcs(rowTemplateFuncs()).Funcs(template.FuncMap{
	"kindColor": func(k string) string {
		switch k {
		case "bedroom":
			return "is-info"
		case "office":
			return "is-warning"
		case "living-room":
			return "is-success"
		case "kitchen":
			return "is-danger"
		default:
			return "is-light"
		}
	},
}).Parse(deviceRowsTemplate + roomsListTemplate))

// RoomsList renders the rooms list HTML fragment
func (h *HTMXHandler) RoomsList(w http.ResponseWriter, r *http.Request) {
	// The temperature service may be disabled: rooms are then empty and every
	// device lands in "Unassigned", so the page still lists each device once.
	roomsDisabled := false
	var roomMap myhome.TemperatureRoomList
	if mh, err := myhome.Methods(myhome.TemperatureList); err != nil {
		roomsDisabled = true
	} else {
		res, err := mh.ActionE(h.ctx, mh.Signature.NewParams())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rm, ok := res.(*myhome.TemperatureRoomList)
		if !ok {
			http.Error(w, "unexpected result type", http.StatusInternalServerError)
			return
		}
		roomMap = *rm
	}

	// One pass over the DB-loaded cache (no device call): each device goes to
	// its room, or to Unassigned when it has none or names an unknown room.
	var allDevices []*myhome.Device
	if h.db != nil {
		allDevices, _ = h.db.GetAllDevices(h.ctx)
	}
	byRoom := make(map[string][]DeviceView, len(roomMap))
	var unassigned []DeviceView
	for _, d := range allDevices {
		dv := DeviceToView(h.ctx, d)
		if _, known := roomMap[d.RoomId]; known && d.RoomId != "" {
			byRoom[d.RoomId] = append(byRoom[d.RoomId], dv)
		} else {
			unassigned = append(unassigned, dv)
		}
	}
	sortViews := func(v []DeviceView) {
		sort.Slice(v, func(i, j int) bool {
			return strings.ToLower(v[i].Name) < strings.ToLower(v[j].Name)
		})
		applyPoolStatus(h.ctx, v)
	}
	sortViews(unassigned)

	views := make([]RoomView, 0, len(roomMap))
	for _, rc := range roomMap {
		kinds := make([]string, len(rc.Kinds))
		for i, k := range rc.Kinds {
			kinds[i] = string(k)
		}

		devViews := byRoom[rc.RoomID]
		sortViews(devViews)

		slots := h.roomAgenda(rc.RoomID)
		inBusy, nextLabel := agendaStatus(slots)

		views = append(views, RoomView{
			ID:          rc.RoomID,
			Name:        rc.Name,
			Kinds:       kinds,
			Levels:      rc.Levels,
			ICalURL:     rc.ICalURL,
			Devices:     devViews,
			AgendaSlots: slots,
			AgendaNow:   inBusy,
			AgendaNext:  nextLabel,
		})
	}

	sort.Slice(views, func(i, j int) bool {
		return views[i].Name < views[j].Name
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if roomsDisabled {
		fmt.Fprint(w, `<div class="column is-12" data-rooms-disabled><p class="has-text-grey">Rooms are not enabled on this daemon.</p></div>`)
	}
	data := struct {
		Rooms      []RoomView
		Unassigned []DeviceView
		Disabled   bool
	}{views, unassigned, roomsDisabled}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := roomsTmpl.ExecuteTemplate(w, "rooms-list", data); err != nil {
		h.log.Error(err, "failed to render rooms list")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// SwitchButton renders a switch button with updated state
func (h *HTMXHandler) SwitchButton(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	deviceID := r.FormValue("device_id")
	switchID := r.FormValue("switch_id")

	if deviceID == "" || switchID == "" {
		http.Error(w, "device_id and switch_id required", http.StatusBadRequest)
		return
	}

	sid, err := strconv.Atoi(switchID)
	if err != nil {
		http.Error(w, "switch_id must be a number", http.StatusBadRequest)
		return
	}

	// Call switch.toggle RPC
	mh, err := myhome.Methods(myhome.SwitchToggle)
	if err != nil {
		http.Error(w, "method not found", http.StatusInternalServerError)
		return
	}

	params := myhome.SwitchParams{
		Identifier: deviceID,
		SwitchId:   sid,
	}

	// The daemon context carries the logger and values the handler needs; the
	// request's cancellation (a closed browser) is layered on top of it.
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	defer context.AfterFunc(r.Context(), cancel)()

	res, err := mh.ActionE(ctx, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result, ok := res.(*myhome.SwitchResult)
	if !ok {
		http.Error(w, "invalid response", http.StatusInternalServerError)
		return
	}

	// Render the round button with the logical state after polarity.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := deviceRowTmpl.ExecuteTemplate(w, "power", powerView{
		DeviceID: deviceID,
		SwitchID: sid,
		Label:    r.FormValue("label"),
		Known:    true,
		On:       result.On,
	}); err != nil {
		h.log.Error(err, "failed to render switch button")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// EventsTable renders the events table HTMX fragment (newest first, limit 50).
func (h *HTMXHandler) EventsTable(w http.ResponseWriter, r *http.Request) {
	h.renderEventsTable(w, r, 0)
}

// EventsMore renders additional event rows for pagination (appended into tbody).
func (h *HTMXHandler) EventsMore(w http.ResponseWriter, r *http.Request) {
	offsetStr := r.URL.Query().Get("offset")
	offset, _ := strconv.Atoi(offsetStr)
	h.renderEventsRows(w, r, offset)
}

// eventRow is the template view for a single event row.
// Fields are copied explicitly to avoid the embedded-struct name collision:
// events.Event has a field named Event (string), and embedding events.Event
// creates an outer field also named Event (the struct itself), shadowing it.
type eventRow struct {
	Ts         float64
	DeviceID   string
	Component  string
	Event      string
	Severity   string
	Data       *string
	DeviceName string
}

// resolveDeviceFilter translates a free-text device filter (name, id, or MAC)
// into the list of device_id values that may appear in the events table.
// Both the stored id and the MAC are included because different event sources
// (Gen2 MQTT vs BLU) store different identifiers as device_id.
// Falls back to the raw filter string when no device record is found.
func (h *HTMXHandler) resolveDeviceFilter(filter string) []string {
	if filter == "" {
		return nil
	}
	d, err := h.db.GetDeviceByAny(h.ctx, filter)
	if err != nil {
		if mac := shellyapi.MacFromShellyID(filter); mac != nil {
			d, err = h.db.GetDeviceByAny(h.ctx, mac.String())
		}
	}
	if err != nil {
		return []string{filter}
	}
	ids := []string{d.Id()}
	if mac := d.Mac(); mac != nil {
		if s := mac.String(); s != d.Id() {
			ids = append(ids, s)
		}
	}
	return ids
}

// deviceName resolves a single device id to a friendly name.
// It queries by id first, then falls back to the MAC derived from a Shelly id suffix.
// Returns "" when no friendly name is found (caller shows the raw id).
func (h *HTMXHandler) deviceName(id string) string {
	d, err := h.db.GetDeviceByAny(h.ctx, id)
	if err != nil {
		if mac := shellyapi.MacFromShellyID(id); mac != nil {
			d, err = h.db.GetDeviceByAny(h.ctx, mac.String())
		}
	}
	if err != nil {
		return ""
	}
	if n := d.Name(); n != "" && n != id {
		return n
	}
	return ""
}

// DeviceNameResolver returns a function suitable for SSEBroadcaster.SetDeviceNameResolver.
func (h *HTMXHandler) DeviceNameResolver() func(string) string {
	return h.deviceName
}

// deviceNameMap resolves names for all unique device IDs that appear in evts.
func (h *HTMXHandler) deviceNameMap(evts []events.Event) map[string]string {
	seen := make(map[string]struct{}, len(evts))
	for _, e := range evts {
		seen[e.DeviceID] = struct{}{}
	}
	m := make(map[string]string, len(seen))
	for id := range seen {
		if n := h.deviceName(id); n != "" {
			m[id] = n
		}
	}
	return m
}

// toEventRows decorates a slice of events with device names from the map.
func toEventRows(evts []events.Event, names map[string]string) []eventRow {
	rows := make([]eventRow, len(evts))
	for i, e := range evts {
		rows[i] = eventRow{
			Ts:         e.Ts,
			DeviceID:   e.DeviceID,
			Component:  e.Component,
			Event:      e.Event,
			Severity:   e.Severity,
			Data:       e.Data,
			DeviceName: names[e.DeviceID],
		}
	}
	return rows
}

func (h *HTMXHandler) buildQuery(r *http.Request, offset int) events.Query {
	device := r.URL.Query().Get("device")
	evType := r.URL.Query().Get("type")
	severity := r.URL.Query().Get("severity")
	q := events.Query{
		DeviceIDs: h.resolveDeviceFilter(device),
		EventType: evType,
		Severity:  severity,
		Since:     24 * time.Hour,
		Limit:     50,
		Offset:    offset,
	}
	return q
}

func (h *HTMXHandler) renderEventsTable(w http.ResponseWriter, r *http.Request, offset int) {
	if h.eventsSvc == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<p class="has-text-grey">Event service not available.</p>`)
		return
	}

	q := h.buildQuery(r, offset)
	evts, err := h.eventsSvc.Store().Query(h.ctx, q)
	if err != nil {
		h.log.Error(err, "EventsTable: failed to query events")
		http.Error(w, "query error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Events":   toEventRows(evts, h.deviceNameMap(evts)),
		"Offset":   offset + len(evts),
		"Device":   r.URL.Query().Get("device"),
		"Type":     q.EventType,
		"Severity": q.Severity,
	}

	tmpl := template.Must(template.New("events-table").Funcs(eventTemplateFuncs()).Parse(eventsTableTemplate))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		h.log.Error(err, "EventsTable: failed to render template")
	}
}

func (h *HTMXHandler) renderEventsRows(w http.ResponseWriter, r *http.Request, offset int) {
	if h.eventsSvc == nil {
		return
	}

	q := h.buildQuery(r, offset)
	evts, err := h.eventsSvc.Store().Query(h.ctx, q)
	if err != nil {
		h.log.Error(err, "EventsMore: failed to query events")
		http.Error(w, "query error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Events":   toEventRows(evts, h.deviceNameMap(evts)),
		"Offset":   offset + len(evts),
		"Device":   r.URL.Query().Get("device"),
		"Type":     q.EventType,
		"Severity": q.Severity,
	}

	tmpl := template.Must(template.New("events-rows").Funcs(eventTemplateFuncs()).Parse(eventsRowsTemplate))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		h.log.Error(err, "EventsMore: failed to render template")
	}
}

const roomsListTemplate = `{{define "rooms-list"}}
{{if .Rooms}}
  {{range .Rooms}}
  <div class="column is-4-desktop is-6-tablet">
    <div class="box">
      <div class="level mb-2">
        <div class="level-left">
          <div>
            <strong>{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}</strong>
            {{if .ICalURL}}<span class="ml-1" title="iCal agenda configured">📅</span>{{end}}
            <p class="is-size-7 has-text-grey">{{.ID}}</p>
          </div>
        </div>
        <div class="level-right">
          <button class="button is-small is-ghost mr-1"
                  @click="$dispatch('edit-room', {roomId: '{{.ID}}'})"
                  title="Edit">✏️</button>
          <button class="button is-small is-ghost"
                  @click="$dispatch('delete-room', {roomId: '{{.ID}}', roomName: '{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}'})"
                  title="Delete">🗑️</button>
        </div>
      </div>

      {{if .Kinds}}
      <div class="tags mb-2">
        {{range .Kinds}}<span class="tag {{kindColor .}}">{{.}}</span>{{end}}
      </div>
      {{end}}

      {{if .Levels}}
      <p class="is-size-7 mb-2">
        {{with index .Levels "eco"}}eco {{printf "%.0f" .}}°C{{end}}
        {{with index .Levels "comfort"}} · comfort {{printf "%.0f" .}}°C{{end}}
        {{with index .Levels "away"}} · away {{printf "%.0f" .}}°C{{end}}
      </p>
      {{end}}

      {{if .AgendaSlots}}
      <p class="is-size-7 mb-2">
        {{if .AgendaNow}}<span class="tag is-success is-light">busy now</span>{{else}}<span class="tag is-light">free now</span>{{end}}
        {{if .AgendaNext}}<span class="has-text-grey ml-1">{{.AgendaNext}}</span>{{end}}
      </p>
      {{end}}

      {{if .Devices}}
      <div class="mt-2 mh-grid">{{template "devices" .Devices}}</div>
      {{end}}
    </div>
  </div>
  {{end}}
{{else if and (not .Unassigned) (not .Disabled)}}
  <div class="column is-12">
    <p class="has-text-grey">No rooms configured yet. Click <strong>+ Add Room</strong> to create one.</p>
  </div>
{{end}}
{{if .Unassigned}}
  <div class="column is-12" data-unassigned>
    <div class="box">
      <strong>Unassigned</strong>
      <div class="mt-2 mh-grid">{{template "devices" .Unassigned}}</div>
    </div>
  </div>
{{end}}
{{end}}
`

const accountsPanelTemplate = `
{{range .}}
<div class="column is-3-desktop is-6-tablet">
  <div class="box">
    <div class="level mb-0">
      <div class="level-left"><strong>{{.Name}}</strong></div>
      <div class="level-right"><span class="tag {{.StatusClass}}">{{.StatusText}}</span></div>
    </div>
    {{if .LastChecked}}
      <p class="is-size-7 has-text-grey">Last checked: {{.LastChecked}}</p>
    {{end}}
    {{if .LastError}}
      <p class="is-size-7 has-text-danger" style="overflow-wrap: anywhere;" title="{{.LastErrorFull}}">{{.LastError}}</p>
    {{end}}
  </div>
</div>
{{else}}
<div class="column is-12">
  <p class="has-text-grey">No accounts tracked yet.</p>
</div>
{{end}}
`
