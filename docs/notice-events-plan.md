# Notice Events + Email Digest Plan

A curated `notice` event severity for the handful of decisions a human actually cares about
(pool schedule, solar pump on/off, garden plan, motion at night/while absent), plus a daily
email digest of those notices. See full design rationale in the approved plan; this doc tracks
phased execution and decisions.

## Decisions recorded

| # | Decision |
|---|----------|
| 1 | `notice` is a new value in the existing `events.severity` column — no schema change |
| 2 | Night window is fixed/configurable (`HH:MM`–`HH:MM`, default `22:00`–`06:00`), no solar-position calc, fully offline |
| 3 | One daily digest at a configurable hour (default `08:00`), covering the last 24h of `notice` events |
| 4 | Agnostic `notify.Mailer` interface; Gmail app-password SMTP is the only implementation for now |
| 5 | Email is skipped (no-op mailer) whenever `MYHOME_SMTP_FROM` is absent from `.env` — no error, just a log line |
| 6 | No new RPC verb — notices are plain events with `severity=notice`; existing `event.list` RPC + UI/CLI filters cover them |

---

## Phases

- [x] Phase 0 — this plan doc
- [x] Phase 1 — `notice` severity vocabulary (listener classification, UI template, CLI rank)
- [x] Phase 2 — `myhome/notify` Mailer package (interface + Gmail impl + no-op)
- [x] Phase 3 — `myhome/notice` service (motion rule + daily digest scheduler), wired into daemon
- [x] Phase 4 — solar pump notice emission (`solar_automation.go` gap)
- [x] Phase 5 — config wiring (options.go, run.go, docs/configuration.md, myhome-example.yaml)
- [x] Phase 6 — `.env` / dpkg-reconfigure SMTP credential prompts
- [x] Phase 7 — `make test` + manual end-to-end verification

All phases complete. `make build` and `make test` pass clean across all workspace
sub-modules (including the two new ones, `myhome/notify` and `myhome/notice`).
`myhome daemon run --help` and `myhome ctl events list/follow --help` confirm
the new flags and the `notice` severity are wired through end to end.

Each phase is committed separately with this checklist updated in the same commit.

## Notice event catalog

| Event | Component | Origin | Severity assignment |
|---|---|---|---|
| `pool.run_window` | `pool` (script) | on-device, via MQTT `+/events/rpc` | `severityFor()` in gen2 listener |
| `pool.pump_start` / `pool.pump_stop` | `pool` (script) | on-device | `severityFor()` |
| `pool.solar_start` / `pool.solar_stop` | `solar` | Go daemon (`SolarAutomation.step`) | set directly to `notice` |
| `garden.plan` / `garden.skip_rain` / `garden.skip_frost` / `garden.plan_fallback` | `garden` (script) | on-device | `severityFor()` |
| `motion.absent` | `motion` | derived in `myhome/notice` from `motion.detected` + `occupancy.IsOccupied` | set directly to `notice` |
| `motion.night` | `motion` | derived in `myhome/notice` from `motion.detected` + night window | set directly to `notice` |

## Verification

- `make test` — canonical, covers all workspace sub-modules including the two new ones
  (`myhome/notify`, `myhome/notice`).
- Manual: `/event-log` severity filter shows `notice` with its own color; `myhome ctl events
  list --severity notice`; trigger a live notice via a Shelly device or the `shelly` MCP tools;
  confirm the no-op mailer logs "email disabled" and sends nothing when `MYHOME_SMTP_FROM` is
  unset, and that a real digest send succeeds once SMTP env vars are set.
