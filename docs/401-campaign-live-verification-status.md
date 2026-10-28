# Issue #401 campaign — status and live-device verification handover

This file is a handover snapshot for whichever agent (or human) picks this up next. It assumes no
memory of any prior conversation. **Verify every claim below against current repo/device/issue
state before acting on it** — this is a point-in-time snapshot (last updated 2026-08-03 evening),
not live truth. Superseded sections from earlier snapshots have been removed rather than left to
accumulate — if you need the full incident narrative (exact commands run, exact error output), it's
preserved in this file's git history and in issue #421's body.

## Objective

Umbrella issue **#401** — "solar-driven pool pump redesign — daemon publishes availability, pump
decides for itself" (https://github.com/asnowfix/home-automation/issues/401). Root problem: the pool
pump didn't start on solar power because of a KVS-parsing bug, plus a structural issue where the
daemon issued raw `Switch.Set` calls that bypassed the pump script's own safety logic. Fix: move all
start/stop decision-making onto the device script itself; the daemon only publishes an availability
topic.

This doc's specific job is tracking **live-device verification** — #406 (deleting the legacy
`SolarAutomation` daemon code) is explicitly gated in #401's own issue text on #402/#403/#405 being
verified working against the **real pool pump device**, not just CI green. That verification is
what this doc has been tracking across sessions, and it is **not yet complete**.

## Status at a glance

| Issue | Title | Status |
|---|---|---|
| #402 | on-device runtime/turnover accumulator | **Merged** — PR #410, commit `40f8b54`, tag `campaign/401-pool-pump-runtime-accumulator` |
| #403 | daemon aggregates solar sources → `myhome/energy/solar/available` | **Merged** — PR #412, commit `1f4cabe`, tag `campaign/401-solar-source-aggregator` |
| #404 | minimal energy-claimers registry + RPC | **Merged** — PR #411, commit `4a24aba`, tag `campaign/401-energy-claimers-registry` |
| #405 | pool-pump.js solar-driven start/stop hysteresis | **Merged** — PR #419, commit `ae4c5da`, tag `campaign/401-solar-hysteresis` |
| #406 | remove legacy `SolarAutomation` daemon code | **NOT STARTED — gated on #421**, see below |
| #407 | multi-consumer "solar router" | **Out of scope**, explicit follow-up, do not touch |
| #421 | crash ("Too many calls in progress") + missing restart catch-up, found during live verification | **NOT STARTED** — blocks #406 |
| #422 | standing real-device integration test campaign (spare Shelly Plus 1 `development`) | **NOT STARTED** — not a #406 blocker; #421's live re-verification is its first tracked test case |

Side-fix also merged during this campaign, not itself a sub-issue: **PR #413** — pushing an
annotated `campaign/401-*` tag onto `main` broke `git describe --tags` version derivation. Fixed
(4 call sites got `--match 'v*'`). Confirmed working — no further action needed.

**Outstanding, not yet filed as an issue**: uploading `pool-pump.js` **unminified**
(`--no-minify`) to `filtration-hiver` fails deterministically (`Missing or bad argument 'code'!`
mid-chunk-upload) — working hypothesis is the Pro1's script storage is now too small for the raw
source since #402+#405 added ~360 lines. Minified upload works fine. Worth filing as its own small
issue (either document the effective size ceiling, e.g. in a new `docs/pool-pump.md`, or make
`script upload`/`update` warn/refuse before a doomed unminified attempt instead of failing deep into
a chunked upload with a confusing device error). Low priority relative to #421.

## Current live-device state (as of end of last session, 2026-08-03 evening)

Device: `filtration-hiver`, id `shellypro1-ec62608c0230` (Shelly Pro1), running `pool-pump.js`
(script id 2). **Re-verify this on pickup — it may have changed** (another agent, a scheduled job,
or the user may have touched it since):

- **`pool-pump.js` is crashed (`running: false`)**, killed by the "Too many calls in progress" bug
  described in #421, hit a second time right after a manual restart attempt during the last
  session. It was deliberately **left crashed/not running** rather than restarted again, since a
  third restart would very likely crash again within seconds (same systemic bug, two different call
  sites already observed) — see #421 for the full root-cause analysis.
- **The pump switch itself is confirmed OFF** (`Switch.GetStatus` → `output: false`), which is the
  safe state. This was confirmed via a manual `Switch.Set {id:0, on:false}` call, though it turned
  out to be a no-op — the crashed script's last `handleEveningStop()` run had already turned it off
  correctly just before crashing on its own follow-up KVS write.
- **Do not restart `pool-pump.js` on `filtration-hiver` and walk away** without watching it — assume
  it will crash again shortly after boot until #421 is actually fixed. If you need the pump under
  script control again before #421 lands, restart it and stay attentive (or better, test the fix on
  the spare `development` device first, per #422).

## Environment facts (still valid, re-confirm if in doubt)

- MQTT broker is at `192.168.1.2:1883`. **Always pass `--mqtt-broker tcp://192.168.1.2:1883`
  explicitly** to `ctl shelly`/`ctl pool` commands — auto-discovery/mDNS may not behave well from
  this dev Mac.
- This dev Mac has an active VPN (`utun4`, gateway `10.5.0.2`) that wins the default route over the
  real LAN (`en0`, `192.168.1.88`) — confirmed still true as of last session (`route -n get default`
  → `10.5.0.2` via `utun4`). This breaks `internal/myhome/net.MainInterface()`'s pick of the LAN IP
  for `ctl shelly script debug <device> true`'s UDP callback address. See memory
  `feedback_live_shelly_debug_workflow.md` for the manual fix. **Do not reboot the device after
  fixing the debug UDP address** — `ctl shelly script debug` only reboots if the device itself
  reports `RestartRequired`; this is now documented in `AGENTS.md` ("Testing Shelly Scripts").
- The MCP tool `shelly_list`/`shelly_call` (`mcp__shelly__*`) times out on `device.lookup` — it
  needs a running local `myhome` daemon instance to resolve device names, and none is reachable this
  way from this Mac. **Use the locally-built `./myhome/myhome ctl ...` binary directly instead**
  (lazy MQTT connect, no separate daemon process needed for `ctl shelly`/`ctl pool` commands).
- **There is a real, already-deployed, long-running `myhome` daemon somewhere on the network**
  (likely the NAS, `gruissan.local` — arm64, `.deb` install, `myhome.service` via systemd) that is
  **still running pre-#402 code**. Confirmed via the legacy KVS-parsing bug appearing in its RPC
  responses. This means `ctl pool status` and the `pool.turnover_today` notice will keep showing the
  old bug, and `myhome/energy/solar/available` is not being published at all, **until that remote
  daemon is redeployed** with a build from current `main`. This is a production-service change —
  confirm with the user before doing it (likely: build an arm64 `.deb`, ship to the NAS, restart
  `myhome.service`).
- Local build in this worktree
  (`/Users/fix/Desktop/GIT/home-automation/.claude/worktrees/bugfix+pool-pump-not-starting`) was
  last confirmed clean at commit `ae4c5da` (`make generate && make build` succeeded, embedded
  version `v0.11.0-106-gae4c5da`). Branch has since moved to `docs/401-live-verification-handover`
  at `1f89344` (this doc + related conventions/instructions commits) — **re-run
  `git log --oneline -1` and rebuild before trusting the binary**, don't assume it's still current.

## What's next, in order

1. **Fix #421** (both the crash and the missing restart-catch-up), writing the reproducing unit
   test *first* per that issue's test plan — this includes adding a minimal slice of #250 (script
   emulator resource-limit enforcement) since the current emulator can't reproduce a
   concurrent-call-ceiling crash at all yet.
2. **Verify the #421 fix on real hardware** — prefer the spare Shelly Plus 1 `development` first
   (safe dry run, per #422's test case #1) before touching `filtration-hiver` again. Confirm the
   script survives a normal boot and a deliberate mid-window restart without either bug recurring.
3. Once #421 is confirmed fixed on real hardware, decide with the user whether/how to **redeploy the
   NAS daemon** to current `main` — required before `ctl pool status`, `pool.turnover_today`, or the
   `myhome/energy/solar/available` publisher can be observed working live at all.
4. With both device and daemon current, actually observe: pump status/turnover reads correctly via
   `ctl pool status` (no parse error), and — if the user wants solar mode exercised — a real
   solar-triggered start/stop cycle, or at minimum clean startup with solar enabled and no crash.
5. Only after the user explicitly confirms this looks right on real hardware, proceed to **#406**
   (delete `myhome/daemon/solar_automation.go` and related config/docs) as its own PR.
6. (Low priority, unblocked, can happen anytime) File the unminified-upload-size issue noted above.
