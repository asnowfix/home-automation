# Issue #401 campaign — status and live-device verification handover

This file is a handover snapshot for whichever agent (or human) picks this up next. It assumes
no memory of any prior conversation. Verify every claim below against current repo/device state
before acting on it — this is a point-in-time snapshot, not live truth.

## What #401 is

Umbrella issue: "solar-driven pool pump redesign — daemon publishes availability, pump decides
for itself" (https://github.com/asnowfix/home-automation/issues/401). Root problem: the pool pump
didn't start on solar power because of a KVS-parsing bug, plus a structural issue where the daemon
issued raw `Switch.Set` calls that bypassed the pump script's own safety logic. Fix: move all
start/stop decision-making onto the device script itself; the daemon only publishes an
availability topic.

Sub-issues and their **current merge status**:

| Issue | Title | Status |
|---|---|---|
| #402 | on-device runtime/turnover accumulator | **Merged** — PR #410, commit `40f8b54`, tag `campaign/401-pool-pump-runtime-accumulator` |
| #403 | daemon aggregates solar sources → `myhome/energy/solar/available` | **Merged** — PR #412, commit `1f4cabe`, tag `campaign/401-solar-source-aggregator` |
| #404 | minimal energy-claimers registry + RPC | **Merged** — PR #411, commit `4a24aba`, tag `campaign/401-energy-claimers-registry` |
| #405 | pool-pump.js solar-driven start/stop hysteresis | **Merged** — PR #419, commit `ae4c5da`, tag `campaign/401-solar-hysteresis` |
| #406 | remove legacy `SolarAutomation` daemon code | **NOT STARTED — gated**, see below |
| #407 | multi-consumer "solar router" | **Out of scope**, explicit follow-up, do not touch |

**#406 is explicitly gated in the issue text**: it must not be started until #402/#403/#405 are
verified working against the **real pool pump device**, not just CI green. That verification is
what this session was in the middle of when it got interrupted. Do not start #406 until that
verification is actually done and the user has confirmed it.

Side-fix also merged during this campcampaign, not itself a sub-issue: **PR #413** — pushing an
annotated `campaign/401-*` tag onto `main` broke `git describe --tags` version derivation (picked
the campaign tag instead of the nearest `vX.Y.Z` release tag), which broke the Debian package
version format. Fixed by adding `--match 'v*'` to all 4 affected `git describe` call sites
(`Makefile:242`, `Makefile:323`, `myhome/Makefile:10`, `.github/workflows/on-tag-main.yml:19`).
Confirmed working (`v0.11.0-99-g4bc9172` etc.) — no further action needed on this.

## Orchestration note (context, not actionable)

Sub-issues #402–#405 were implemented via spawned sub-agents, reviewed, merged, and tagged from
the main coordinating session directly (not through the originally-launched "orchestrator" agent,
which stalled/failed repeatedly — ~8 times, mostly transient API disconnects and a couple of
apparent interactive-git-editor hangs — and was eventually abandoned in favor of driving things
directly). This is just background; it doesn't affect what's left to do.

## What's in progress right now: real-device verification on `filtration-hiver`

Device: `filtration-hiver`, id `shellypro1-ec62608c0230` (a Shelly Pro1). This is the device
running `pool-pump.js`.

**Environment facts, confirmed this session:**
- MQTT broker is at `192.168.1.2:1883`. **Always pass `--mqtt-broker tcp://192.168.1.2:1883`
  explicitly** — the user explicitly asked for this (auto-discovery / mDNS may not behave well
  from this dev Mac). Do not rely on default discovery.
- This dev Mac has an active VPN (`utun4`, gateway `10.5.0.2`) that wins the default route over
  the real LAN (`en0`, `192.168.1.88`). This previously broke `internal/myhome/net.MainInterface()`
  picking the wrong source IP for the UDP callback address used by
  `myhome ctl shelly script debug <device> true` (device-level debug). See memory
  `feedback_live_shelly_debug_workflow.md` for the exact symptom and manual fix
  (`Sys.SetConfig` with the correct LAN IP) — this has NOT yet been re-checked in this session, do
  it before relying on debug UDP output.
- The MCP tool `shelly_list`/`shelly_call` (mcp__shelly__*) times out on `device.lookup` — it needs
  a running myhome daemon instance to answer that RPC and none was reachable this way. Use the
  locally-built `./myhome/myhome ctl ...` binary directly instead (it does lazy MQTT connect / can
  talk to devices without a separate daemon process for most `ctl shelly`/`ctl pool` commands).
- **There is a real, already-deployed, long-running `myhome` daemon somewhere on the network
  (likely the NAS, `gruissan.local`, per memory `reference_nas_ssh.md` — arm64, `.deb` install,
  `myhome.service` via systemd) that is still running PRE-#402 code.** Confirmed by: `ctl pool
  status filtration-hiver` returned `PoolGetStatus` RPC content containing the exact legacy bug
  description (`read active speed: parse KVS script/pool-pump/speed="eco": strconv.ParseFloat:
  parsing "e": invalid syntax`), which only exists in the OLD `readPoolKVSFloat`-on-`speed`-key
  code path (`myhome/daemon/solar_automation.go`), not the new `ComputeTurnover()` in
  `myhome/daemon/pool_notices.go` (which reads pre-computed KVS keys and has no such parse). This
  means **`ctl pool status` and the `pool.turnover_today` notice will keep showing the old bug
  until that remote daemon is redeployed with the new merged binary** — this is separate from the
  device-script work below and has NOT been done yet. Decide with the user whether/how to redeploy
  it (likely: build an arm64 `.deb` from current `main`, ship it to the NAS, restart
  `myhome.service`) — this is a production-service change, confirm before doing it.

**Local build state (this worktree,
`/Users/fix/Desktop/GIT/home-automation/.claude/worktrees/bugfix+pool-pump-not-starting`):**
- Was on a stray branch (`issue-405-solar-hysteresis` at `992d62f`) at the start of this sub-task
  for unclear reasons (possibly left over from earlier in this long session) — switched to `main`
  and fast-forwarded to `ae4c5da` (HEAD at time of writing, includes #402/#403/#404/#405/#413).
  Re-verify `git log --oneline -1` and `git status` before continuing — another agent may have
  moved main further since.
- `make generate && make build` succeeded cleanly from repo root of this worktree. Binary is at
  `./myhome/myhome` (relative to worktree root). Embedded version string correctly showed
  `v0.11.0-106-gae4c5da`, confirming PR #413's fix works end-to-end.

**Script upload — DONE, with an important finding:**
- `./myhome/myhome ctl shelly script update filtration-hiver pool-pump.js --no-minify --force
  --mqtt-broker tcp://192.168.1.2:1883` **failed deterministically** (same result on two separate
  attempts) at byte offset 53248 with device error `Missing or bad argument 'code'! (code:-103)`
  on a `Script.PutCode` chunk call. The unminified script is 76614 bytes. Byte 53248 is chunk #26
  of 38 (2048-byte chunks) — not a boundary/last-chunk artifact, and the content at that offset is
  ordinary ASCII JS (part of the new solar-hysteresis code from #405), nothing structurally odd.
  **Working hypothesis (not fully proven): the Shelly Pro1's script storage/heap is too small to
  hold the raw unminified source now that #402+#405 added ~360 lines** — `--no-minify` is meant
  for debugging readability per this repo's own convention (see root `CLAUDE.md`), not for
  production size.
- Retried **without** `--no-minify` (i.e. with minification): `./myhome/myhome ctl shelly script
  update filtration-hiver pool-pump.js --force --mqtt-broker tcp://192.168.1.2:1883` —
  **succeeded**: `✓ Successfully updated pool-pump.js (id: 2)`. So the minified script does fit and
  upload cleanly. This is worth writing up as a real finding (possibly a follow-up issue: either
  document the effective size ceiling somewhere, e.g. `docs/pool-pump.md`, or make `script
  upload`/`update` warn/refuse before attempting an unminified upload known to be too large for a
  device's declared JS RAM, rather than failing deep into a chunked upload with a confusing
  "missing code argument" device error). **Not yet filed as an issue — consider doing so.**

**Immediately after the successful minified upload, the device stopped responding to MQTT RPC
calls** (`ctl pool status` reported "No devices running pool-pump.js" — likely a discovery-scan
failure, not necessarily the specific device; a direct `ctl shelly call filtration-hiver
Script.List --mqtt-broker tcp://192.168.1.2:1883` also timed out after 14s). **This was the last
thing observed before this session was paused — it is NOT yet understood whether:**
- the device is just briefly busy/recompiling the new script and needs more time before responding
  again (plausible — Shelly script recompilation can take a few seconds to tens of seconds), or
- the new script (or the act of re-creating/restarting it) crashed or is looping the device, or
- something is wrong with MQTT connectivity/broker state unrelated to the device itself (less
  likely given the earlier successful upload went through the same channel).

**This is the very next thing to investigate.** Suggested next steps, in order:
1. Wait a short interval (say 30–60s from whenever the successful upload happened) and retry a
   simple, cheap call first — e.g. `./myhome/myhome ctl shelly call filtration-hiver
   Shelly.GetDeviceInfo --mqtt-broker tcp://192.168.1.2:1883` (basic device-level reachability,
   independent of scripts) before retrying `Script.List`.
2. If the device responds to `Shelly.GetDeviceInfo` but still not to `Script.List`/script-specific
   calls, that narrows it to a script-level problem (e.g. new solarEnabled=false path still
   crashing on init, or the script failed to start after upload).
3. If the device doesn't respond to anything, it may genuinely be rebooting/crash-looping — check
   via the physical device or its own local status LED/web UI if accessible, or wait longer and
   retry, before assuming anything is broken code-side. **Do not reboot the device manually /
   proactively** — recall project convention (`feedback_live_shelly_debug_workflow.md`): follow
   explicit literal steps, don't improvise extra reboots.
4. Once basic reachability is confirmed, the actual verification plan (not yet started) is:
   - `ctl shelly script debug filtration-hiver true --mqtt-broker tcp://192.168.1.2:1883`
     (device-level UDP debug), backgrounded with output to a file, read that file directly (don't
     pre-filter with grep/Monitor — see the live-debug-workflow memory for why). Check first
     whether the VPN-vs-LAN-IP callback issue recurs (it broke this exact mechanism previously on
     this same Mac).
   - Confirm the script actually started (`Script.List`/`Script.GetStatus` id 2, running=true).
   - Check KVS state for the new solar-* keys — they'll likely be **absent** (falling back to
     script defaults, e.g. `solar-enabled` defaults to `false`) since KVS defaults are written by
     `ctl pool setup`/`ctl pool add`, not by a script upload. Uploading the script alone does NOT
     enable solar mode. Decide with the user whether to also run `ctl pool setup`/`add` with the
     new `--solar-*` flags to actually exercise the hysteresis live, or whether to just confirm the
     script boots cleanly with solar disabled (safe/no-op) as a first pass.
   - Watch for the expected startup log lines (KVS reads with "using default" warnings for
     unset solar-* keys, similar to the pattern already seen in the CI emulator logs during PR
     review), and no crashes.
   - Once solar-enabled is actually turned on (if the user wants that tested live), watch for
     `Subscribed to myhome/energy/solar/available` and hysteresis log lines when a real/simulated
     solar-available MQTT message arrives — this requires **#403's daemon-side aggregator to
     actually be running somewhere and publishing**, which circles back to the stale-remote-daemon
     problem above: the currently-deployed daemon predates #403 too, so it isn't publishing
     `myhome/energy/solar/available` at all yet. Full live solar-hysteresis verification is
     therefore blocked on redeploying the daemon, not just the device script.

## Bottom line / what "done" looks like before #406 can start

1. Confirm `filtration-hiver` is reachable again and running the new `pool-pump.js` cleanly
   (id 2, started, no crash loop).
2. Decide on and (if approved) execute a daemon redeploy to the NAS so `ctl pool status`,
   `pool.turnover_today`, and the new `myhome/energy/solar/available` publisher are actually live
   — without this, solar hysteresis can never be observed end-to-end no matter what the device
   script does.
3. With both device and daemon current, actually observe: pump status/turnover reads correctly via
   `ctl pool status` (no parse error), and — if the user wants solar mode exercised — a real
   solar-triggered start/stop cycle, or at minimum clean startup with solar enabled and no crash.
4. Only after the user explicitly confirms this looks right on real hardware, proceed to #406
   (delete `myhome/daemon/solar_automation.go` and related config/docs) as its own PR.
