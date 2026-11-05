package ui

import (
	"bytes"
	"encoding/json"
	"html/template"
	"time"
)

// eventTemplateFuncs returns the template.FuncMap used by all events templates.
func eventTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatTime": func(ts float64) string {
			t := time.Unix(int64(ts), 0)
			return t.Format("2006-01-02 15:04:05")
		},
		"truncate": func(s *string) string {
			if s == nil {
				return ""
			}
			if len(*s) > 60 {
				return (*s)[:60] + "…"
			}
			return *s
		},
		"severityClass": func(sev string) string {
			switch sev {
			case "alarm":
				return "has-text-danger"
			case "warn":
				return "has-text-warning"
			case "notice":
				return "has-text-link"
			case "debug":
				return "has-text-grey-light"
			default:
				return ""
			}
		},
		// eventDataCell renders Data as a collapsible <details> when the value is
		// valid JSON, or a plain span otherwise. Returns template.HTML so the
		// caller does not need to mark it safe.
		"eventDataCell": func(s *string) template.HTML {
			if s == nil || *s == "" {
				return ""
			}
			raw := *s
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, []byte(raw), "", "  "); err != nil {
				// Not JSON — render as escaped plain text
				return template.HTML("<span>" + template.HTMLEscapeString(raw) + "</span>")
			}
			preview := raw
			if len(preview) > 60 {
				preview = preview[:60] + "…"
			}
			return template.HTML(
				`<details class="event-data">` +
					`<summary><code>` + template.HTMLEscapeString(preview) + `</code></summary>` +
					`<pre style="max-height:12em;overflow-y:auto">` + template.HTMLEscapeString(pretty.String()) + `</pre>` +
					`</details>`,
			)
		},
	}
}

// eventRowTemplate is the shared <tr> fragment for a single event.
// Used both in the HTMX table and in BroadcastEvent SSE push.
// Columns: Time | Device (name + id below) | Component | Event | Severity | Data
const eventRowTemplate = `<tr class="{{severityClass .Severity}}">
  <td>{{formatTime .Ts}}</td>
  <td>{{if .DeviceName}}{{.DeviceName}}<br><code class="is-size-7 has-text-grey">{{.DeviceID}}</code>{{else}}{{.DeviceID}}{{end}}</td>
  <td>{{.Component}}</td>
  <td>{{.Event}}</td>
  <td>{{.Severity}}</td>
  <td>{{eventDataCell .Data}}</td>
</tr>`

// eventsRowsTemplate renders a list of <tr> rows plus a "Load more" button.
const eventsRowsTemplate = `{{range .Events}}` + eventRowTemplate + `
{{end}}
{{if .Events}}
<tr id="load-more-row">
  <td colspan="6" class="has-text-centered">
    <button class="button is-small is-light"
            hx-get="/htmx/events/more?offset={{.Offset}}&device={{.Device}}&type={{.Type}}&severity={{.Severity}}"
            hx-swap="outerHTML"
            hx-target="#load-more-row">
      Load more
    </button>
  </td>
</tr>
{{end}}`

// eventsTableTemplate renders the full events table fragment (filter bar + table).
const eventsTableTemplate = `<form id="events-filter-form"
      hx-get="/htmx/events"
      hx-trigger="change from:#events-filter-form, keyup delay:400ms from:#events-filter-form input"
      hx-target="#events-table"
      hx-swap="innerHTML">
  <div class="field is-grouped is-grouped-multiline mb-4">
    <div class="control">
      <input class="input is-small" type="text" name="device" placeholder="Device name, ID or MAC"
             value="{{.Device}}">
    </div>
    <div class="control">
      <input class="input is-small" type="text" name="type" placeholder="Event type (e.g. switch)"
             value="{{.Type}}">
    </div>
    <div class="control">
      <div class="select is-small">
        <select name="severity">
          <option value="" {{if eq .Severity ""}}selected{{end}}>All severities</option>
          <option value="alarm"  {{if eq .Severity "alarm"}}selected{{end}}>alarm</option>
          <option value="warn"   {{if eq .Severity "warn"}}selected{{end}}>warn</option>
          <option value="notice" {{if eq .Severity "notice"}}selected{{end}}>notice</option>
          <option value="info"   {{if eq .Severity "info"}}selected{{end}}>info</option>
          <option value="debug"  {{if eq .Severity "debug"}}selected{{end}}>debug</option>
        </select>
      </div>
    </div>
  </div>
</form>
<div class="table-container">
  <table class="table is-fullwidth is-striped is-hoverable is-narrow">
    <thead>
      <tr>
        <th>Time</th>
        <th>Device</th>
        <th>Component</th>
        <th>Event</th>
        <th>Severity</th>
        <th>Data</th>
      </tr>
    </thead>
    <tbody id="events-tbody">
      {{range .Events}}` + eventRowTemplate + `
      {{end}}
      {{if .Events}}
      <tr id="load-more-row">
        <td colspan="6" class="has-text-centered">
          <button class="button is-small is-light"
                  hx-get="/htmx/events/more?offset={{.Offset}}&device={{.Device}}&type={{.Type}}&severity={{.Severity}}"
                  hx-swap="outerHTML"
                  hx-target="#load-more-row">
            Load more
          </button>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{if not .Events}}
  <p class="has-text-grey has-text-centered">No events in the last 24 hours.</p>
  {{end}}
</div>`
