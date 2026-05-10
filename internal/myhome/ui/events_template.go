package ui

import (
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
			case "debug":
				return "has-text-grey-light"
			default:
				return ""
			}
		},
	}
}

// eventRowTemplate is the shared <tr> fragment for a single event.
// Used both in the HTMX table and in BroadcastEvent SSE push.
const eventRowTemplate = `<tr class="{{severityClass .Severity}}">
  <td>{{formatTime .Ts}}</td>
  <td>{{.DeviceID}}</td>
  <td>{{.Component}}</td>
  <td>{{.Event}}</td>
  <td>{{.Severity}}</td>
  <td>{{truncate .Data}}</td>
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
      <input class="input is-small" type="text" name="device" placeholder="Device ID"
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
          <option value="alarm" {{if eq .Severity "alarm"}}selected{{end}}>alarm</option>
          <option value="warn"  {{if eq .Severity "warn"}}selected{{end}}>warn</option>
          <option value="info"  {{if eq .Severity "info"}}selected{{end}}>info</option>
          <option value="debug" {{if eq .Severity "debug"}}selected{{end}}>debug</option>
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

// eventLogPageHTML is the full-page event log HTML template.
// Follows the same structure as static/index.html.
const eventLogPageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>MyHome – Event Log</title>
  <link rel="stylesheet" href="/static/bulma.min.css"/>
  <link rel="icon" href="/static/penates.svg" type="image/svg+xml"/>
  <script defer src="/static/alpine.min.js"></script>
  <script src="/static/htmx.min.js"></script>
  <style>[x-cloak] { display: none !important; }</style>
</head>
<body x-data="eventLogApp()">

  <!-- Hero Section -->
  <section class="hero is-light is-small">
    <div class="hero-body">
      <div class="container">
        <div class="level">
          <div class="level-left">
            <button class="button is-light is-small mr-3" @click="window.location.reload()" title="Reload page">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="23 4 23 10 17 10"></polyline>
                <polyline points="1 20 1 14 7 14"></polyline>
                <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
              </svg>
            </button>
            <h1 class="title is-3">MyHome</h1>
            <span class="subtitle is-6 ml-3">{{.Version}}</span>
          </div>
          <div class="level-right">
            <a class="button" href="/">Devices</a>
            <a class="button is-info ml-2" href="/event-log">Event Log</a>
          </div>
        </div>
      </div>
    </div>
  </section>

  <!-- Event Log Section -->
  <section class="section">
    <div class="container">
      <h2 class="title is-4">Event Log</h2>
      <div id="events-table"
           hx-get="/htmx/events"
           hx-trigger="load"
           hx-swap="innerHTML">
        <p class="has-text-grey">Loading events...</p>
      </div>
    </div>
  </section>

  <footer class="footer">
    <div class="content has-text-centered has-text-grey-light is-size-7">
      Served by MyHome reverse proxy · Powered by Alpine.js &amp; HTMX
    </div>
  </footer>

  <script>
    function eventLogApp() {
      return {
        init() {
          this.connectSSE();
        },

        connectSSE() {
          var self = this;
          var eventSource = new EventSource('/events');

          eventSource.addEventListener('connected', function() {
            console.log('SSE: connected (event-log page)');
          });

          eventSource.addEventListener('eventlog', function(e) {
            var tbody = document.getElementById('events-tbody');
            if (!tbody) { return; }
            var tmp = document.createElement('tbody');
            tmp.innerHTML = e.data;
            var row = tmp.firstElementChild;
            if (row) {
              tbody.insertBefore(row, tbody.firstChild);
            }
          });

          eventSource.onerror = function(e) {
            console.error('SSE: error:', e);
          };
        }
      };
    }
  </script>
</body>
</html>`
