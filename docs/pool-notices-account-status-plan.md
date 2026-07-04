# Pool notices + account-status UI — implementation plan

Status doc for an in-progress feature. Delete this file once all phases are checked off and
merged. Update checkboxes as work lands so this can be resumed cold (fresh session / context
loss) by reading this file plus `git log` / `git diff` on this branch.

Branch: `worktree-feature-pool-notices-account-status`
Worktree: `.claude/worktrees/feature-pool-notices-account-status`

## Context

Six related additions to `myhome`:

1. **Account visibility** — surface which external accounts the system talks to and whether
   their last connection succeeded (net-new: no aggregated status exists today).
2. **Pool notices** — notice events for pool water-supply on/off, pump start (reason: schedule /
   solar / manual), pump stop (reason + today's turnover), and the morning scheduler's planned
   start/stop window.
3. **Pool status display** — turnover rate + water-supply status in web UI and `ctl pool status`.

Notices already exist as `Severity:"notice"` events → daily SMTP digest + events DB + UI event
log. Most pool events are already emitted by the device JS. Gaps: `pool.pump_start` drops its
`reason`; achieved turnover is only computable on the daemon (`PoolRuntimeTracker`), not the
device; no account-status registry; no pool rendering in the web UI.

## Assumptions (confirmed by proceeding past plan approval)

- Delivery: reuse existing daily digest + events DB + UI event log (no new real-time channel).
- Turnover shown as achieved-today vs configured target (e.g. "3.2 of 5.0 x/day").
- Accounts shown: Beem Energy, SFR box, SMTP/email, MQTT broker.
- Connection success = live last-attempt result (ok/failed + timestamp), not just "configured".

## Progress

### Phase 0 — Setup
- [x] Worktree created, branch `worktree-feature-pool-notices-account-status`
- [x] `make generate` run successfully in this worktree (required before any `go build`)

### Phase 1 — Account connection status (net-new) — DONE
- [x] New package `internal/myhome/accounts/registry.go`: `Status` struct + `Registry` with
      `Report(name, err)`, `SetEnabled(name, on)`, `Snapshot() []Status` (+ registry_test.go)
- [x] Wire Beem (`pkg/beem/watcher.go` `poll`, new `Watcher.OnResult` hook) to report into registry
- [x] Wire SFR (`internal/myhome/sfr/router.go` `refresh`, new package-level `StatusReporter`/
      `SetStatusReporter`) to report into registry. Added `options.Flags.SFRUsername/SFRPassword`
      (mirrors Beem) so daemon.go can compute `SetEnabled`.
- [x] Wire SMTP via a small `reportingMailer` wrapper defined in `myhome/daemon/daemon.go`
      (decorates `notify.Mailer` without touching the leaf `myhome/notify` package)
- [x] Wire MQTT (`daemon.go` periodic status block + right after `mc.Start()`) to report
- [x] Construct registry once in `daemon.go` (`accountsRegistry := accounts.NewRegistry()`),
      pass to integrations + `ui.Start(...)` (new trailing param) + `NewHTMXHandler(...)`
- [x] UI: `/htmx/accounts` handler `HTMXHandler.AccountsPanel` in `internal/myhome/ui/htmx.go` +
      route in `server.go`
- [x] UI: lazy-load container in `internal/myhome/ui/static/index.html` ("🔌 Accounts" section,
      mirrors the Rooms section pattern)
- [ ] Optional: extend `myhome/metrics/exporter.go` `handleHealth` with per-account block — SKIPPED
      (not essential; UI panel covers the requirement)

### Phase 2 — Pool notice enrichment — DONE
- [x] REVISED FINDING (during implementation): `myhome/ctl/pool/start.go`/`stop.go` already call
      `doStart('...', 'Manual start via ctl pool start <speed>')` / `doStart(..., 'Manual stop
      via ctl pool stop')` via `EvalInDevice` — manual CLI actions already flow through
      `doStart`/`doStop` with a distinguishing reason string. Solar automation never calls
      `doStart`/`doStop` at all — it drives `Switch.Set` directly and records its own
      `pool.solar_start`/`pool.solar_stop` notices with an explicit `reason` field. So
      schedule/manual vs. solar are *already* naturally separated by which event fires
      (`pool.pump_start`/`stop` vs `pool.solar_start`/`stop`) — no daemon-side correlation
      logic was needed, simplifying this phase considerably vs. the original plan.
- [x] `internal/shelly/scripts/pool-pump.js` `doStart` (~line 1801): `pool.pump_start` now emits
      `reason: reason || "start"` alongside `speed`/`switch_id` (`doStop` already carried reason)
- [x] `make generate` re-run after JS change — no generated-defaults diff (event payload only)
- [x] New `myhome/daemon/pool_notices.go` (`PoolNotices`, `NewPoolNotices`, `OnEvent`): subscribes
      to `pool.pump_stop` and `pool.solar_stop` via the existing `broadcastFn` hook in
      `daemon.go` (same pattern as `noticeSvc.OnEvent`); nil-receiver safe so it can be wired
      unconditionally. Computes `turnover_achieved` from `PoolRuntimeTracker.DailyRuntimeSec` +
      pool-volume/max-flow-rate/max-rpm/speed KVS (reusing `computeRuntimeTargets`'s helpers in
      `solar_automation.go`), `turnover_target` from KVS `turnover`; records companion
      `pool.turnover_today` notice. Unit tests in `pool_notices_test.go` (roundTo, nil-receiver,
      event-name filter).
- [x] Manual CLI reason: no change needed — already flows through `doStart`/`doStop` (see above)
- [x] `internal/myhome/shelly/gen2/listener.go` `severityFor`: `pool.water_supply_protected` moved
      warn→notice, `pool.water_supply_restored` and `pool.turnover_today` added as notice;
      `listener_test.go` table updated

### Phase 3 — Readable digest/log rendering — DONE (event-log reuse deliberately skipped)
- [x] `myhome/notice/digest.go`: new `humanizePoolData(event, data)` + `hoursToClock(h)` render
      pool events as sentences (run_window w/ mode+clock times, pump_start w/ speed+reason,
      pump_stop w/ reason, turnover_today, water_supply protected/restored, solar_start/stop);
      unrecognized events or unparseable JSON fall back to the raw JSON tail unchanged, so
      `formatDigest`'s prefix columns (device/component/event) are untouched — only the tail
      changed, keeping all pre-existing digest tests passing. New `digest_pool_test.go`.
- [x] Found & fixed a pre-existing, date-dependent flaky test while verifying:
      `TestSendDigest` (service_test.go) seeded its event at a hardcoded fixed clock date, but
      `events.Query`'s `Since` filter always uses real `time.Now()` — confirmed via a throwaway
      detached worktree at the pre-change commit that this already failed on `main` today
      (2026-07-04). Fixed by seeding relative to `time.Now()` instead.
- [ ] Reuse formatting helper in UI event log (`internal/myhome/ui/template.go`/`events_template.go`)
      — SKIPPED: would require adding `myhome/notice` as a new cross-module dependency of
      `internal/myhome/ui` (go.mod require+replace) purely to reformat the event-log's Data
      cell, which already has a reasonable collapsible-JSON UX. Lower priority than Phase 4
      (explicit user ask for CLI/UI turnover+water-supply status); revisit later if wanted.

### Phase 4 — Pool status in UI + terminal — DONE
- [x] New pool RPC verb `pool.getstatus` (`internal/myhome/const.go`), no-params/
      `PoolGetStatusResult` type (`internal/myhome/pool.go`: `DeviceID`, `WaterSupplyActive`,
      `TurnoverAchieved`, `TurnoverTarget`, `RuntimeSec`), signature registered in
      `internal/myhome/methods.go`.
- [x] Handler: `myhome/daemon/pool_rpc.go` (`PoolRPCHandler`, registered unconditionally in
      `daemon.go` next to the occupancy/temperature RPC registration) — reuses
      `PoolNotices.ComputeTurnover` (refactored out of `recordTurnoverToday`, same KVS reads) and
      new `PoolNotices.WaterSupplyActive` (reads `Input.GetStatus` id 0). Returns a clear error
      if `poolNotices` is nil (pool disabled/device unreachable) rather than panicking.
- [x] `myhome/ctl/pool/status.go`: new `printFiltrationStatus`/`formatRuntime`, calls
      `myhome.TheClient.CallE(ctx, myhome.PoolGetStatus, nil)` and prints a "Filtration Status"
      section (turnover achieved/target/runtime, water-supply OK/active) after the per-device
      mesh listing; degrades to a one-line note if the RPC errors.
  - REVISED FINDING: `PoolService.Status`/`getDeviceStatus` (the originally-planned data
    source) queries per-device KVS the same way `PoolNotices`/`SolarAutomation` already do —
    reusing the daemon's existing `PoolNotices` handle (single configured device) was simpler
    than standing up a `mhscript.PoolService` + `DeviceProvider` inside the daemon just for
    this, so the CLI/UI path goes through the new RPC verb instead of `PoolService.Status`.
- [x] `internal/myhome/ui/template.go`: added `IsPoolPump`, `TurnoverAchieved`,
      `TurnoverTarget`, `WaterSupplyActive` to `DeviceView`; new `applyPoolStatus(ctx, views)`
      calls `pool.getstatus` **once per request** (not per device — avoids N redundant
      KVS/MQTT round trips) and matches by `DeviceID`. Wired into `RenderIndex`,
      `htmx.go`'s `DeviceCards` and `DeviceCard`.
- [x] `internal/myhome/ui/htmx.go`: pool tags (💧 water-supply OK/Paused, 🔄 turnover x/day)
      added to both `deviceCardsTemplate`/`deviceCardTemplate`, via new `cardTemplateFuncs()`
      (`isActive`, `turnoverText`) — required because `html/template` does **not**
      auto-dereference `*bool`/`*float64` fields for `{{if}}`/`{{printf}}` (verified
      empirically: a non-nil pointer is always template-truthy regardless of its value, and
      `{{printf "%.1f" ptr}}` prints the pointer's address, not the float). New
      `htmx_pool_test.go` covers both funcs and full template rendering (including a
      pointer-address leak guard).
  - **Pre-existing bug found (out of scope, not fixed)**: the existing `.Temperature`/
    `.Humidity` tags in both card templates use this exact broken `{{printf "%.1f" .Temperature}}`
    pattern (`*float64` field) — confirmed via a standalone repro that this renders
    `%!f(*float64=0x...)` instead of the number. Flagged to the user; not touched here since
    it's unrelated to this feature.

### Phase 5 — Verification
- [ ] `make build` then `make test` from repo root
- [ ] Trigger pool start/stop via MCP `shelly_call`; confirm `pool.pump_start` carries `reason`
      and turnover notice appears on stop
- [ ] `go run ./myhome ctl pool status` shows Turnover + Water supply lines
- [ ] `make run`, open dev UI (port 6080): Accounts panel shows 4 accounts w/ status tags; pool
      card shows turnover + water-supply
- [ ] Trigger digest send; confirm pool notices render as readable sentences
- [ ] New unit tests pass with `go test -race` (accounts registry, digest formatting, pool RPC
      handler, CLI status output)

## Notes for a cold resume

- If resuming with no memory of this session: read this file top to bottom, `git log` on this
  branch for what's already committed, `git diff` for uncommitted work, then continue at the
  first unchecked box.
- Config conventions: no new config flags planned — reuses existing Beem/SFR/SMTP credentials.
  If a flag becomes necessary, it must touch all 4 files (`options.go`, `run.go`,
  `docs/configuration.md`, `myhome-example.yaml`).
- `pool_defaults_generated.go` is gitignored; any new `//go:generate` under `myhome/ctl/` needs
  registering in Makefile, `.goreleaser.yml`, package-release MSI step, and any bare-`go build`
  workflow (see root `CLAUDE.md`).
- Shelly JS constraints apply to the `pool-pump.js` edit: no hoisting, `var` only, never empty
  `catch`, `"prop" in obj` checks.
- RPC handler tests: restore state in `t.Cleanup`, no `t.Parallel()` (shared package-level map).
