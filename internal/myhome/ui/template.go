package ui

import (
	"context"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strings"

	"myhome/storage"
)

// Embed static assets under this package
//
//go:embed static/*
var staticFS embed.FS

// StaticFS returns the embedded static filesystem
func StaticFS() (fs.FS, error) {
	return fs.Sub(staticFS, "static")
}

// StaticFileServer returns an http.Handler for serving static files
func StaticFileServer() (http.Handler, error) {
	sub, err := StaticFS()
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(sub)), nil
}

// DeviceView represents a device for rendering in the UI
type DeviceView struct {
	Name         string
	Id           string
	Manufacturer string
	Host         string
	LinkToken    string
}

// IndexData holds the data for rendering the index page
type IndexData struct {
	Devices []DeviceView
}

// index template and renderer
var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>MyHome Devices</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bulma@0.9.4/css/bulma.min.css"/>
  <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='0.9em' font-size='90'>🏠</text></svg>"/>
  </head>
<body>
  <section class="hero is-light is-small">
    <div class="hero-body">
      <div class="container">
        <div class="level">
          <div class="level-left">
            <h1 class="title is-3">MyHome</h1>
            <span class="subtitle is-6 ml-3">Known devices ({{len .Devices}})</span>
          </div>
          <div class="level-right">
            <a class="button is-info" href="https://control.shelly.cloud" target="_blank" rel="noopener noreferrer">Shelly Control</a>
            <a class="button is-link ml-2" href="https://community.shelly.cloud/" target="_blank" rel="noopener noreferrer">Community Forum</a>
          </div>
        </div>
      </div>
    </div>
  </section>
  <section class="section">
    <div class="container">
      {{if .Devices}}
      <div class="columns is-multiline">
        {{range .Devices}}
        <div class="column is-4-desktop is-6-tablet">
          <div class="card">
            <div class="card-content">
              <p class="title is-5">{{.Name}}</p>
              <p class="subtitle is-7 has-text-grey">{{.Manufacturer}} · {{.Id}}</p>
              {{if .Host}}
                <p class="has-text-grey">Host: {{.Host}}</p>
              {{else}}
                <p class="has-text-grey">No host known</p>
              {{end}}
              <div class="buttons mt-3">
                {{if .Host}}
                  <a class="button is-link" href="/devices/{{.LinkToken}}/" target="_blank" rel="noopener noreferrer">Open</a>
                {{end}}
                <button class="button is-warning" id="btn-refresh-{{.Id}}" onclick="refreshDevice('{{.Id}}')">Refresh</button>
                <button class="button is-success" id="btn-setup-{{.Id}}" onclick="setupDevice('{{.Id}}')">Setup</button>
              </div>
            </div>
          </div>
        </div>
        {{end}}
      </div>
      {{else}}
        <div class="notification is-light has-text-grey is-size-6">No devices found.</div>
      {{end}}
    </div>
  </section>
  <!-- Setup Modal -->
  <div class="modal" id="setup-modal">
    <div class="modal-background" onclick="closeSetupModal()"></div>
    <div class="modal-card">
      <header class="modal-card-head">
        <p class="modal-card-title">Setup Device</p>
        <button class="delete" aria-label="close" onclick="closeSetupModal()"></button>
      </header>
      <section class="modal-card-body">
        <input type="hidden" id="setup-device-id">
        <div class="field">
          <label class="label">Device Name</label>
          <div class="control">
            <input class="input" type="text" id="setup-name" placeholder="Leave empty for auto-derivation">
          </div>
          <p class="help">Overrides automatic name derivation from output/input names</p>
        </div>
        <div class="field">
          <label class="label">MQTT Broker</label>
          <div class="control">
            <input class="input" type="text" id="setup-mqtt-broker" placeholder="Leave empty for default">
          </div>
          <p class="help">e.g., 192.168.1.100:1883 or mqtt.local</p>
        </div>
        <hr>
        <p class="has-text-weight-semibold mb-2">WiFi Settings (optional)</p>
        <div class="field">
          <label class="label">STA ESSID</label>
          <div class="control">
            <input class="input" type="text" id="setup-sta-essid" placeholder="Primary WiFi network name">
          </div>
        </div>
        <div class="field">
          <label class="label">STA Password</label>
          <div class="control">
            <input class="input" type="password" id="setup-sta-passwd" placeholder="Primary WiFi password">
          </div>
        </div>
        <div class="field">
          <label class="label">STA1 ESSID</label>
          <div class="control">
            <input class="input" type="text" id="setup-sta1-essid" placeholder="Backup WiFi network name">
          </div>
        </div>
        <div class="field">
          <label class="label">STA1 Password</label>
          <div class="control">
            <input class="input" type="password" id="setup-sta1-passwd" placeholder="Backup WiFi password">
          </div>
        </div>
        <div class="field">
          <label class="label">AP Password</label>
          <div class="control">
            <input class="input" type="password" id="setup-ap-passwd" placeholder="Access Point password">
          </div>
        </div>
      </section>
      <footer class="modal-card-foot">
        <button class="button is-success" id="setup-submit-btn" onclick="submitSetup()">Setup</button>
        <button class="button" onclick="closeSetupModal()">Cancel</button>
      </footer>
    </div>
  </div>

  <footer class="footer">
    <div class="content has-text-centered has-text-grey-light is-size-7">
      Served by MyHome reverse proxy
    </div>
  </footer>
  <script>
    async function refreshDevice(id) {
      const btn = document.getElementById('btn-refresh-' + id);
      if (btn) { btn.disabled = true; btn.textContent = 'Refreshing…'; }
      let es;
      try {
        es = new EventSource('/events?device=' + encodeURIComponent(id));
        es.addEventListener('device-refresh', function(ev) {
          if (btn) { btn.textContent = 'Done'; }
          try { es.close(); } catch {}
          location.reload();
        });
        es.onerror = function() { /* keep open or close silently */ };

        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'device.refresh', params: id })
        });
        if (!res.ok) {
          const t = await res.text();
          if (btn) { btn.disabled = false; btn.textContent = 'Refresh'; }
          alert('Refresh failed: ' + t);
          if (es) try { es.close(); } catch {}
          return;
        }
        // If backend completed before SSE connected, fallback: reload soon
        setTimeout(() => { if (btn) { btn.textContent = 'Done'; } location.reload(); }, 1500);
      } catch (e) {
        if (btn) { btn.disabled = false; btn.textContent = 'Refresh'; }
        if (es) try { es.close(); } catch {}
        alert('Refresh error: ' + e);
      }
    }

    function setupDevice(id) {
      // Open modal and store device ID
      document.getElementById('setup-device-id').value = id;
      // Clear all fields
      document.getElementById('setup-name').value = '';
      document.getElementById('setup-mqtt-broker').value = '';
      document.getElementById('setup-sta-essid').value = '';
      document.getElementById('setup-sta-passwd').value = '';
      document.getElementById('setup-sta1-essid').value = '';
      document.getElementById('setup-sta1-passwd').value = '';
      document.getElementById('setup-ap-passwd').value = '';
      // Show modal
      document.getElementById('setup-modal').classList.add('is-active');
    }

    function closeSetupModal() {
      document.getElementById('setup-modal').classList.remove('is-active');
    }

    async function submitSetup() {
      const id = document.getElementById('setup-device-id').value;
      const btn = document.getElementById('setup-submit-btn');
      const deviceBtn = document.getElementById('btn-setup-' + id);
      
      // Build params object, only including non-empty values
      const params = { identifier: id };
      const name = document.getElementById('setup-name').value.trim();
      const mqttBroker = document.getElementById('setup-mqtt-broker').value.trim();
      const staEssid = document.getElementById('setup-sta-essid').value.trim();
      const staPasswd = document.getElementById('setup-sta-passwd').value;
      const sta1Essid = document.getElementById('setup-sta1-essid').value.trim();
      const sta1Passwd = document.getElementById('setup-sta1-passwd').value;
      const apPasswd = document.getElementById('setup-ap-passwd').value;
      
      if (name) params.name = name;
      if (mqttBroker) params.mqtt_broker = mqttBroker;
      if (staEssid) params.sta_essid = staEssid;
      if (staPasswd) params.sta_passwd = staPasswd;
      if (sta1Essid) params.sta1_essid = sta1Essid;
      if (sta1Passwd) params.sta1_passwd = sta1Passwd;
      if (apPasswd) params.ap_passwd = apPasswd;

      if (btn) { btn.disabled = true; btn.textContent = 'Setting up…'; }
      if (deviceBtn) { deviceBtn.disabled = true; deviceBtn.textContent = 'Setting up…'; }
      
      try {
        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'device.setup', params: params })
        });
        if (!res.ok) {
          const t = await res.text();
          if (btn) { btn.disabled = false; btn.textContent = 'Setup'; }
          if (deviceBtn) { deviceBtn.disabled = false; deviceBtn.textContent = 'Setup'; }
          alert('Setup failed: ' + t);
          return;
        }
        if (btn) { btn.textContent = 'Done'; }
        if (deviceBtn) { deviceBtn.textContent = 'Done'; }
        closeSetupModal();
        // Reload to show updated device info
        setTimeout(() => location.reload(), 500);
      } catch (e) {
        if (btn) { btn.disabled = false; btn.textContent = 'Setup'; }
        if (deviceBtn) { deviceBtn.disabled = false; deviceBtn.textContent = 'Setup'; }
        alert('Setup error: ' + e);
      }
    }
  </script>
</body>
</html>`))

// RenderIndex renders the index page with device list
func RenderIndex(ctx context.Context, db *storage.DeviceStorage, w io.Writer) error {
	data := IndexData{Devices: []DeviceView{}}
	if db != nil {
		devices, err := db.GetAllDevices(ctx)
		if err != nil {
			return indexTmpl.Execute(w, data)
		}
		for _, d := range devices {
			name := d.Name()
			if name == "" {
				name = d.Id()
			}
			host := d.Host()
			token := host
			if token == "" {
				token = d.Name()
				if token == "" {
					token = d.Id()
				}
			}
			data.Devices = append(data.Devices, DeviceView{
				Name:         name,
				Id:           d.Id(),
				Manufacturer: d.Manufacturer(),
				Host:         host,
				LinkToken:    token,
			})
		}
		sort.Slice(data.Devices, func(i, j int) bool {
			return strings.ToLower(data.Devices[i].Name) < strings.ToLower(data.Devices[j].Name)
		})
	}
	return indexTmpl.Execute(w, data)
}
