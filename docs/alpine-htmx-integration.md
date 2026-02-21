# Alpine.js & HTMX Integration Proposal

## Overview

This document outlines the proposed changes to integrate Alpine.js and HTMX into the MyHome UI while maintaining Bulma for styling.

## Architecture Benefits

### Alpine.js
- **Reactive State Management**: Eliminates manual DOM manipulation
- **Declarative Syntax**: Cleaner, more maintainable code
- **Lightweight**: Only ~15KB minified
- **No Build Step**: Works directly in the browser

### HTMX
- **Declarative Server Interactions**: Replace verbose fetch() calls with HTML attributes
- **Partial HTML Updates**: Server returns HTML fragments instead of JSON
- **Progressive Enhancement**: Works without JavaScript for basic functionality
- **Reduced Client-Side Logic**: Move complexity to server where it's easier to test

### Combined Benefits
- **Less JavaScript**: ~70% reduction in client-side code
- **Better Separation**: Server handles data/logic, client handles presentation
- **Easier Testing**: Server-side HTML generation is easier to test than client-side DOM manipulation
- **Better Performance**: Less client-side processing, smaller payloads

## Files Created

### 1. `/internal/myhome/ui/htmx.go`
New server-side handler for HTMX partial HTML responses.

**Key Features:**
- `DeviceCard()`: Returns single device card HTML fragment
- `RoomsList()`: Returns rooms list HTML fragment  
- `SwitchButton()`: Handles switch toggle and returns updated button HTML

**Endpoints:**
- `GET /htmx/device/{id}` - Get device card HTML
- `GET /htmx/rooms` - Get rooms list HTML
- `POST /htmx/switch/toggle` - Toggle switch and return updated button

### 2. `/internal/myhome/ui/static/index-alpine.html`
Modernized HTML template demonstrating Alpine.js + HTMX integration.

**Key Changes:**
- Alpine.js `x-data` directive for reactive state
- HTMX `hx-*` attributes for server interactions
- Simplified JavaScript (~400 lines vs ~1000 lines in original)
- Reactive sensor updates via Alpine.js state
- Modal management via Alpine.js state
- Event-driven architecture with custom events

## Server Changes

### Modified: `/internal/myhome/ui/server.go`
Added HTMX endpoint registration:
```go
htmxHandler := NewHTMXHandler(ctx, log.WithName("HTMXHandler"), db)
mux.HandleFunc("/htmx/device/", htmxHandler.DeviceCard)
mux.HandleFunc("/htmx/rooms", htmxHandler.RoomsList)
mux.HandleFunc("/htmx/switch/toggle", htmxHandler.SwitchButton)
```

## Migration Strategy

### Phase 1: Parallel Implementation (Current)
- Keep existing `index.html` as-is
- New `index-alpine.html` demonstrates the new approach
- Both versions work side-by-side
- No breaking changes

### Phase 2: Gradual Migration (Recommended)
1. Test `index-alpine.html` thoroughly
2. Migrate one feature at a time from old to new
3. Add more HTMX endpoints as needed
4. Eventually replace `index.html` with `index-alpine.html`

### Phase 3: Full Adoption
- Remove old JavaScript code
- Standardize on Alpine.js + HTMX patterns
- Add more server-side HTML templates
- Simplify client-side codebase

## Code Comparison

### Before (Vanilla JS):
```javascript
async function toggleSwitch(id, switchId) {
  var new_state = false;
  const btn = document.getElementById('btn-switch-' + id + '-' + switchId);
  if (btn) {
    btn.disabled = true;
    btn.classList.add('is-loading');
  }
  const shape = document.getElementById('btn-switch-' + id + '-' + switchId + '-shape');
  try {
    res = await fetch('/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ method: 'switch.toggle', params: {
        identifier: id,
        switch_id: parseInt(switchId)
      }})
    });
    if (!res.ok) {
      console.error('toggleSwitch: switch.toggle response error', await res.text());
      return;
    } else {
      out = await res.json();
      new_state = ! out.result.result.was_on;
    }
  } catch (e) {
    console.error('toggleSwitch: error', e.name, e.message, e);
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.classList.remove('is-loading');
      if (new_state) {
        btn.classList.remove('is-light');
        btn.classList.add('is-info');
        btn.classList.add('is-active');
        shape.innerHTML = `...`;
      } else {
        btn.classList.remove('is-info');
        btn.classList.remove('is-active');
        btn.classList.add('is-light');
        shape.innerHTML = `...`;
      }
    }
  }
}
```

### After (HTMX):
```html
<button class="button is-rounded {{if .On}}is-info is-active{{else}}is-light{{end}}" 
        hx-post="/htmx/switch/toggle"
        hx-vals='{"device_id": "{{.DeviceID}}", "switch_id": "{{.SwitchID}}"}'
        hx-target="this"
        hx-swap="outerHTML"
        title="Toggle switch">
  <!-- SVG content -->
</button>
```

**Server-side (Go):**
```go
func (h *HTMXHandler) SwitchButton(w http.ResponseWriter, r *http.Request) {
    // Parse params, call RPC, render template
    tmpl.Execute(w, data)
}
```

## Alpine.js State Management

### Before (Manual DOM):
```javascript
function updateDeviceSensor(deviceId, sensor, value) {
  const el = document.getElementById('sensor-' + deviceId + '-' + sensor);
  if (el && sensor === 'temperature') {
    el.textContent = value + '°C';
  }
}
```

### After (Alpine.js):
```javascript
// State
sensors: {},

// Update
updateSensor(deviceId, sensor, value) {
  if (!this.sensors[deviceId]) {
    this.sensors[deviceId] = {};
  }
  this.sensors[deviceId].temperature = value + '°C';
}

// Template (auto-updates)
<span x-text="sensors['{{.Id}}']?.temperature || '--°C'"></span>
```

## Modal Management

### Before (Manual Classes):
```javascript
function closeSetupModal() {
  document.getElementById('setup-modal').classList.remove('is-active');
}
```

### After (Alpine.js):
```javascript
// State
modals: { setup: false, room: false, heater: false },

// Methods
closeSetupModal() {
  this.modals.setup = false;
}

// Template
<div class="modal" :class="{'is-active': modals.setup}">
```

## Event-Driven Architecture

### Custom Events with Alpine.js:
```html
<!-- Dispatch event -->
<button @click="$dispatch('open-setup-modal', {deviceId: '{{.Id}}'})">

<!-- Listen for event -->
<body @open-setup-modal.window="openSetupModal($event.detail)">
```

## HTMX Features Used

### Automatic Loading States:
```html
<button hx-post="/htmx/switch/toggle" 
        hx-indicator="#spinner">
  Toggle
  <span id="spinner" class="htmx-indicator">⏳</span>
</button>
```

### Partial Updates:
```html
<div id="rooms-list" 
     hx-get="/htmx/rooms"
     hx-trigger="load, roomsUpdated from:body"
     hx-swap="innerHTML">
```

### Form Submission:
```html
<button hx-post="/rpc"
        hx-vals='{"method": "device.refresh", "params": "{{.Id}}"}'
        hx-trigger="click">
```

## Testing Strategy

### Unit Tests (Server-Side):
- Test HTMX handlers return correct HTML
- Test template rendering with various data
- Test error handling

### Integration Tests:
- Test SSE updates trigger Alpine.js reactivity
- Test HTMX requests update UI correctly
- Test modal interactions

### Manual Testing:
1. Load `index-alpine.html`
2. Verify all device cards render
3. Test switch toggles
4. Test room management
5. Test setup modal
6. Verify SSE updates work
7. Test on mobile devices

## Performance Considerations

### Pros:
- Smaller JavaScript bundle (~15KB Alpine + ~14KB HTMX vs custom code)
- Less client-side processing
- Server-side HTML generation is cached
- Fewer DOM manipulations

### Cons:
- More server requests (mitigated by HTMX caching)
- Slightly larger HTML payloads (mitigated by compression)

### Optimization Tips:
- Use HTMX `hx-boost` for navigation
- Enable HTTP/2 for multiplexing
- Use `hx-trigger="load delay:1s"` for deferred loading
- Cache HTMX responses with `hx-cache="true"`

## Browser Support

- **Alpine.js**: All modern browsers (IE11+ with polyfills)
- **HTMX**: All modern browsers (IE11+ with polyfills)
- **Bulma**: All modern browsers

## Next Steps

1. **Review** this proposal and the demo file
2. **Test** `index-alpine.html` in your environment
3. **Decide** on migration strategy
4. **Implement** additional HTMX endpoints as needed
5. **Migrate** features incrementally
6. **Document** patterns for future development

## Additional Resources

- [Alpine.js Documentation](https://alpinejs.dev/)
- [HTMX Documentation](https://htmx.org/)
- [Bulma Documentation](https://bulma.io/)
- [HTMX Examples](https://htmx.org/examples/)
- [Alpine.js Examples](https://alpinejs.dev/examples)

## Questions to Consider

1. Do you want to keep both versions or migrate fully?
2. Should we add more HTMX endpoints for other features?
3. Do you want to use Alpine.js plugins (e.g., `@alpinejs/persist` for localStorage)?
4. Should we add HTMX extensions (e.g., `sse` for better SSE integration)?
5. Do you want to add TypeScript for better type safety?
