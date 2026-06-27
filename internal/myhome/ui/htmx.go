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
	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/myhome/storage"
	shellyapi "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/go-logr/logr"
)

// HTMXHandler returns handlers for HTMX partial HTML responses
type HTMXHandler struct {
	ctx       context.Context
	log       logr.Logger
	db        *storage.DeviceStorage
	eventsSvc *events.Service
}

// NewHTMXHandler creates a new HTMX handler
func NewHTMXHandler(ctx context.Context, log logr.Logger, db *storage.DeviceStorage, eventsSvc *events.Service) *HTMXHandler {
	return &HTMXHandler{
		ctx:       ctx,
		log:       log,
		db:        db,
		eventsSvc: eventsSvc,
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

	h.log.Info("DeviceCards: rendering template", "device_count", len(deviceViews))

	// Render all device cards
	tmpl := template.Must(template.New("device-cards").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).Parse(deviceCardsTemplate))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, deviceViews); err != nil {
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

	dv := DeviceToView(h.ctx, device)

	tmpl := template.Must(template.New("device-card").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).Parse(deviceCardTemplate))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, dv); err != nil {
		h.log.Error(err, "failed to render device card")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// roomDeviceView summarises one device assigned to a room.
type roomDeviceView struct {
	ID   string
	Name string
	Role string // "heater", "temp-sensor", "door-sensor", "other"
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

	Devices []roomDeviceView
	// today's agenda from cache (nil if not available)
	AgendaSlots []agendaSlot
	AgendaNow   bool   // is current time in a busy slot?
	AgendaNext  string // e.g. "busy at 18:00" or "free until 22:00"
}

func (h *HTMXHandler) roomAgenda(roomID string) []agendaSlot {
	var row struct {
		Slots string `db:"slots"`
	}
	if err := h.db.DB().Get(&row, `SELECT slots FROM room_agenda_cache WHERE room_id = ?`, roomID); err != nil {
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

func deviceRoleLabel(d *myhome.Device) string {
	id := strings.ToLower(d.Id())
	if strings.HasPrefix(id, "shellyblu-") {
		if d.Info != nil && d.Info.BTHome != nil {
			for _, cap := range d.Info.BTHome.Capabilities {
				if cap == "window" {
					return "door-sensor"
				}
			}
			for _, cap := range d.Info.BTHome.Capabilities {
				if cap == "temperature" {
					return "temp-sensor"
				}
			}
		}
		return "other"
	}
	if strings.HasPrefix(id, "shellyht-") {
		return "temp-sensor"
	}
	return "heater"
}

// RoomsList renders the rooms list HTML fragment
func (h *HTMXHandler) RoomsList(w http.ResponseWriter, r *http.Request) {
	mh, err := myhome.Methods(myhome.TemperatureList)
	if err != nil {
		http.Error(w, "method not found", http.StatusInternalServerError)
		return
	}

	res, err := mh.ActionE(h.ctx, mh.Signature.NewParams())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	roomMap, ok := res.(*myhome.TemperatureRoomList)
	if !ok {
		http.Error(w, "unexpected result type", http.StatusInternalServerError)
		return
	}

	views := make([]RoomView, 0, len(*roomMap))
	for _, rc := range *roomMap {
		kinds := make([]string, len(rc.Kinds))
		for i, k := range rc.Kinds {
			kinds[i] = string(k)
		}

		var devViews []roomDeviceView
		devices, _ := h.db.GetDevicesByRoom(h.ctx, rc.RoomID)
		for _, d := range devices {
			name := d.Name()
			if name == "" {
				name = d.Id()
			}
			devViews = append(devViews, roomDeviceView{
				ID:   d.Id(),
				Name: name,
				Role: deviceRoleLabel(d),
			})
		}

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

	tmpl := template.Must(template.New("rooms-list").Funcs(template.FuncMap{
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
		"roleEmoji": func(role string) string {
			switch role {
			case "heater":
				return "🔥"
			case "temp-sensor":
				return "🌡️"
			case "door-sensor":
				return "🚪"
			default:
				return "📦"
			}
		},
	}).Parse(roomsListTemplate))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, views); err != nil {
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

	// Parse switch ID
	var sid int
	fmt.Sscanf(switchID, "%d", &sid)

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

	res, err := mh.ActionE(h.ctx, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	on, ok := res.(bool)
	if !ok {
		http.Error(w, "invalid response", http.StatusInternalServerError)
		return
	}

	// Render switch button HTML
	tmpl := template.Must(template.New("switch-button").Parse(switchButtonTemplate))

	data := map[string]interface{}{
		"DeviceID": deviceID,
		"SwitchID": switchID,
		"On":       on,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
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

const deviceCardsTemplate = `
{{range .}}
{{ $deviceId := .Id }}
<div class="column is-4-desktop is-6-tablet" data-device-name="{{.Name | lower}}">
  <div class="card" id="device-{{.Id}}">
    <div class="card-content">
      <p class="title is-5">
        {{if .DeviceTypeEmoji}}{{.DeviceTypeEmoji}} {{end}}
        <span id="device-{{.Id}}-name">{{.Name}}</span>
        {{if .HasTemperatureSensor}}
          {{if .Temperature}}
            <span class="tag is-info ml-2" id="sensor-{{.Id}}-temperature">{{printf "%.1f" .Temperature}}°C</span>
          {{else}}
            <span class="tag is-light ml-2" id="sensor-{{.Id}}-temperature">--°C</span>
          {{end}}
        {{end}}
        
        {{range $switchId, $switch := .Switches}}
          <div class="level" id="switch-{{$deviceId}}-{{$switchId}}">
            <button class="button is-rounded {{if $switch.On}}is-info is-active{{else}}is-light{{end}}" 
                    id="btn-switch-{{$deviceId}}-{{$switchId}}"
                    @click="toggleSwitch('{{$deviceId}}', '{{$switchId}}')"
                    title="Toggle switch">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" id="btn-switch-{{$deviceId}}-{{$switchId}}-shape">
                {{if $switch.On}}
                <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
                <circle cx="16" cy="12" r="3"></circle>
                {{else}}
                <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
                <circle cx="8" cy="12" r="3"></circle>
                {{end}}
              </svg>
            </button>
            <span class="level-item">{{$switch.Name}}</span>
          </div>
        {{end}}
        
        {{if .HasHumiditySensor}}
          {{if .Humidity}}
            <span class="tag is-info ml-2" id="sensor-{{.Id}}-humidity">{{printf "%.1f" .Humidity}}%</span>
          {{else}}
            <span class="tag is-light ml-2" id="sensor-{{.Id}}-humidity">--%</span>
          {{end}}
        {{end}}
        {{if .HasDoorSensor}}
          {{if ne .DoorOpened nil}}
            {{if .DoorOpened}}
              <span class="tag is-warning ml-2" id="door-{{.Id}}">
                <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                  <path d="M3 21V3h12l6 6v12H3zm2-2h14V9.828L14.172 5H5v14zm9-6h2v2h-2v-2z"/>
                </svg>
                Open
              </span>
            {{else}}
              <span class="tag is-success ml-2" id="door-{{.Id}}">
                <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                  <path d="M3 21V3h12v18H3zm2-2h8V5H5v14zm9-6h2v2h-2v-2z"/>
                </svg>
                Closed
              </span>
            {{end}}
          {{else}}
            <span class="tag is-light ml-2" id="door-{{.Id}}">
              <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                <path d="M3 21V3h12v18H3zm2-2h8V5H5v14zm9-6h2v2h-2v-2z"/>
              </svg>
              --
            </span>
          {{end}}
        {{end}}
      </p>
      <p class="subtitle is-7 has-text-grey">{{.Manufacturer}} · {{.Id}}</p>
      <div class="buttons mt-3">
        {{if .Host}}
          <a class="button is-link is-small" href="/devices/{{.LinkToken}}/" target="_blank" rel="noopener noreferrer" title="Open device web interface">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="2" y1="12" x2="22" y2="12"></line>
              <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path>
            </svg>
          </a>
        {{end}}
        {{if .IsRefreshable}}
        <button class="button is-warning is-small" 
                id="btn-refresh-{{.Id}}"
                @click="refreshDevice('{{.Id}}')"
                title="Refresh device">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="23 4 23 10 17 10"></polyline>
            <polyline points="1 20 1 14 7 14"></polyline>
            <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
          </svg>
        </button>
        {{end}}
        <button class="button is-success is-small" 
                @click="$dispatch('open-setup-modal', {deviceId: '{{.Id}}'})"
                title="Setup device">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
          </svg>
        </button>
        {{if .HasHeaterScript}}
        <button class="button is-danger is-small" 
                @click="$dispatch('open-heater-modal', {deviceId: '{{.Id}}'})"
                title="Heater Configuration">🔥</button>
        {{end}}
      </div>
    </div>
  </div>
</div>
{{end}}
`

const deviceCardTemplate = `
<div class="column is-4-desktop is-6-tablet" data-device-name="{{.Name | lower}}">
  <div class="card" id="device-{{.Id}}">
    <div class="card-content">
      <p class="title is-5">
        {{if .DeviceTypeEmoji}}{{.DeviceTypeEmoji}} {{end}}
        <span id="device-{{.Id}}-name">{{.Name}}</span>
        {{if .HasTemperatureSensor}}
          {{if .Temperature}}
            <span class="tag is-info ml-2" id="sensor-{{.Id}}-temperature">{{printf "%.1f" .Temperature}}°C</span>
          {{else}}
            <span class="tag is-light ml-2" id="sensor-{{.Id}}-temperature">--°C</span>
          {{end}}
        {{end}}
        
        {{range $switchId, $switch := .Switches}}
          <div class="level" id="switch-{{$.Id}}-{{$switchId}}">
            <button class="button is-rounded {{if $switch.On}}is-info is-active{{else}}is-light{{end}}" 
                    id="btn-switch-{{$.Id}}-{{$switchId}}"
                    @click="toggleSwitch('{{$.Id}}', '{{$switchId}}')"
                    title="Toggle switch">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" id="btn-switch-{{$.Id}}-{{$switchId}}-shape">
                {{if $switch.On}}
                <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
                <circle cx="16" cy="12" r="3"></circle>
                {{else}}
                <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
                <circle cx="8" cy="12" r="3"></circle>
                {{end}}
              </svg>
            </button>
            <span class="level-item">{{$switch.Name}}</span>
          </div>
        {{end}}
        
        {{if .HasHumiditySensor}}
          {{if .Humidity}}
            <span class="tag is-info ml-2" id="sensor-{{.Id}}-humidity">{{printf "%.1f" .Humidity}}%</span>
          {{else}}
            <span class="tag is-light ml-2" id="sensor-{{.Id}}-humidity">--%</span>
          {{end}}
        {{end}}
        {{if .HasDoorSensor}}
          {{if ne .DoorOpened nil}}
            {{if .DoorOpened}}
              <span class="tag is-warning ml-2" id="door-{{.Id}}">
                <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                  <path d="M3 21V3h12l6 6v12H3zm2-2h14V9.828L14.172 5H5v14zm9-6h2v2h-2v-2z"/>
                </svg>
                Open
              </span>
            {{else}}
              <span class="tag is-success ml-2" id="door-{{.Id}}">
                <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                  <path d="M3 21V3h12v18H3zm2-2h8V5H5v14zm9-6h2v2h-2v-2z"/>
                </svg>
                Closed
              </span>
            {{end}}
          {{else}}
            <span class="tag is-light ml-2" id="door-{{.Id}}">
              <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                <path d="M3 21V3h12v18H3zm2-2h8V5H5v14zm9-6h2v2h-2v-2z"/>
              </svg>
              --
            </span>
          {{end}}
        {{end}}
      </p>
      <p class="subtitle is-7 has-text-grey">{{.Manufacturer}} · {{.Id}}</p>
      <div class="buttons mt-3">
        {{if .Host}}
          <a class="button is-link is-small" href="/devices/{{.LinkToken}}/" target="_blank" rel="noopener noreferrer" title="Open device web interface">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="2" y1="12" x2="22" y2="12"></line>
              <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path>
            </svg>
          </a>
        {{end}}
        {{if .IsRefreshable}}
        <button class="button is-warning is-small" 
                id="btn-refresh-{{.Id}}"
                @click="refreshDevice('{{.Id}}')"
                title="Refresh device">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="23 4 23 10 17 10"></polyline>
            <polyline points="1 20 1 14 7 14"></polyline>
            <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
          </svg>
        </button>
        {{end}}
        <button class="button is-success is-small" 
                @click="$dispatch('open-setup-modal', {deviceId: '{{.Id}}'})"
                title="Setup device">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
          </svg>
        </button>
        {{if .HasHeaterScript}}
        <button class="button is-danger is-small" 
                @click="$dispatch('open-heater-modal', {deviceId: '{{.Id}}'})"
                title="Heater Configuration">🔥</button>
        {{end}}
      </div>
    </div>
  </div>
</div>
`

const roomsListTemplate = `
{{if .}}
  {{range .}}
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
      <div class="mt-2">
        {{range .Devices}}
        <p class="is-size-7">{{roleEmoji .Role}} <a href="/devices/{{.ID}}/" target="_blank" rel="noopener noreferrer">{{.Name}}</a></p>
        {{end}}
      </div>
      {{end}}
    </div>
  </div>
  {{end}}
{{else}}
  <div class="column is-12">
    <p class="has-text-grey">No rooms configured yet. Click <strong>+ Add Room</strong> to create one.</p>
  </div>
{{end}}
`

const switchButtonTemplate = `
<button class="button is-rounded {{if .On}}is-info is-active{{else}}is-light{{end}}" 
        hx-post="/htmx/switch/toggle"
        hx-vals='{"device_id": "{{.DeviceID}}", "switch_id": "{{.SwitchID}}"}'
        hx-target="this"
        hx-swap="outerHTML"
        title="Toggle switch">
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
    {{if .On}}
    <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
    <circle cx="16" cy="12" r="3"></circle>
    {{else}}
    <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
    <circle cx="8" cy="12" r="3"></circle>
    {{end}}
  </svg>
</button>
`
