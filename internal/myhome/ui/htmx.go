package ui

import (
	"context"
	"fmt"
	"html/template"
	"myhome"
	"myhome/storage"
	"net/http"
	"sort"
	"strings"

	"github.com/go-logr/logr"
)

// HTMXHandler returns handlers for HTMX partial HTML responses
type HTMXHandler struct {
	ctx context.Context
	log logr.Logger
	db  *storage.DeviceStorage
}

// NewHTMXHandler creates a new HTMX handler
func NewHTMXHandler(ctx context.Context, log logr.Logger, db *storage.DeviceStorage) *HTMXHandler {
	return &HTMXHandler{
		ctx: ctx,
		log: log,
		db:  db,
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

// RoomsList renders the rooms list HTML fragment
func (h *HTMXHandler) RoomsList(w http.ResponseWriter, r *http.Request) {
	// Call the temperature.list RPC method
	mh, err := myhome.Methods(myhome.TemperatureList)
	if err != nil {
		http.Error(w, "method not found", http.StatusInternalServerError)
		return
	}

	params := mh.Signature.NewParams()
	res, err := mh.ActionE(h.ctx, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.New("rooms-list").Parse(roomsListTemplate))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, res); err != nil {
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
{{$rooms := .}}
{{if $rooms}}
  {{range $roomId, $room := $rooms}}
    <div class="column is-4">
      <div class="box">
        <div class="level mb-2">
          <div class="level-left"><strong>{{if $room.Name}}{{$room.Name}}{{else}}{{$roomId}}{{end}}</strong></div>
          <div class="level-right">
            <button class="button is-small is-info is-outlined mr-1" 
                    @click="$dispatch('edit-room', {roomId: '{{$roomId}}'})"
                    title="Edit">✏️</button>
            <button class="button is-small is-danger is-outlined" 
                    @click="$dispatch('delete-room', {roomId: '{{$roomId}}', roomName: '{{if $room.Name}}{{$room.Name}}{{else}}{{$roomId}}{{end}}'})"
                    title="Delete">🗑️</button>
          </div>
        </div>
        <p class="is-size-7 has-text-grey">ID: {{$roomId}}</p>
        {{if $room.Kinds}}
          <p class="is-size-7">Types: {{range $i, $k := $room.Kinds}}{{if $i}}, {{end}}{{$k}}{{end}}</p>
        {{end}}
        {{if $room.Levels}}
          <p class="is-size-7">Levels: 
            {{range $k, $v := $room.Levels}}{{$k}}: {{$v}}°C {{end}}
          </p>
        {{end}}
      </div>
    </div>
  {{end}}
{{else}}
  <div class="column is-12">
    <p class="has-text-grey">No rooms configured yet.</p>
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
