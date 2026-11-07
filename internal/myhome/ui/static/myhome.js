// Shell state shared with htmx trigger filters (kiosk.visible) and fragment
// freshness bookkeeping. The Alpine component below owns navigation.
var kiosk = {
  sections: ['home', 'rooms', 'pool', 'vacuum', 'events'],
  section: 'home',
  staleMs: 60000,

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
        this.connectSSE();
        this.loadRooms();
        document.addEventListener('contextmenu', (e) => e.preventDefault());
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

      connectSSE() {
        const eventSource = new EventSource('/events');
        
        eventSource.addEventListener('connected', () => {
          console.log('SSE: connected');
        });

        eventSource.addEventListener('sensor-update', (e) => {
          try {
            const data = JSON.parse(e.data);
            console.log('SSE: sensor-update', data);
            this.updateSensor(data.device_id, data.sensor, data.value);
          } catch (err) {
            console.error('SSE: Failed to parse sensor-update:', err);
          }
        });

        eventSource.addEventListener('device-update', (e) => {
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
          try {
            kiosk.prependEvent(JSON.parse(e.data));
          } catch (err) {
            console.error('SSE: Failed to parse eventlog:', err);
          }
        });

        eventSource.onerror = (e) => {
          console.error('SSE: error:', e);
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
