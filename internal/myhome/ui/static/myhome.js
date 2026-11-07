// Shell state shared with htmx trigger filters (kiosk.visible) and fragment
// freshness bookkeeping. The Alpine component below owns navigation.
var kiosk = {
  sections: ['home', 'rooms', 'pool', 'vacuum', 'events'],
  section: 'home',
  staleMs: 60000,

  // Daemon liveness (682 §5 rule). online is also read by htmx trigger filters.
  // offlineMs: longest silence tolerated on a stream that still looks open.
  online: true,
  offlineMs: 40000,
  lastSeenMono: 0,   // performance.now() at the last SSE frame
  lastFrameWall: 0,  // browser clock (ms) at the last SSE frame: fallback for staleSince
  lastPingTs: 0,     // the daemon's clock (s) from the last ping
  started: 0,        // daemon start time (s) from the first ping of this page load
  version: '',       // daemon version from the first ping of this page load
  staleText: '',

  // Any SSE frame proves the daemon is reachable. ping is the parsed ping
  // payload, or null for other events. Returns what the caller must do:
  // recovered (was offline) and reload (the daemon was upgraded meanwhile).
  frame: function(nowMono, nowWall, ping) {
    var recovered = !this.online;
    this.online = true;
    this.staleText = '';
    this.lastSeenMono = nowMono;
    this.lastFrameWall = nowWall;
    var reload = false;
    if (ping) {
      this.lastPingTs = Number(ping.ts) || this.lastPingTs;
      if (!this.version && !this.started) {
        this.started = ping.started || 0;
        this.version = ping.version || '';
      } else if (ping.version && this.version && ping.version !== this.version) {
        reload = true;
      }
    }
    return { recovered: recovered, reload: reload };
  },

  // The stream closed or errored: offline. Returns true when this is a change.
  lost: function(now) {
    if (!this.online) { return false; }
    this.online = false;
    this.staleText = this.staleLabel(now);
    return true;
  },

  // Silence watchdog, called every few seconds. Returns true when it flipped.
  tick: function(nowMono, now) {
    if (this.online && nowMono - this.lastSeenMono > this.offlineMs) { return this.lost(now); }
    return false;
  },

  // "stale since 14:32" from the daemon's clock (a Pi without an RTC may have a
  // wrong one); the browser time of the last frame until a ping has been seen.
  staleLabel: function(now) {
    var d = new Date(this.lastPingTs ? this.lastPingTs * 1000 : this.lastFrameWall);
    var hm = String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
    if (d.toDateString() !== now.toDateString()) {
      hm = d.toLocaleDateString([], { day: 'numeric', month: 'short' }) + ' ' + hm;
    }
    return 'stale since ' + hm;
  },

  // Back online: re-fetch what is on screen, flag the rest for when it is shown.
  recovered: function() {
    for (var i = 0; i < this.sections.length; i++) {
      if (this.visible(this.sections[i])) { this.refresh(this.sections[i]); } else { this.dirty(this.sections[i]); }
    }
  },

  visible: function(name) { return this.section === name; },

  fromHash: function() {
    var h = (location.hash || '').replace('#', '');
    return this.sections.indexOf(h) >= 0 ? h : 'home';
  },

  el: function(name) { return document.getElementById('sec-' + name); },

  // Any swap inside a section records when its data was fetched and clears the error pill.
  fetched: function(target) {
    var sec = target && target.closest ? target.closest('[data-section]') : null;
    if (!sec) { return; }
    sec.dataset.fetchedAt = String(Date.now());
    delete sec.dataset.dirty;
    if (sec.id === 'sec-rooms') {
      var add = document.getElementById('add-room-btn');
      if (add) { add.hidden = !!sec.querySelector('[data-rooms-disabled]'); }
    }
    var pill = sec.querySelector('[data-pill]');
    if (pill) { pill.hidden = true; }
  },

  // A failed refresh keeps the previous DOM (htmx does not swap on error).
  failed: function(target) {
    var sec = target && target.closest ? target.closest('[data-section]') : null;
    var pill = sec && sec.querySelector('[data-pill]');
    if (!pill) { return; }
    var d = new Date();
    pill.textContent = "couldn't refresh, " + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
    pill.hidden = false;
  },

  // Invalidation of a section that may be hidden.
  dirty: function(name) {
    var sec = this.el(name);
    if (sec && !this.visible(name)) { sec.dataset.dirty = '1'; }
  },

  // Showing a section refreshes it when dirty or older than staleMs.
  showing: function(name) {
    var sec = this.el(name);
    if (!sec) { return; }
    var age = Date.now() - Number(sec.dataset.fetchedAt || 0);
    if (sec.dataset.dirty || age > this.staleMs) { this.refresh(name); }
  },

  refresh: function(name) {
    var sec = this.el(name);
    if (sec) { sec.querySelectorAll('[data-refresh]').forEach(function(n) { htmx.trigger(n, 'refresh'); }); }
  },

  // Live event rows: ev.html is the server-rendered <tr>. When the table is not
  // in the DOM yet (or the payload has no row) the Events section is marked dirty.
  prependEvent: function(ev) {
    var tbody = document.getElementById('events-tbody');
    if (!tbody || !ev.html) { this.dirty('events'); return; }
    tbody.insertAdjacentHTML('afterbegin', ev.html);
  }
};

  function app() {
    return {
      section: 'home',
      online: true,
      staleLabel: '',
      sensors: {},
      doors: {},
      switches: {},
      rooms: [],
      modals: {
        room: false,
        setup: false,
        heater: false,
        status: false
      },
      roomEditMode: 'create',
      roomForm: {
        id: '',
        name: '',
        kinds: ['other'],
        levels: { eco: 18, comfort: 20, away: 15 },
        icalUrl: '',
        saving: false
      },
      setupForm: {
        deviceId: '',
        name: '',
        roomId: '',
        staEssid: '',
        staPasswd: '',
        sta1Essid: '',
        sta1Passwd: '',
        apPasswd: '',
        saving: false
      },
      heaterForm: {
        deviceId: ''
      },

      init() {
        this.section = kiosk.fromHash();
        kiosk.section = this.section;
        kiosk.lastSeenMono = performance.now();
        kiosk.lastFrameWall = Date.now();
        this.connectSSE();
        setInterval(() => { if (kiosk.tick(performance.now(), new Date())) { this.syncLive(); } }, 5000);
        this.loadRooms();
        document.addEventListener('contextmenu', (e) => e.preventDefault());
        // Offline: nothing may write. GET refreshes are harmless and stay.
        document.body.addEventListener('htmx:beforeRequest', (e) => {
          const verb = e.detail.requestConfig && e.detail.requestConfig.verb;
          if (!kiosk.online && verb && verb !== 'get') { e.preventDefault(); }
        });
        document.body.addEventListener('htmx:afterSwap', (e) => kiosk.fetched(e.detail.target));
        document.body.addEventListener('htmx:responseError', (e) => kiosk.failed(e.detail.target));
        document.body.addEventListener('htmx:sendError', (e) => kiosk.failed(e.detail.target));
      },

      // Rail tap: close any overlay, show the section at its root, and keep the
      // hash current without growing the history. Tapping the active item is
      // the same "go to top".
      go(name) {
        this.modals.room = this.modals.setup = this.modals.heater = this.modals.status = false;
        this.section = name;
        kiosk.section = name;
        history.replaceState(null, '', '#' + name);
        const main = document.getElementById('mh-content');
        if (main) { main.scrollTop = 0; }
        kiosk.showing(name);
      },

      async toggleSwitch(deviceId, switchId) {
        const btn = document.getElementById('btn-switch-' + deviceId + '-' + switchId);
        const shape = document.getElementById('btn-switch-' + deviceId + '-' + switchId + '-shape');
        
        if (btn) {
          btn.disabled = true;
          btn.classList.add('is-loading');
        }
        
        try {
          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ 
              method: 'switch.toggle', 
              params: {
                identifier: deviceId,
                switch_id: parseInt(switchId)
              }
            })
          });
          
          if (!res.ok) {
            console.error('toggleSwitch: error', await res.text());
            return;
          }
          
          const out = await res.json();
          console.log('toggleSwitch: response', out);
          const newState = out.on;
          
          if (btn) {
            btn.disabled = false;
            btn.classList.remove('is-loading');
            
            if (newState) {
              btn.classList.remove('is-light');
              btn.classList.add('is-info', 'is-active');
              shape.innerHTML = `
                <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
                <circle cx="16" cy="12" r="3"></circle>
              `;
            } else {
              btn.classList.remove('is-info', 'is-active');
              btn.classList.add('is-light');
              shape.innerHTML = `
                <rect x="1" y="5" width="22" height="14" rx="7" ry="7"></rect>
                <circle cx="8" cy="12" r="3"></circle>
              `;
            }
          }
        } catch (e) {
          console.error('toggleSwitch: error', e);
          if (btn) {
            btn.disabled = false;
            btn.classList.remove('is-loading');
          }
        }
      },

      async refreshDevice(deviceId) {
        const btn = document.getElementById('btn-refresh-' + deviceId);
        if (btn) {
          btn.disabled = true;
          btn.classList.add('is-loading');
        }
        
        try {
          console.log('refreshDevice: POST /rpc device.refresh for', deviceId);
          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'device.refresh', params: deviceId })
          });
          
          console.log('refreshDevice: response status', res.status);
          if (!res.ok) {
            console.error('refreshDevice: error', await res.text());
            return;
          }
          
          console.log('refreshDevice: success');
        } catch (e) {
          console.error('refreshDevice: error', e.name, e.message, e);
        } finally {
          if (btn) {
            btn.disabled = false;
            btn.classList.remove('is-loading');
          }
        }
      },

      // Mirror the kiosk liveness state into Alpine (banner and greyed view).
      syncLive() {
        this.online = kiosk.online;
        this.staleLabel = kiosk.staleText;
      },

      // Every SSE frame lands here first. ping is the parsed ping payload or null.
      seen(ping) {
        const r = kiosk.frame(performance.now(), Date.now(), ping);
        this.syncLive();
        if (r.reload) { location.reload(); return; }
        if (r.recovered) { kiosk.recovered(); }
      },

      connectSSE() {
        const eventSource = new EventSource('/events');
        
        eventSource.addEventListener('connected', () => {
          console.log('SSE: connected');
          this.seen(null);
        });

        eventSource.addEventListener('ping', (e) => {
          try {
            this.seen(JSON.parse(e.data));
          } catch (err) {
            console.error('SSE: Failed to parse ping:', err);
            this.seen(null);
          }
        });

        eventSource.addEventListener('sensor-update', (e) => {
          this.seen(null);
          try {
            const data = JSON.parse(e.data);
            console.log('SSE: sensor-update', data);
            this.updateSensor(data.device_id, data.sensor, data.value);
          } catch (err) {
            console.error('SSE: Failed to parse sensor-update:', err);
          }
        });

        eventSource.addEventListener('device-update', (e) => {
          this.seen(null);
          try {
            const deviceView = JSON.parse(e.data);
            console.log('SSE: device-update', deviceView);
            
            // Trigger HTMX to reload the specific device card from server
            const deviceCard = document.querySelector(`[id="device-${deviceView.id}"]`);
            if (deviceCard) {
              const parentColumn = deviceCard.closest('.column');
              if (parentColumn) {
                htmx.ajax('GET', `/htmx/device/${deviceView.id}`, {
                  target: parentColumn,
                  swap: 'outerHTML'
                });
              }
            }
          } catch (err) {
            console.error('SSE: Failed to parse device-update:', err);
          }
        });

        eventSource.addEventListener('eventlog', (e) => {
          this.seen(null);
          try {
            kiosk.prependEvent(JSON.parse(e.data));
          } catch (err) {
            console.error('SSE: Failed to parse eventlog:', err);
          }
        });

        eventSource.onerror = (e) => {
          console.error('SSE: error:', e);
          // EventSource retries by itself (retry: 3000); until a frame arrives we are offline.
          if (eventSource.readyState !== 1 && kiosk.lost(new Date())) { this.syncLive(); }
        };
      },

      updateSensor(deviceId, sensor, value) {
        if (!this.sensors[deviceId]) {
          this.sensors[deviceId] = {};
        }
        
        if (sensor === 'temperature') {
          this.sensors[deviceId].temperature = value + '°C';
          // Update DOM element directly
          const tempEl = document.getElementById('sensor-' + deviceId + '-temperature');
          if (tempEl) {
            tempEl.textContent = parseFloat(value).toFixed(1) + '°C';
            tempEl.classList.remove('is-light');
            tempEl.classList.add('is-info');
          }
        } else if (sensor === 'humidity') {
          this.sensors[deviceId].humidity = value + '%';
          // Update DOM element directly
          const humEl = document.getElementById('sensor-' + deviceId + '-humidity');
          if (humEl) {
            humEl.textContent = parseFloat(value).toFixed(1) + '%';
            humEl.classList.remove('is-light');
            humEl.classList.add('is-info');
          }
        } else if (sensor === 'window') {
          const isOpen = value === "1";
          this.doors[deviceId] = isOpen ? 'open' : 'closed';
          // Update DOM element directly
          const doorEl = document.getElementById('door-' + deviceId);
          if (doorEl) {
            if (isOpen) {
              doorEl.className = 'tag is-warning ml-2';
              doorEl.innerHTML = `
                <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                  <path d="M3 21V3h12l6 6v12H3zm2-2h14V9.828L14.172 5H5v14zm9-6h2v2h-2v-2z"/>
                </svg>
                Open
              `;
            } else {
              doorEl.className = 'tag is-success ml-2';
              doorEl.innerHTML = `
                <svg class="door-icon" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
                  <path d="M3 21V3h12v18H3zm2-2h8V5H5v14zm9-6h2v2h-2v-2z"/>
                </svg>
                Closed
              `;
            }
          } else {
            // Door element doesn't exist yet - trigger HTMX card refresh
            console.log('SSE: door element not found, triggering card refresh for', deviceId);
            const deviceCard = document.querySelector(`[id="device-${deviceId}"]`);
            if (deviceCard) {
              const parentColumn = deviceCard.closest('.column');
              if (parentColumn) {
                htmx.ajax('GET', `/htmx/device/${deviceId}`, {
                  target: parentColumn,
                  swap: 'outerHTML'
                });
              }
            }
          }
        }
      },

      async loadRooms() {
        try {
          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'room.list', params: null })
          });
          if (res.ok) {
            const data = await res.json();
            if (data.result && data.result.rooms) {
              this.rooms = data.result.rooms;
            }
          }
        } catch (e) {
          console.error('Failed to load rooms:', e);
        }
      },

      showAddRoomModal() {
        this.roomEditMode = 'create';
        this.roomForm = {
          id: '',
          name: '',
          kinds: ['other'],
          levels: { eco: 18, comfort: 20, away: 15 },
          icalUrl: '',
          saving: false
        };
        this.modals.room = true;
      },

      async editRoom({roomId}) {
        try {
          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'temperature.get', params: { room_id: roomId } })
          });
          if (!res.ok) {
            alert('Failed to load room');
            return;
          }
          const data = await res.json();
          const room = data.result || data;

          this.roomEditMode = 'edit';
          this.roomForm = {
            id: roomId,
            name: room.name || '',
            kinds: room.kinds || ['other'],
            levels: room.levels || { eco: 18, comfort: 20, away: 15 },
            icalUrl: room.ical_url || '',
            saving: false
          };
          this.modals.room = true;
        } catch (e) {
          alert('Error loading room: ' + e);
        }
      },

      closeRoomModal() {
        this.modals.room = false;
      },

      async submitRoomEdit() {
        this.roomForm.saving = true;
        try {
          const method = this.roomEditMode === 'create' ? 'room.create' : 'room.edit';
          const params = {
            id: this.roomForm.id,
            name: this.roomForm.name || this.roomForm.id,
            kinds: this.roomForm.kinds,
            levels: this.roomForm.levels,
            ical_url: this.roomForm.icalUrl
          };

          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method, params })
          });

          if (!res.ok) {
            const t = await res.text();
            alert('Failed to save room: ' + t);
            return;
          }

          if (this.roomEditMode === 'create') {
            await fetch('/rpc', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                method: 'temperature.set',
                params: {
                  room_id: this.roomForm.id,
                  name: this.roomForm.name || this.roomForm.id,
                  kinds: this.roomForm.kinds,
                  levels: this.roomForm.levels
                }
              })
            });
            if (this.roomForm.icalUrl) {
              await fetch('/rpc', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                  method: 'room.edit',
                  params: { id: this.roomForm.id, ical_url: this.roomForm.icalUrl }
                })
              });
            }
          }

          this.closeRoomModal();
          this.loadRooms();
          document.body.dispatchEvent(new CustomEvent('roomsUpdated'));
        } catch (e) {
          alert('Error: ' + e);
        } finally {
          this.roomForm.saving = false;
        }
      },

      async deleteRoom() {
        if (!confirm('Delete this room?\n\nNote: If this room is assigned to heaters, they will need to be reconfigured.')) {
          return;
        }
        try {
          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'room.delete', params: { id: this.roomForm.id } })
          });
          if (!res.ok) {
            const t = await res.text();
            alert('Failed to delete room: ' + t);
            return;
          }
          this.closeRoomModal();
          this.loadRooms();
          document.body.dispatchEvent(new CustomEvent('roomsUpdated'));
        } catch (e) {
          alert('Error deleting room: ' + e);
        }
      },

      confirmDeleteRoom({roomId, roomName}) {
        if (confirm(`Delete room "${roomName}"?\n\nNote: If this room is assigned to heaters, they will need to be reconfigured.`)) {
          this.deleteRoomById(roomId);
        }
      },

      async deleteRoomById(roomId) {
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
          this.loadRooms();
          document.body.dispatchEvent(new CustomEvent('roomsUpdated'));
        } catch (e) {
          alert('Error deleting room: ' + e);
        }
      },

      async openSetupModal({deviceId}) {
        this.setupForm = {
          deviceId,
          name: '',
          roomId: '',
          staEssid: '',
          staPasswd: '',
          sta1Essid: '',
          sta1Passwd: '',
          apPasswd: '',
          saving: false
        };
        
        await this.loadRooms();
        
        try {
          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'device.show', params: { identifier: deviceId } })
          });
          if (res.ok) {
            const data = await res.json();
            if (data.result) {
              this.setupForm.roomId = data.result.room_id || '';
              this.setupForm.name = data.result.name || '';
            }
          }
        } catch (e) {
          console.log('Could not fetch device info:', e);
        }
        
        this.modals.setup = true;
      },

      closeSetupModal() {
        this.modals.setup = false;
      },

      async submitSetup() {
        this.setupForm.saving = true;
        try {
          const params = { identifier: this.setupForm.deviceId };
          
          if (this.setupForm.name) params.name = this.setupForm.name;
          if (this.setupForm.staEssid) params.sta_essid = this.setupForm.staEssid;
          if (this.setupForm.staPasswd) params.sta_passwd = this.setupForm.staPasswd;
          if (this.setupForm.sta1Essid) params.sta1_essid = this.setupForm.sta1Essid;
          if (this.setupForm.sta1Passwd) params.sta1_passwd = this.setupForm.sta1Passwd;
          if (this.setupForm.apPasswd) params.ap_passwd = this.setupForm.apPasswd;

          if (this.setupForm.roomId !== undefined) {
            await fetch('/rpc', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ 
                method: 'device.setroom', 
                params: { identifier: this.setupForm.deviceId, room_id: this.setupForm.roomId } 
              })
            });
          }

          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'device.setup', params })
          });
          
          if (!res.ok) {
            const t = await res.text();
            alert('Setup failed: ' + t);
            return;
          }
          
          this.closeSetupModal();
          setTimeout(() => kiosk.refresh('home'), 500);
        } catch (e) {
          alert('Setup error: ' + e);
        } finally {
          this.setupForm.saving = false;
        }
      },

      openHeaterModal({deviceId}) {
        this.heaterForm.deviceId = deviceId;
        this.modals.heater = true;
      },

      closeHeaterModal() {
        this.modals.heater = false;
      }
    };
  }
