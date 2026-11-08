package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"
)

// powerView is the data of one round state button (the "power" template). It
// is also what the toggle route renders, so the button a tap returns is the
// button the row started with.
type powerView struct {
	DeviceID string
	SwitchID int
	Label    string // accessible name: the row title
	Known    bool   // false until the daemon has seen the switch state: the button is shown disabled
	On       bool   // logical state, after the device's polarity
	Starting bool   // unknown, and the daemon has not yet had its turn to look at the device (#763)
}

// deviceRow is one row of the Home grid: one per switch, or one per device
// when it has none.
type deviceRow struct {
	D         *DeviceView
	Title     string
	First     bool // pills are shown on the first row of a device only
	HasSwitch bool
	Power     powerView
}

// deviceRows expands a device into its rows. A device with a single switch is
// one row titled with the device name; several switches give "Device · Switch".
func deviceRows(d DeviceView) []deviceRow {
	ids := make([]int, 0, len(d.Switches))
	for id := range d.Switches {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) == 0 {
		return []deviceRow{{D: &d, Title: d.Name, First: true}}
	}
	rows := make([]deviceRow, 0, len(ids))
	for i, id := range ids {
		sw := d.Switches[id]
		title := d.Name
		if len(ids) > 1 {
			name := sw.Name
			if name == "" {
				name = fmt.Sprintf("Switch %d", id)
			}
			title = d.Name + " · " + name
		}
		rows = append(rows, deviceRow{
			D: &d, Title: title, First: i == 0, HasSwitch: true,
			Power: powerView{DeviceID: d.Id, SwitchID: id, Label: title, Known: !d.SwitchUnknown[id], On: sw.On,
				Starting: d.Starting && d.SwitchUnknown[id]},
		})
	}
	return rows
}

// rowTemplateFuncs: isActive/turnoverText/f1 exist because html/template does
// not auto-dereference *bool/*float64 fields for {{if}} or {{printf}} — a
// non-nil pointer is always "truthy" regardless of the value it points to, and
// printf on a pointer prints its address, not the pointed-to value.
func rowTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"lower": strings.ToLower,
		"rows":  deviceRows,
		"isActive": func(b *bool) bool {
			return b != nil && *b
		},
		"turnoverText": func(achieved, target *float64) string {
			if achieved == nil || target == nil {
				return ""
			}
			return fmt.Sprintf("%.1f/%.1f x/day", *achieved, *target)
		},
		"f1": func(f *float64) string {
			if f == nil {
				return ""
			}
			return fmt.Sprintf("%.1f", *f)
		},
		// vals is the hx-vals JSON of the state button (html/template escapes it for the attribute).
		"vals": func(p powerView) string {
			b, _ := json.Marshal(map[string]string{
				"device_id": p.DeviceID,
				"switch_id": fmt.Sprint(p.SwitchID),
				"label":     p.Label,
			})
			return string(b)
		},
	}
}

// deviceRowTmpl is parsed once. "devices" renders the whole Home list,
// "device" one device (the SSE refresh target), "power" the state button.
var deviceRowTmpl = template.Must(template.New("rows").Funcs(rowTemplateFuncs()).Parse(deviceRowsTemplate))

// Every control is at least --mh-tap square (myhome.css). The row's main
// button carries its data in data-* attributes (never in script text) and
// opens the device sheet.
const deviceRowsTemplate = `
{{define "devices"}}{{range .}}{{template "device" .}}{{end}}{{end}}

{{define "device"}}<div class="mh-device" id="device-{{.Id}}" data-device-name="{{.Name | lower}}">{{range rows .}}{{template "row" .}}{{end}}</div>{{end}}

{{define "row"}}
<div class="mh-row">
  <button type="button" class="mh-row-main"
          data-device-id="{{.D.Id}}" data-name="{{.D.Name}}" data-manufacturer="{{.D.Manufacturer}}"
          data-refreshable="{{if .D.IsRefreshable}}1{{end}}" data-heater="{{if .D.HasHeaterScript}}1{{end}}"
          @click="openDeviceSheet($el.dataset)">
    <span class="mh-avatar" aria-hidden="true">{{if .HasSwitch}}<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-4 10.5c.7.7 1 1.5 1 2.5h6c0-1 .3-1.8 1-2.500A6 6 0 0 0 12 3z"/></svg>{{else}}<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><circle cx="12" cy="12" r="9"/></svg>{{end}}</span>
    <span class="mh-row-text">
      <span class="mh-row-name"{{if .First}} id="device-{{.D.Id}}-name"{{end}}>{{.Title}}</span>
      {{if .Power.Starting}}<span class="mh-pill is-unknown mh-starting" id="starting-{{.D.Id}}-{{.Power.SwitchID}}">Starting…</span>{{end}}
      {{if .First}}<span class="mh-tags">
        {{if .D.HasTemperatureSensor}}<span class="mh-pill{{if not .D.Temperature}} is-unknown{{end}}" id="sensor-{{.D.Id}}-temperature">{{if .D.Temperature}}{{f1 .D.Temperature}}{{else}}--{{end}}°C</span>{{end}}
        {{if .D.HasHumiditySensor}}<span class="mh-pill{{if not .D.Humidity}} is-unknown{{end}}" id="sensor-{{.D.Id}}-humidity">{{if .D.Humidity}}{{f1 .D.Humidity}}{{else}}--{{end}}%</span>{{end}}
        {{if .D.HasDoorSensor}}<span class="mh-pill{{if not .D.DoorOpened}} is-unknown{{else if isActive .D.DoorOpened}} is-alert{{end}}" id="door-{{.D.Id}}"><svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M3 21V3h12v18H3zm2-2h8V5H5v14zm9-6h2v2h-2v-2z"/></svg><span data-door-text>{{if not .D.DoorOpened}}--{{else if isActive .D.DoorOpened}}Open{{else}}Closed{{end}}</span></span>{{end}}
        {{if .D.IsPoolPump}}<span class="mh-pill{{if not .D.WaterSupplyActive}} is-unknown{{else if isActive .D.WaterSupplyActive}} is-alert{{end}}" id="water-supply-{{.D.Id}}">{{if not .D.WaterSupplyActive}}Water supply --{{else if isActive .D.WaterSupplyActive}}Water supply paused{{else}}Water supply OK{{end}}</span>
        <span class="mh-pill is-unknown" id="turnover-{{.D.Id}}">Turnover 24 h {{with turnoverText .D.TurnoverAchieved .D.TurnoverTarget}}{{.}}{{else}}--{{end}}</span>{{end}}
      </span>{{end}}
    </span>
  </button>
  {{if .HasSwitch}}{{template "power" .Power}}{{end}}
</div>
{{end}}

{{define "power"}}<button type="button" class="mh-power{{if not .Known}} is-unknown{{else if .On}} is-on{{end}}" id="btn-switch-{{.DeviceID}}-{{.SwitchID}}"
        {{if .Known}}hx-post="/htmx/switch/toggle" hx-vals="{{vals .}}" hx-target="this" hx-swap="outerHTML" hx-disabled-elt="this"{{else}}disabled{{end}}
        aria-label="Toggle {{.Label}}{{if .Starting}} (starting){{end}}" aria-pressed="{{if .On}}true{{else}}false{{end}}"><svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/></svg></button>{{end}}
`
