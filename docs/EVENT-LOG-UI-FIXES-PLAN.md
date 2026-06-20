# Event Log UI — bug-fix pass

Status: **planned** (no code yet)

## Context

The devices Event Log page (`/event-log`) has three defects, treated here as **bug-fixes**
(minimal, targeted — no architectural churn):

1. **Raw device IDs** shown instead of friendly names. The row template already renders a
   friendly name with the ID as a tooltip, but (a) we want the ID kept as its own right-most
   column, and (b) BLU events never resolve a name because the BLU bridge stores a different
   `device_id` than the one the device is registered under.
2. **`1970-01-01` timestamps** on BLU events. `handleBLUEventBridge` records events without ever
   setting `Ts`, so it defaults to `0` → epoch. (Gen2 does this correctly; BLU was never updated.)
3. **Truncated one-line JSON** in the Data column. There is no JSON pretty-printer anywhere;
   the only transform is a 60-char `truncate`.

Decisions: Data cells are **collapsible** (compact preview, click to expand pretty JSON);
**live SSE rows are enriched too** so they match the server-rendered table.

Intended outcome: each row shows the friendly device name, the real receive time, a collapsible
pretty-printed Data payload, and the raw device ID in the right-most column — both on load and on
live updates.

## Phases

- [ ] **Phase 1** — BLU bridge: timestamp, canonical device id, populated Data
- [ ] **Phase 2** — Events service: `Ts` safety-net default
- [ ] **Phase 3** — Table template: name column, ID right-most, collapsible Data + client JS
- [ ] **Phase 4** — SSE: enrich live event payload with device name
- [ ] **Phase 5** — DRY name-resolution helper + daemon wiring
- [ ] **Phase 6** — Tests + end-to-end verification

## Changes

### 1. BLU bridge — fix timestamp, device id, and empty Data
`internal/myhome/shelly/blu/listener.go` → `handleBLUEventBridge` (lines ~160-250)

- **Canonical device id (fixes name resolution + filtering):** replace `deviceID := data.Address`
  with `deviceID := deviceIDFromCapabilities(data.Address, data)` — the same id the device is
  registered under (`handleBLUEvent` line 270), so `GetDeviceByAny` matches the `id` column and the
  friendly name resolves. Keep `data.Address` only for the Data payload below.
- **Timestamp:** compute once near the top:
  `ts := float64(time.Now().Unix()); if data.Timestamp != nil { ts = float64(*data.Timestamp) }`
  (mirrors gen2 `listener.go:94-99`; the BTHome 0x50 `timestamp` is rarely present, so receipt time
  is the fallback). Set `Ts: ts` on every `events.Event{...}` (motion, window, button, battery).
- **Populate Data** (small enhancement so the Data column is informative and pretty-print has
  something to show for BLU rows): build a compact `*string` JSON per event with the triggering
  value(s) plus `address` and `rssi`, e.g. motion → `{"motion":1,"address":"…","rssi":-72}`.
  Add a tiny local helper that `json.Marshal`s a `map[string]any` and returns `*string`.
- Add `"time"` to the import block.

### 2. Events service — safety net so no event ever shows 1970
`myhome/events/service.go` → `Record` (lines 53-64)

After defaulting `ReceivedAt`, add: `if e.Ts == 0 { e.Ts = e.ReceivedAt }`. Defensive: any source
that forgets `Ts` falls back to receipt time instead of epoch. Cheap, broadly correct.

### 3. Table template — name column, ID right-most, collapsible Data
`internal/myhome/ui/events_template.go`

- **`eventTemplateFuncs`** (lines 9-37): add `eventDataCell(s *string) template.HTML`:
  - nil/empty → `""`.
  - valid JSON (try `json.Indent`) → return
    `<details class="event-data"><summary>{escaped 60-char preview}</summary><pre>{escaped pretty JSON}</pre></details>`
    built from `template.HTMLEscapeString` parts, returned as `template.HTML`.
  - not JSON → escaped raw text in a `<span>`.
  - Keep `truncate` (reused for the preview).
- **`eventRowTemplate`** (lines 41-48): new column order —
  `Time | Device(name, fallback id) | Component | Event | Severity | Data | Device ID`.
  Device cell: `{{if .DeviceName}}{{.DeviceName}}{{else}}{{.DeviceID}}{{end}}`.
  Data cell: `{{eventDataCell .Data}}`. New right-most cell: `<td><code>{{.DeviceID}}</code></td>`.
- **`eventsTableTemplate`** thead (lines 96-105): add `<th>Device ID</th>` as the last column.
- **colspans**: `6 → 7` in the "Load more" rows of both `eventsRowsTemplate` (line 55) and
  `eventsTableTemplate` (line 111).
- **Client JS** in `eventLogPageHTML` (lines 203-219): rebuild the `eventlog` listener to emit the
  same 7 columns — name column (use new `device_name`, fallback `device_id`), collapsible
  `<details><summary>…</summary><pre>…</pre></details>` for data (pretty-print via
  `JSON.stringify(JSON.parse(ev.data), null, 2)` with a try/catch fallback to raw), and the raw
  `device_id` in a right-most `<code>` cell. Build via DOM nodes (current style) to avoid XSS.

### 4. SSE — enrich live event payload with the device name
`internal/myhome/ui/sse.go`

- Add `nameFor func(string) string` field to `SSEBroadcaster` and a setter
  `SetDeviceNameResolver(fn func(string) string)`.
- Change `BroadcastEvent` (line 152) to marshal an enriched anonymous struct
  `{ts, device_id, device_name, component, event, severity, data}` (resolve `device_name` via
  `nameFor` when set) instead of broadcasting the raw `events.Event`. Keep the `"eventlog"` SSE
  event name and all existing fields — only **add** `device_name` (the CLI follow command also
  parses this payload, so the change must be additive).

### 5. Reuse one name-resolution helper (DRY)
`internal/myhome/ui/htmx.go`

Extract the per-id lookup inside `deviceNameMap` (lines 254-267) into a small method
`func (h *HTMXHandler) deviceName(id string) string` (GetDeviceByAny → MAC fallback → name).
`deviceNameMap` calls it in a loop. This same logic backs the SSE resolver.

### 6. Wire the resolver in the daemon
`myhome/daemon/daemon.go` (around line 210, after `storage` at line 202 and the broadcaster)

Call `sseBroadcaster.SetDeviceNameResolver(func(id string) string { … })` using `storage`
(same lookup as `HTMXHandler.deviceName`). Wire it right after the broadcaster is created so live
events resolve names.

## Tests

- `internal/myhome/shelly/blu/listener_test.go`: add a focused test for `handleBLUEventBridge`
  feeding a motion payload through an in-memory `events.Service`, then `Store().Query` and assert:
  `Ts != 0` (≈ now), `DeviceID == deviceIDFromCapabilities(addr, data)`, and `Data != nil` with the
  expected JSON. Existing BLU tests don't touch the bridge, so no breakage.
- `myhome/events/service_test.go`: assert `Record` of an event with `Ts == 0` stores
  `Ts == ReceivedAt` (non-zero).
- Run `make test` (canonical — covers go.work sub-modules).

## Verification (end-to-end)

1. `make build` (runs `go generate` first — required for embedded CSS/JS assets).
2. `make test`.
3. `make run`, open `http://localhost:6080/event-log`.
   - Confirm columns: `Time | Device | Component | Event | Severity | Data | Device ID`.
   - Trigger a BLU sensor (or use the `shelly_call`/MQTT mock) to emit a motion/window event.
     Confirm the new row shows: real time (not 1970), friendly name (or shellyblu… id) in Device,
     raw MAC/ID in the right-most column, and a collapsible Data cell that expands to indented JSON.
   - Confirm a gen2 `script:N shelly-blu` row's Data expands to pretty JSON.
   - Confirm the **live** row (appears without reload, via SSE) has the name column filled and the
     same collapsible Data — i.e. matches a row after page reload.
4. Sanity: the CLI event-follow still parses the `eventlog` SSE payload (only an additive
   `device_name` field).
