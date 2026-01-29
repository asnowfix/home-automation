package ui

import (
	"context"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"myhome"
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
	Name            string
	Id              string
	Manufacturer    string
	Host            string
	LinkToken       string
	HasHeaterScript bool
	DeviceTypeEmoji string // Emoji indicating device type (e.g., 🌡️ for thermometer, 🚶 for motion)
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
              <p class="title is-5">{{if .DeviceTypeEmoji}}{{.DeviceTypeEmoji}} {{end}}{{.Name}}</p>
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
                {{if .HasHeaterScript}}
                <button class="button is-danger" id="btn-heater-{{.Id}}" onclick="openHeaterConfig('{{.Id}}')" title="Heater Configuration">🔥</button>
                {{end}}
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

  <!-- Rooms Management Section -->
  <section class="section pt-0">
    <div class="container">
      <div class="level">
        <div class="level-left">
          <h2 class="title is-4">🏠 Rooms</h2>
        </div>
        <div class="level-right">
          <button class="button is-primary is-small" onclick="showAddRoomModal()">+ Add Room</button>
        </div>
      </div>
      <div id="rooms-list" class="columns is-multiline">
        <div class="column is-12">
          <p class="has-text-grey">Loading rooms...</p>
        </div>
      </div>
    </div>
  </section>

  <!-- Room Edit Modal -->
  <div class="modal" id="room-edit-modal">
    <div class="modal-background" onclick="closeRoomEditModal()"></div>
    <div class="modal-card" style="max-width: 500px;">
      <header class="modal-card-head">
        <p class="modal-card-title" id="room-edit-title">Edit Room</p>
        <button class="delete" aria-label="close" onclick="closeRoomEditModal()"></button>
      </header>
      <section class="modal-card-body">
        <input type="hidden" id="room-edit-id">
        <input type="hidden" id="room-edit-mode" value="edit">
        <div class="field">
          <label class="label">Room ID</label>
          <div class="control">
            <input class="input" type="text" id="room-edit-slug" placeholder="e.g., living-room">
          </div>
          <p class="help">Unique identifier (lowercase, hyphens only)</p>
        </div>
        <div class="field">
          <label class="label">Room Name</label>
          <div class="control">
            <input class="input" type="text" id="room-edit-name" placeholder="e.g., Living Room">
          </div>
        </div>
        <div class="field">
          <label class="label">Room Types</label>
          <div class="control">
            <label class="checkbox mr-3"><input type="checkbox" id="room-kind-bedroom" value="bedroom"> Bedroom</label>
            <label class="checkbox mr-3"><input type="checkbox" id="room-kind-office" value="office"> Office</label>
            <label class="checkbox mr-3"><input type="checkbox" id="room-kind-living-room" value="living-room"> Living Room</label>
            <label class="checkbox mr-3"><input type="checkbox" id="room-kind-kitchen" value="kitchen"> Kitchen</label>
            <label class="checkbox"><input type="checkbox" id="room-kind-other" value="other"> Other</label>
          </div>
        </div>
        <hr>
        <p class="has-text-weight-semibold mb-2">Temperature Levels (°C)</p>
        <div class="columns">
          <div class="column">
            <div class="field">
              <label class="label is-small">Eco</label>
              <input class="input is-small" type="number" id="room-level-eco" step="0.5" value="18">
            </div>
          </div>
          <div class="column">
            <div class="field">
              <label class="label is-small">Comfort</label>
              <input class="input is-small" type="number" id="room-level-comfort" step="0.5" value="20">
            </div>
          </div>
          <div class="column">
            <div class="field">
              <label class="label is-small">Away</label>
              <input class="input is-small" type="number" id="room-level-away" step="0.5" value="15">
            </div>
          </div>
        </div>
      </section>
      <footer class="modal-card-foot">
        <button class="button is-success" id="room-edit-submit" onclick="submitRoomEdit()">Save</button>
        <button class="button" onclick="closeRoomEditModal()">Cancel</button>
        <button class="button is-danger is-outlined ml-auto" id="room-delete-btn" onclick="deleteRoom()" style="display:none;">Delete</button>
      </footer>
    </div>
  </div>

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

  <!-- Heater Configuration Modal -->
  <div class="modal" id="heater-modal">
    <div class="modal-background" onclick="closeHeaterModal()"></div>
    <div class="modal-card" style="max-width: 600px;">
      <header class="modal-card-head">
        <p class="modal-card-title">🔥 Heater Configuration</p>
        <button class="delete" aria-label="close" onclick="closeHeaterModal()"></button>
      </header>
      <section class="modal-card-body">
        <input type="hidden" id="heater-device-id">
        <div id="heater-loading" class="has-text-centered py-4">
          <span class="icon is-large"><i class="fas fa-spinner fa-spin"></i></span>
          <p>Loading configuration...</p>
        </div>
        <div id="heater-config-form" style="display:none;">
          <div class="field">
            <label class="label">Room</label>
            <div class="field has-addons">
              <div class="control is-expanded">
                <div class="select is-fullwidth">
                  <select id="heater-room-id">
                    <option value="">-- Select Room --</option>
                  </select>
                </div>
              </div>
              <div class="control">
                <button class="button is-info" onclick="showAddRoomModalFromHeater()" title="Add new room">+</button>
              </div>
            </div>
          </div>
          <hr>
          <p class="has-text-weight-semibold mb-2">Temperature Sensors</p>
          <div class="field">
            <label class="label">Internal Temperature Sensor</label>
            <div class="control">
              <div class="select is-fullwidth">
                <select id="heater-internal-temp">
                  <option value="">-- Select Sensor --</option>
                </select>
              </div>
            </div>
            <p class="help">Sensor inside the heated space</p>
          </div>
          <div class="field">
            <label class="label">External Temperature Sensor</label>
            <div class="control">
              <div class="select is-fullwidth">
                <select id="heater-external-temp">
                  <option value="">-- Select Sensor --</option>
                </select>
              </div>
            </div>
            <p class="help">Sensor outside (for weather compensation)</p>
          </div>
          <hr>
          <p class="has-text-weight-semibold mb-2">Cheap Electricity Window</p>
          <div class="columns">
            <div class="column">
              <div class="field">
                <label class="label">Start Hour</label>
                <div class="control">
                  <input class="input" type="number" id="heater-cheap-start" min="0" max="23" value="23">
                </div>
              </div>
            </div>
            <div class="column">
              <div class="field">
                <label class="label">End Hour</label>
                <div class="control">
                  <input class="input" type="number" id="heater-cheap-end" min="0" max="23" value="7">
                </div>
              </div>
            </div>
          </div>
          <hr>
          <p class="has-text-weight-semibold mb-2">Advanced Settings</p>
          <div class="field">
            <label class="label">Preheat Hours</label>
            <div class="control">
              <input class="input" type="number" id="heater-preheat-hours" min="0" max="12" value="2">
            </div>
            <p class="help">Hours before comfort time to start preheating</p>
          </div>
          <div class="field">
            <label class="label">Poll Interval (ms)</label>
            <div class="control">
              <input class="input" type="number" id="heater-poll-interval" min="10000" max="300000" step="1000" value="60000">
            </div>
          </div>
          <div class="field">
            <label class="checkbox">
              <input type="checkbox" id="heater-normally-closed">
              Normally Closed Relay
            </label>
            <p class="help">Check if the relay is normally closed (heater ON when relay OFF)</p>
          </div>
          <div class="field">
            <label class="checkbox">
              <input type="checkbox" id="heater-enable-logging" checked>
              Enable Logging
            </label>
          </div>
        </div>
      </section>
      <footer class="modal-card-foot">
        <button class="button is-success" id="heater-submit-btn" onclick="submitHeaterConfig()">Save</button>
        <button class="button" onclick="closeHeaterModal()">Cancel</button>
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
      // Reset submit button state
      const btn = document.getElementById('setup-submit-btn');
      if (btn) { btn.disabled = false; btn.textContent = 'Setup'; }
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

    // Heater configuration functions
    const heaterKVSKeys = {
      'script/heater/enable-logging': 'heater-enable-logging',
      'script/heater/room-id': 'heater-room-id',
      'script/heater/cheap-start-hour': 'heater-cheap-start',
      'script/heater/cheap-end-hour': 'heater-cheap-end',
      'script/heater/poll-interval-ms': 'heater-poll-interval',
      'script/heater/preheat-hours': 'heater-preheat-hours',
      'normally-closed': 'heater-normally-closed',
      'script/heater/internal-temperature-topic': 'heater-internal-temp',
      'script/heater/external-temperature-topic': 'heater-external-temp'
    };

    async function openHeaterConfig(deviceId) {
      document.getElementById('heater-device-id').value = deviceId;
      document.getElementById('heater-loading').style.display = 'block';
      document.getElementById('heater-config-form').style.display = 'none';
      // Reset save button state
      const btn = document.getElementById('heater-submit-btn');
      if (btn) { btn.disabled = false; btn.textContent = 'Save'; }
      document.getElementById('heater-modal').classList.add('is-active');

      try {
        // Load rooms list
        const roomsRes = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'room.list' })
        });
        if (roomsRes.ok) {
          const roomsData = await roomsRes.json();
          const rooms = roomsData.result || roomsData;
          const roomSelect = document.getElementById('heater-room-id');
          roomSelect.innerHTML = '<option value="">-- Select Room --</option>';
          if (rooms && rooms.rooms) {
            rooms.rooms.forEach(r => {
              const opt = document.createElement('option');
              opt.value = r.id;
              opt.textContent = r.name || r.id;
              roomSelect.appendChild(opt);
            });
          }
        }

        // Load thermometers list
        const thermRes = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'thermometer.list' })
        });
        if (thermRes.ok) {
          const thermData = await thermRes.json();
          const therms = thermData.result || thermData;
          const intSelect = document.getElementById('heater-internal-temp');
          const extSelect = document.getElementById('heater-external-temp');
          intSelect.innerHTML = '<option value="">-- Select Sensor --</option>';
          extSelect.innerHTML = '<option value="">-- Select Sensor --</option>';
          if (therms && therms.thermometers) {
            therms.thermometers.forEach(t => {
              const opt1 = document.createElement('option');
              opt1.value = t.mqtt_topic;
              opt1.textContent = t.name + ' (' + t.type + ')';
              intSelect.appendChild(opt1);
              const opt2 = opt1.cloneNode(true);
              extSelect.appendChild(opt2);
            });
          }
        }

        // Load current heater config via server-side RPC (uses MQTT)
        const configRes = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'heater.getconfig', params: { identifier: deviceId } })
        });
        if (configRes.ok) {
          const data = await configRes.json();
          const result = data.result || data;
          if (result.config) {
            const cfg = result.config;
            document.getElementById('heater-enable-logging').checked = cfg.enable_logging || false;
            document.getElementById('heater-room-id').value = cfg.room_id || '';
            document.getElementById('heater-cheap-start').value = cfg.cheap_start_hour || 23;
            document.getElementById('heater-cheap-end').value = cfg.cheap_end_hour || 7;
            document.getElementById('heater-poll-interval').value = cfg.poll_interval_ms || 60000;
            document.getElementById('heater-preheat-hours').value = cfg.preheat_hours || 2;
            document.getElementById('heater-normally-closed').checked = cfg.normally_closed || false;
            document.getElementById('heater-internal-temp').value = cfg.internal_temperature_topic || '';
            document.getElementById('heater-external-temp').value = cfg.external_temperature_topic || '';
          }
        }

        document.getElementById('heater-loading').style.display = 'none';
        document.getElementById('heater-config-form').style.display = 'block';
      } catch (e) {
        alert('Failed to load heater configuration: ' + e);
        closeHeaterModal();
      }
    }

    function closeHeaterModal() {
      document.getElementById('heater-modal').classList.remove('is-active');
    }

    // Opens the main room creation modal from the heater config dialog
    function showAddRoomModalFromHeater() {
      // Store that we came from heater config so we can refresh the dropdown after
      window.heaterRoomRefreshNeeded = true;
      showAddRoomModal();
    }

    // Refreshes the heater room dropdown and optionally selects a room
    async function refreshHeaterRoomDropdown(selectRoomId) {
      try {
        const roomsRes = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'room.list' })
        });
        if (roomsRes.ok) {
          const roomsData = await roomsRes.json();
          const rooms = roomsData.result || roomsData;
          const roomSelect = document.getElementById('heater-room-id');
          roomSelect.innerHTML = '<option value="">-- Select Room --</option>';
          if (rooms && rooms.rooms) {
            rooms.rooms.forEach(r => {
              const opt = document.createElement('option');
              opt.value = r.id;
              opt.textContent = r.name || r.id;
              roomSelect.appendChild(opt);
            });
          }
          if (selectRoomId) {
            roomSelect.value = selectRoomId;
          }
        }
      } catch (e) {
        console.error('Failed to refresh room dropdown:', e);
      }
    }

    async function submitHeaterConfig() {
      const deviceId = document.getElementById('heater-device-id').value;
      const btn = document.getElementById('heater-submit-btn');
      if (btn) { btn.disabled = true; btn.textContent = 'Saving...'; }

      try {
        // Build config params for heater.setconfig RPC
        const params = { identifier: deviceId };
        
        const enableLogging = document.getElementById('heater-enable-logging');
        if (enableLogging) params.enable_logging = enableLogging.checked;
        
        const roomId = document.getElementById('heater-room-id');
        if (roomId && roomId.value) params.room_id = roomId.value;
        
        const cheapStart = document.getElementById('heater-cheap-start');
        if (cheapStart && cheapStart.value) params.cheap_start_hour = parseInt(cheapStart.value, 10);
        
        const cheapEnd = document.getElementById('heater-cheap-end');
        if (cheapEnd && cheapEnd.value) params.cheap_end_hour = parseInt(cheapEnd.value, 10);
        
        const pollInterval = document.getElementById('heater-poll-interval');
        if (pollInterval && pollInterval.value) params.poll_interval_ms = parseInt(pollInterval.value, 10);
        
        const preheatHours = document.getElementById('heater-preheat-hours');
        if (preheatHours && preheatHours.value) params.preheat_hours = parseInt(preheatHours.value, 10);
        
        const normallyClosed = document.getElementById('heater-normally-closed');
        if (normallyClosed) params.normally_closed = normallyClosed.checked;
        
        const internalTemp = document.getElementById('heater-internal-temp');
        if (internalTemp && internalTemp.value) params.internal_temperature_topic = internalTemp.value;
        
        const externalTemp = document.getElementById('heater-external-temp');
        if (externalTemp && externalTemp.value) params.external_temperature_topic = externalTemp.value;

        // Save via server-side RPC (uses MQTT)
        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'heater.setconfig', params: params })
        });
        
        if (!res.ok) {
          const t = await res.text();
          throw new Error('Failed to save configuration: ' + t);
        }
        
        const data = await res.json();
        const result = data.result || data;
        if (result.success === false) {
          throw new Error(result.message || 'Unknown error');
        }

        if (btn) { btn.textContent = 'Saved!'; }
        setTimeout(() => closeHeaterModal(), 500);
      } catch (e) {
        if (btn) { btn.disabled = false; btn.textContent = 'Save'; }
        alert('Error saving configuration: ' + e);
      }
    }

    // Room management functions
    async function loadRooms() {
      try {
        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'temperature.list' })
        });
        if (!res.ok) {
          document.getElementById('rooms-list').innerHTML = '<div class="column is-12"><p class="has-text-danger">Failed to load rooms</p></div>';
          return;
        }
        const data = await res.json();
        const container = document.getElementById('rooms-list');
        
        // RPC response is wrapped in {"result": ...}
        const rooms = data.result || data;
        
        if (!rooms || Object.keys(rooms).length === 0) {
          container.innerHTML = '<div class="column is-12"><p class="has-text-grey">No rooms configured yet.</p></div>';
          return;
        }

        let html = '';
        for (const [roomId, room] of Object.entries(rooms)) {
          const kinds = room.kinds ? room.kinds.join(', ') : 'other';
          const levels = room.levels ? Object.entries(room.levels).map(([k,v]) => k + ': ' + v + '°C').join(', ') : '';
          html += '<div class="column is-4">' +
            '<div class="box">' +
            '<div class="level mb-2">' +
            '<div class="level-left"><strong>' + (room.name || roomId) + '</strong></div>' +
            '<div class="level-right">' +
            '<button class="button is-small is-info is-outlined mr-1" onclick="editRoom(\'' + roomId + '\')" title="Edit">✏️</button>' +
            '<button class="button is-small is-danger is-outlined" onclick="confirmDeleteRoom(\'' + roomId + '\', \'' + (room.name || roomId) + '\')" title="Delete">🗑️</button>' +
            '</div></div>' +
            '<p class="is-size-7 has-text-grey">ID: ' + roomId + '</p>' +
            '<p class="is-size-7">Types: ' + kinds + '</p>' +
            '<p class="is-size-7">Levels: ' + levels + '</p>' +
            '</div></div>';
        }
        container.innerHTML = html;
      } catch (e) {
        document.getElementById('rooms-list').innerHTML = '<div class="column is-12"><p class="has-text-danger">Error: ' + e + '</p></div>';
      }
    }

    function showAddRoomModal() {
      document.getElementById('room-edit-mode').value = 'create';
      document.getElementById('room-edit-title').textContent = 'Add Room';
      document.getElementById('room-edit-id').value = '';
      document.getElementById('room-edit-slug').value = '';
      document.getElementById('room-edit-slug').disabled = false;
      document.getElementById('room-edit-name').value = '';
      document.querySelectorAll('[id^="room-kind-"]').forEach(cb => cb.checked = false);
      document.getElementById('room-kind-other').checked = true;
      document.getElementById('room-level-eco').value = '18';
      document.getElementById('room-level-comfort').value = '20';
      document.getElementById('room-level-away').value = '15';
      document.getElementById('room-delete-btn').style.display = 'none';
      // Reset save button state
      const btn = document.getElementById('room-edit-submit');
      btn.disabled = false;
      btn.textContent = 'Save';
      document.getElementById('room-edit-modal').classList.add('is-active');
    }

    async function editRoom(roomId) {
      try {
        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'temperature.get', params: { room_id: roomId } })
        });
        if (!res.ok) { alert('Failed to load room'); return; }
        const data = await res.json();
        const room = data.result || data;

        document.getElementById('room-edit-mode').value = 'edit';
        document.getElementById('room-edit-title').textContent = 'Edit Room';
        document.getElementById('room-edit-id').value = roomId;
        document.getElementById('room-edit-slug').value = roomId;
        document.getElementById('room-edit-slug').disabled = true;
        document.getElementById('room-edit-name').value = room.name || '';

        document.querySelectorAll('[id^="room-kind-"]').forEach(cb => cb.checked = false);
        if (room.kinds) {
          room.kinds.forEach(k => {
            const cb = document.getElementById('room-kind-' + k);
            if (cb) cb.checked = true;
          });
        }

        if (room.levels) {
          document.getElementById('room-level-eco').value = room.levels.eco || 18;
          document.getElementById('room-level-comfort').value = room.levels.comfort || 20;
          document.getElementById('room-level-away').value = room.levels.away || 15;
        }

        document.getElementById('room-delete-btn').style.display = 'block';
        // Reset save button state
        const btn = document.getElementById('room-edit-submit');
        btn.disabled = false;
        btn.textContent = 'Save';
        document.getElementById('room-edit-modal').classList.add('is-active');
      } catch (e) {
        alert('Error loading room: ' + e);
      }
    }

    function closeRoomEditModal() {
      document.getElementById('room-edit-modal').classList.remove('is-active');
    }

    async function submitRoomEdit() {
      const mode = document.getElementById('room-edit-mode').value;
      const btn = document.getElementById('room-edit-submit');
      btn.disabled = true;
      btn.textContent = 'Saving...';

      try {
        const roomId = mode === 'create' ? document.getElementById('room-edit-slug').value.trim() : document.getElementById('room-edit-id').value;
        const name = document.getElementById('room-edit-name').value.trim();

        const kinds = [];
        document.querySelectorAll('[id^="room-kind-"]:checked').forEach(cb => kinds.push(cb.value));
        if (kinds.length === 0) kinds.push('other');

        const levels = {
          eco: parseFloat(document.getElementById('room-level-eco').value) || 18,
          comfort: parseFloat(document.getElementById('room-level-comfort').value) || 20,
          away: parseFloat(document.getElementById('room-level-away').value) || 15
        };

        if (!roomId) { alert('Room ID is required'); btn.disabled = false; btn.textContent = 'Save'; return; }

        const method = mode === 'create' ? 'room.create' : 'room.edit';
        const params = mode === 'create' 
          ? { id: roomId, name: name || roomId }
          : { id: roomId, name: name || roomId, kinds: kinds, levels: levels };

        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: method, params: params })
        });

        if (!res.ok) {
          const t = await res.text();
          alert('Failed to save room: ' + t);
          btn.disabled = false;
          btn.textContent = 'Save';
          return;
        }

        // If creating, also set the full config via temperature.set
        if (mode === 'create') {
          await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'temperature.set', params: { room_id: roomId, name: name || roomId, kinds: kinds, levels: levels } })
          });
        }

        btn.textContent = 'Saved!';
        setTimeout(async () => {
          closeRoomEditModal();
          loadRooms();
          // If we came from heater config, refresh the room dropdown and select the new room
          if (window.heaterRoomRefreshNeeded && mode === 'create') {
            window.heaterRoomRefreshNeeded = false;
            await refreshHeaterRoomDropdown(roomId);
          }
        }, 300);
      } catch (e) {
        alert('Error: ' + e);
        btn.disabled = false;
        btn.textContent = 'Save';
      }
    }

    function confirmDeleteRoom(roomId, roomName) {
      if (confirm('Delete room "' + roomName + '"?\n\nNote: If this room is assigned to heaters, they will need to be reconfigured.')) {
        deleteRoomById(roomId);
      }
    }

    async function deleteRoom() {
      const roomId = document.getElementById('room-edit-id').value;
      if (!roomId) return;
      if (!confirm('Delete this room?\n\nNote: If this room is assigned to heaters, they will need to be reconfigured.')) return;
      await deleteRoomById(roomId);
      closeRoomEditModal();
    }

    async function deleteRoomById(roomId) {
      try {
        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'room.delete', params: { id: roomId } })
        });
        if (!res.ok) {
          const t = await res.text();
          alert('Failed to delete room: ' + t);
          return;
        }
        loadRooms();
      } catch (e) {
        alert('Error deleting room: ' + e);
      }
    }

    // Load rooms on page load
    document.addEventListener('DOMContentLoaded', loadRooms);
  </script>
</body>
</html>`))

// hasHeaterScript checks if a device has the heater.js script installed
func hasHeaterScript(d *myhome.Device) bool {
	if d.Config == nil {
		return false
	}
	for _, s := range d.Config.Scripts {
		if s.Name == "heater.js" {
			return true
		}
	}
	return false
}

// getDeviceTypeEmoji returns an emoji indicating the device type based on capabilities
func getDeviceTypeEmoji(d *myhome.Device) string {
	// Check for Gen1 H&T devices by ID pattern
	if strings.HasPrefix(d.Id(), "shellyht-") {
		return "🌡️"
	}

	// Check BLU device capabilities
	if d.Info == nil || d.Info.BTHome == nil {
		return ""
	}

	caps := d.Info.BTHome.Capabilities
	if len(caps) == 0 {
		return ""
	}

	// Check for specific capabilities and return appropriate emoji
	hasMotion := false
	hasTemperature := false
	hasButton := false
	hasWindow := false

	for _, cap := range caps {
		switch cap {
		case "motion":
			hasMotion = true
		case "temperature":
			hasTemperature = true
		case "button":
			hasButton = true
		case "window":
			hasWindow = true
		}
	}

	// Priority: motion > window > button > temperature
	if hasMotion {
		return "🚶"
	}
	if hasWindow {
		return "🪟"
	}
	if hasButton {
		return "🔘"
	}
	if hasTemperature {
		return "🌡️"
	}

	return ""
}

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
				Name:            name,
				Id:              d.Id(),
				Manufacturer:    d.Manufacturer(),
				Host:            host,
				LinkToken:       token,
				HasHeaterScript: hasHeaterScript(d),
				DeviceTypeEmoji: getDeviceTypeEmoji(d),
			})
		}
		sort.Slice(data.Devices, func(i, j int) bool {
			return strings.ToLower(data.Devices[i].Name) < strings.ToLower(data.Devices[j].Name)
		})
	}
	return indexTmpl.Execute(w, data)
}
