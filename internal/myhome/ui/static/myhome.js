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
  started: 0,        // daemon start time (s) from the first ping of this page load; a change means a restart
  version: '',       // daemon version this page was rendered with (body data-version)
  staleText: '',
  es: null,          // the live EventSource (kept here, not in Alpine's reactive proxy)

  // Reopening a dead stream: EventSource gives up for good (readyState CLOSED)
  // after a non-200 answer, and a silent partition leaves a half-open socket
  // that never errors. So while offline the shell closes and re-creates the
  // stream itself, backing off from reopenMinMs to reopenMaxMs.
  reopenMinMs: 3000,
  reopenMaxMs: 30000,
  reopenDelay: 3000,
  nextReopenMono: 0,

  // Any SSE frame proves the daemon is reachable. ping is the parsed ping
  // payload, or null for other events. pageVersion is the version the page was
  // rendered with (the baseline, so an upgrade while offline is seen on the
  // first ping). Returns the single action the caller must take:
  // 'reload' (daemon upgraded), 'refresh-all' (daemon restarted), 'recover'
  // (was offline, same daemon) or '' (nothing).
  frame: function(nowMono, nowWall, ping) {
    var wasOffline = !this.online;
    this.online = true;
    this.staleText = '';
    this.lastSeenMono = nowMono;
    this.lastFrameWall = nowWall;
    this.reopenDelay = this.reopenMinMs;
    var upgraded = false, restarted = false;
    if (ping) {
      this.lastPingTs = Number(ping.ts) || this.lastPingTs;
      if (ping.version && this.version && ping.version !== this.version) { upgraded = true; }
      if (!this.version && ping.version) { this.version = ping.version; }
      if (ping.started) {
        if (this.started && ping.started !== this.started) { restarted = true; }
        this.started = ping.started;
      }
    }
    if (upgraded) { return 'reload'; }
    if (restarted) { return 'refresh-all'; }
    return wasOffline ? 'recover' : '';
  },

  // The stream errored. readyState: 0 CONNECTING (the browser retries by
  // itself), 2 CLOSED (it will not). Returns { changed, reopen }: changed when
  // the banner state flipped, reopen when the caller must re-create the stream.
  streamError: function(readyState, nowMono, now) {
    var changed = false;
    if (readyState !== 1) { changed = this.lost(now); }
    var reopen = readyState === 2;
    if (reopen) { this.scheduleReopen(nowMono); }
    return { changed: changed, reopen: reopen };
  },

  scheduleReopen: function(nowMono) {
    this.nextReopenMono = nowMono + this.reopenDelay;
    this.reopenDelay = Math.min(this.reopenDelay * 2, this.reopenMaxMs);
  },

  // The stream closed or errored: offline. Returns true when this is a change.
  lost: function(now) {
    if (!this.online) { return false; }
    this.online = false;
    this.staleText = this.staleLabel(now);
    return true;
  },

  // Watchdog, called every second. A silent stream flips to offline and is
  // reopened; while offline it is reopened on a backoff. Also recomputes the
  // label so "stale since" gains its date prefix across midnight. Returns
  // { changed, reopen }.
  tick: function(nowMono, now) {
    var changed = false, reopen = false;
    if (this.online) {
      if (nowMono - this.lastSeenMono > this.offlineMs) {
        changed = this.lost(now);
        reopen = true;
        this.reopenDelay = this.reopenMinMs;
        this.scheduleReopen(nowMono);
      }
    } else {
      var text = this.staleLabel(now);
      if (text !== this.staleText) { this.staleText = text; changed = true; }
      if (nowMono >= this.nextReopenMono) { reopen = true; this.scheduleReopen(nowMono); }
    }
    return { changed: changed, reopen: reopen };
  },

  // htmx writes are refused while offline; GET refreshes stay.
  blocks: function(verb) {
    return !this.online && !!verb && String(verb).toLowerCase() !== 'get';
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
  recovered: function(all) {
    for (var i = 0; i < this.sections.length; i++) {
      if (all || this.visible(this.sections[i])) { this.refresh(this.sections[i]); } else { this.dirty(this.sections[i]); }
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
    // A failed state-button POST says which switch, not "refresh".
    var verb = "couldn't refresh";
    if (target.classList && target.classList.contains('mh-power')) {
      verb = "couldn't toggle " + String(target.getAttribute('aria-label') || '').replace(/^Toggle /, '');
    }
    pill.textContent = verb + ", " + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
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

  // Re-render one device (all its rows) from the server. A GET, so it is
  // allowed while offline; the previous DOM stays if it fails.
  reloadDevice: function(id) {
    var el = document.getElementById('device-' + id);
    if (el) { htmx.ajax('GET', '/htmx/device/' + encodeURIComponent(id), { target: el, swap: 'outerHTML' }); }
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
        status: false,
        device: false
      },
      deviceSheet: { id: '', name: '', manufacturer: '', refreshable: false, heater: false, busy: false, error: '' },
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
        kiosk.version = document.body.dataset.version || '';
        kiosk.lastSeenMono = performance.now();
        kiosk.lastFrameWall = Date.now();
        this.connectSSE();
        setInterval(() => {
          const r = kiosk.tick(performance.now(), new Date());
          if (r.changed) { this.syncLive(); }
          if (r.reopen) { this.connectSSE(); }
        }, 1000);
        this.loadRooms();
        document.addEventListener('contextmenu', (e) => e.preventDefault());
        // Offline: nothing may write. GET refreshes are harmless and stay.
        document.body.addEventListener('htmx:beforeRequest', (e) => {
          if (kiosk.blocks(e.detail.requestConfig && e.detail.requestConfig.verb)) { e.preventDefault(); }
        });
        document.body.addEventListener('htmx:afterSwap', (e) => kiosk.fetched(e.detail.target));
        document.body.addEventListener('htmx:responseError', (e) => kiosk.failed(e.detail.target));
        document.body.addEventListener('htmx:sendError', (e) => kiosk.failed(e.detail.target));
      },

      // Rail tap: close any overlay, show the section at its root, and keep the
      // hash current without growing the history. Tapping the active item is
      // the same "go to top".
      go(name) {
        this.modals.room = this.modals.setup = this.modals.heater = this.modals.status = this.modals.device = false;
        this.section = name;
        kiosk.section = name;
        history.replaceState(null, '', '#' + name);
        const main = document.getElementById('mh-content');
        if (main) { main.scrollTop = 0; }
        kiosk.showing(name);
      },

      // Device sheet: filled from the row's data-* attributes, closed by default.
      openDeviceSheet(d) {
        this.deviceSheet = {
          id: d.deviceId,
          name: d.name,
          manufacturer: d.manufacturer,
          refreshable: !!d.refreshable,
          heater: !!d.heater,
          busy: false,
          error: ''
        };
        this.modals.device = true;
      },

      async refreshDevice(deviceId) {
        if (!kiosk.online || this.deviceSheet.busy) { return; }
        this.deviceSheet.busy = true;
        this.deviceSheet.error = '';
        try {
          const res = await fetch('/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ method: 'device.refresh', params: deviceId })
          });
          if (!res.ok) {
            console.error('refreshDevice: error', await res.text());
            this.deviceSheet.error = "Couldn't refresh " + this.deviceSheet.name;
          }
        } catch (e) {
          console.error('refreshDevice: error', e.name, e.message, e);
          this.deviceSheet.error = "Couldn't refresh " + this.deviceSheet.name;
        } finally {
          this.deviceSheet.busy = false;
        }
      },

      // Mirror the kiosk liveness state into Alpine (banner and greyed view).
      syncLive() {
        this.online = kiosk.online;
        this.staleLabel = kiosk.staleText;
      },

      // Every SSE frame lands here first. ping is the parsed ping payload or null.
      seen(ping) {
        const action = kiosk.frame(performance.now(), Date.now(), ping);
        this.syncLive();
        if (action === 'reload') { location.reload(); return; }
        if (action === 'refresh-all') { kiosk.recovered(true); }
        if (action === 'recover') { kiosk.recovered(false); }
      },

      connectSSE() {
        if (kiosk.es) { kiosk.es.close(); }
        const eventSource = new EventSource('/events');
        kiosk.es = eventSource;
        
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
            
            kiosk.reloadDevice(deviceView.id);
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
          // Offline until a frame arrives. CONNECTING: the browser retries (retry: 3000).
          // CLOSED: it never will, so the next tick reopens the stream.
          if (eventSource !== kiosk.es) { return; }
          const r = kiosk.streamError(eventSource.readyState, performance.now(), new Date());
          if (r.changed) { this.syncLive(); }
        };
      },

      updateSensor(deviceId, sensor, value) {
        if (!this.sensors[deviceId]) {
          this.sensors[deviceId] = {};
        }

        if (sensor === 'temperature' || sensor === 'humidity') {
          const unit = sensor === 'temperature' ? '°C' : '%';
          this.sensors[deviceId][sensor] = value + unit;
          const el = document.getElementById('sensor-' + deviceId + '-' + sensor);
          if (el) {
            el.textContent = parseFloat(value).toFixed(1) + unit;
            el.classList.remove('is-unknown');
          }
        } else if (sensor === 'window') {
          const isOpen = value === "1";
          this.doors[deviceId] = isOpen ? 'open' : 'closed';
          const doorEl = document.getElementById('door-' + deviceId);
          const text = doorEl && doorEl.querySelector('[data-door-text]');
          if (text) {
            text.textContent = isOpen ? 'Open' : 'Closed';
            doorEl.classList.remove('is-unknown');
            doorEl.classList.toggle('is-alert', isOpen);
          } else {
            // The pill is not on the page yet: re-render the device.
            kiosk.reloadDevice(deviceId);
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
            const roomRes = await fetch('/rpc', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ 
                method: 'device.setroom', 
                params: { identifier: this.setupForm.deviceId, room_id: this.setupForm.roomId } 
              })
            });
            if (!roomRes.ok) {
              alert('Room assignment refused: ' + await roomRes.text());
              return;
            }
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
