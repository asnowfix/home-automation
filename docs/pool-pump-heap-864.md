# pool-pump.js heap margin on the Pro1: desk analysis (#864)

> Last updated: 2026-11-22. Phase 1 (desk analysis) done. Phases 2 and 3 not started; they need a
> device and the owner's authorization.

## Goal

`pool-pump.js` on the production pump (`filtration-hiver`, Shelly Pro1, firmware 2.0.0) settles at
`mem_peak` 23044, the whole shared heap, so a restart that does not start from a clean heap can die
with `out_of_memory` (#864). This document is the desk phase of #864: find out statically where the
margin went, classify the KVS keys for the two hypotheses of #883 (H1 merge keys, H2 bake constants),
and rank init-path allocation cuts so they can be measured on `development` (#883) and then
re-measured on a Pro1. It proposes changes; it makes none. `pool-pump.js` is unchanged by this phase.

Rules that frame every number below (`docs/shelly-heap-allocation.md`,
`.claude/skills/shelly/references/memory.md`):

- only `mem_peak` decides survival, and it is roughly *live set + garbage since the last collection*;
- minified bytes do not convert to heap at a fixed rate (0.13 to ~1.0 of the byte delta depending on
  what kind of bytes, up to 1.67 for an inline); a byte figure here is a proxy, never a heap figure;
- the goja emulator has no heap ceiling and says nothing about memory.

No device, NAS, broker, `events.db` or Shelly RPC was used. Every figure is either computed from the
repository (labelled *computed*) or quoted from an earlier measurement recorded in the repository
(labelled *recorded*, with its source).

## Current state

| item | value | source |
|---|---|---|
| `pool-pump.js` on `origin/main` (9ebc434a) | 144728 B source, **36714 B minified** | computed |
| v0.13.3, v0.14.0, v0.14.3 | byte-identical to `main` | computed (same sizes) and #862 triage |
| schema fields (`pool-pump.schema.json`) | 24, of which 2 `cliOnly` (not read by the script) | computed |
| KVS keys read at init by `loadConfig()` | **22**, one `KVS.Get` each, sequential | computed |
| KVS keys the script writes itself | `active-output`, `runtime-sec`, `runtime-ts` | computed (`STATE_KEYS`) |
| keys on the production pump | "~30" `script/pool-pump/*` | recorded (#864), not re-read |

## Phase 1: static bisect from `ae4c5da` (done)

Method: every first-parent commit on `main` that touched `pool-pump.js` since `ae4c5da`, minified
with the same minifier and settings as the upload path (`tdewolff/minify` v2.24.3, `Version: 2009`,
`pkg/shelly/script/main.go`), then counted for the allocation sources of
`docs/shelly-heap-allocation.md` §2 by regex over the minified text. Counts are static occurrences,
not runtime frequencies. The throwaway tool lived in the session scratchpad and was not committed.

| date | commit | PR / subject | minified | string-literal bytes | `function` | anon `function(` | `Shelly.call(` | `queueTask(function` | `JSON.parse` | `log(` |
|---|---|---|---|---|---|---|---|---|---|---|
| 08-03 | ae4c5da | baseline (recorded peak 22330) | 36155 | 10506 | 146 | 62 | 16 | 21 | 5 | 155 |
| 08-07 | a0e8f9d4 | #442 window re-eval | 37365 | 10681 | 152 | 64 | 17 | 21 | 5 | 156 |
| 08-08 | 7afc6926 | #447 schema as source of truth (no `description:`) | 35237 | 9013 | 152 | 64 | 17 | 21 | 5 | 156 |
| 08-09 | fb77cab1 | #458 single controller | 31264 | 7603 | 137 | 57 | 17 | 19 | 4 | 130 |
| 08-10 | 025c31d2 | #461 call safety (`CALL_SLOTS`) to main | 33651 | 8056 | 146 | 58 | 17 | 19 | 4 | 131 |
| 08-11 | 9093e8d0 | #472 runtime `{sec,ts}` persistence | 36543 | 9012 | 162 | 63 | 17 | 20 | 3 | 142 |
| 08-11 | 574f52d3 | #473 water-supply re-entrancy | 37078 | 9018 | 165 | 64 | 17 | 21 | 3 | 142 |
| 08-12 | b690d5b8 | #477 log before `MQTT.subscribe` | 37079 | 9019 | 165 | 64 | 17 | 21 | 3 | 142 |
| 08-12 | 62c2f20a | #483 wrap `script.eval` entry points | 37612 | 9251 | 167 | 65 | 17 | 21 | 3 | 146 |
| 08-16 | b940b3cf | #479 reconciler + serialised actuator | 36804 | 8726 | 156 | 48 | 18 | 20 | 3 | 132 |
| 08-16 | 18735bdb | #500 ASCII-only output | 36792 | 8714 | 156 | 48 | 18 | 20 | 3 | 132 |
| 08-16 | e230ac82 | #505 day anchor | 36914 | 8737 | 156 | 48 | 18 | 20 | 3 | 132 |
| 08-18 | 5a210a18 | #510 drop 2nd `Schedule.List` | 37008 | 8722 | 155 | 48 | 17 | 20 | 3 | 132 |
| 08-23 | 9ecf1126 | #531 day-rollover crash | 37100 | 8722 | 159 | 52 | 17 | 24 | 3 | 132 |
| 08-23 | 07ce00c1 | #526 runtime recovery, dead schedule code removed | 35186 | 7870 | 149 | 47 | 13 | 24 | 3 | 121 |
| 08-23 | 39815c12 | #537 init stack depth | 35304 | 7890 | 152 | 50 | 13 | 27 | 3 | 121 |
| 08-24 | 97731d7a | #542 inline 13 single-call-site functions | 34592 | 7871 | 139 | 50 | 13 | 27 | 3 | 121 |
| 08-26 | 05eb7d78 | #551 observed relay transition | 35224 | 8009 | 141 | 50 | 13 | 27 | 3 | 122 |
| 08-26 | 300f634b | #555 button immune to fuse | 35519 | 8009 | 142 | 50 | 13 | 27 | 3 | 122 |
| 08-26 | 23dfb8bb | #560 one-shot runtime checkpoint | 35928 | 8009 | 143 | 50 | 13 | 27 | 3 | 122 |
| 08-31 | 66eefa9a | #582 restore `CALLS_IN_FLIGHT` throttle | 36038 | 8049 | 143 | 50 | 13 | 27 | 3 | 123 |
| 09-05 | f380a143 | #595 pump event reason | 36683 | 8027 | 143 | 50 | 13 | 27 | 3 | 123 |
| 11-01 | 382db671 | #660 winter solar gate (= v0.13.3 .. v0.14.3) | 36714 | 8035 | 143 | 50 | 13 | 27 | 3 | 123 |

### What the bisect says

1. **The script did not grow in minified terms.** `ae4c5da` → `main` is **+559 B** minified (+1.5 %)
   while the source nearly doubled (+68 KB, almost all comments). String-literal bytes fell by
   2.5 KB. Static allocation sites fell too (anonymous functions 62 → 50, `log(` 155 → 123,
   `JSON.parse` 5 → 3). The "+1966/-1031 lines" of the triage is comment growth, which the
   minifier strips. Code size alone cannot explain a lost margin.

2. **The steady free heap was spent on purpose.** The recorded `mem_free` series on Pro1s is
   10276 (`ae4c5da`, recorded, `memory.md`) → 6398 (`CALL_SLOTS` cut on the #421 branch, recorded)
   → 7280 (`filtration-hiver`, 2026-09-05, build `66eefa9a`, recorded in `docs/integration-tests.md`)
   → 7154 (`filtration-hiver`, 2026-11-21, v0.14.x, recorded in #864). The ~3 KB drop is mostly the
   `CALL_SLOTS` trade that PR #426 measured as +3878 B resident for −980 B of peak, landed on main by
   #461 (`025c31d2`). It is not a regression; it is why the script survived #421.

3. **On the same device, the peak rose ~1.1 KB between 2026-08-16 and 2026-09-05, then only
   ~126 B.** Recorded settled peaks on `filtration-hiver`: 21826 (2026-08-16, build around #479,
   `memory.md`), 22918 (2026-09-05, build `66eefa9a`), 23044 (2026-11-21, v0.14.x). From 09-05 to
   now, `mem_peak` +126 and `mem_free` −126 match the +676 B minified of #595 and #660 at a ~0.19
   conversion, inside the documented range. The larger 08-16 → 09-05 step (+1092 B) coincides with
   #505, #510, #531, #526, #537, #542, #551, #555, #560 and #582, during which the minified size went
   *down* (36792 → 36038). Candidates for that step, none verified:
   - #582 restored the `CALLS_IN_FLIGHT` throttle (4 calls max): calls that used to fail fast now
     queue, so more slots, params objects and task-queue entries can be alive at once during init;
   - #526 and #551 added init-time recovery work (`loadRuntimeStateFromKVS`, `reconcileMissedStop`,
     `run-start` in `Script.storage`), i.e. more live code paths exercised during the init window;
   - #542's inlining, which the calibration of #570/#569 measured at up to 1.67× its byte delta;
   - device state, not code: KVS keys added on the device in that period (state keys, legacy keys,
     the `#537` key), or a different settle time between the two readings (the 2026-09-05 one was a
     single HTTP read; its settle is not recorded).
   **The two endpoint readings are single samples with unrecorded settle times. Treat the 1092 B as
   an observation to explain, not a measured regression attributable to a commit.**

4. **What can be ruled out: v0.14.x and the minified size.** What cannot be ruled out from the desk:
   device KVS state (the ~30 keys versus 24 in the schema, see Phase 1b) and the firmware.

5. Per-commit logic checks in the goja emulator were not run: they would say nothing about memory
   (rule above), and no logic regression is suspected.

## Phase 1b: KVS key classification for #883's H1 and H2 (done, repo evidence only)

Edit frequency could **not** be observed: that needs the NAS `events.db` (`config_changed` rows) and
the device's KVS history, both out of scope for this phase. The classes below are *write paths
found in the code*, which bound how a key can change; "never edited" must still be observed before
H2 is applied (#883's own rule).

Who writes these keys, from the repository:

- `ctl pool add` / `ctl pool update` (`internal/myhome/shelly/script/pool.go`, `PoolKVSKeys`) write
  **the whole set at once** (the map at `pool.go:130-160`). There is no per-key setter for config in
  `myhome/ctl/pool/`, except `ctl pool speed <speed>` (`setup.go:224`) for `speed`.
- The script writes `active-output`, `runtime-sec`, `runtime-ts` (fire-and-forget `KVS.Set`); the
  daemon reads `runtime-sec` (`myhome/daemon/pool_notices.go`).
- The script reads all 22 non-`cliOnly` config keys once, at init, sequentially.

So every config key is **co-written** (by `ctl pool add/update`) and **co-read** (by `loadConfig`).
Co-access does not split the set into groups; edit paths do:

| class | keys (KVS suffix) | n | write path | H1 (merge) | H2 (bake) |
|---|---|---|---|---|---|
| A. hardware / pool constants | `eco-speed`, `day-speed`, `max-speed`, `pool-volume`, `max-flow-rate`, `max-rpm`, `eco-rpm`, `day-rpm` | 8 | `ctl pool add/update` only | yes | **best H2 candidates**: they describe the pump and the pool, not a policy |
| B. tuning policy | `temp-threshold`, `turnover`, `max-temp`, `override-ms`, `logging` | 5 | `ctl pool add/update` only | yes | possible, if observed unedited |
| C. solar policy | `solar-enabled`, `solar-start-w`, `solar-stop-w`, `solar-start-delay`, `solar-stop-delay`, `solar-min-turnover`, `solar-max-turnover`, `solar-stale-ms` | 8 | `ctl pool add/update` only | yes | weaker: actively tuned during the solar campaign (#405, #621) |
| D. operator-facing | `speed` | 1 | `ctl pool speed`, add/update | keep separate | no |
| E. CLI-only (never read by the script) | `mqtt-topic`, `night-duration` | 2 | `ctl pool add/update` | not loaded | n/a: cost nothing at init today |
| F. script state | `active-output`, `runtime-sec`, `runtime-ts` | 3 | script, every change / checkpoint | no (written at different times; `runtime-sec` is read by the daemon) | no |
| G. unknown | the rest of the "~30" on production (legacy keys from older layouts, e.g. pre-#469 runtime keys or the pre-`Script.storage` `schedule-mode`, are plausible) | ~3-6 | unknown | **delete if unread** | n/a |

### H1, merge co-accessed keys

The co-access analysis allows the extreme form: **one** `script/pool-pump/config` value for classes
A to C (21 keys), keeping `speed` and the state keys separate. That turns 21 `KVS.Get` round-trips
(each with its own callback closure, params object, key string and task-queue hop) into one.

- **Format.** JSON costs a transient: the response string plus the parsed object graph are alive
  together (~4 B of peak per byte of payload for HTTP bodies, `memory.md`; KVS has not been
  measured). A comma-separated positional string of ~21 short numbers is ~120 B; `split(",")`
  allocates an array plus one string per piece, then each is converted with `Number()`. Positional
  CSV is the cheaper transient and needs no key names on the device, at the price of a fragile
  layout (order fixed by the schema, versioned by a leading field count). Both must be measured.
- **Value length limit.** Shelly KVS values have a firmware size limit; it is not checked anywhere in
  this repo. Verify on `development` before choosing JSON.
- **Partial updates.** `ctl pool speed` stays a single key, so the only partial writer is unaffected.
  `ctl pool add/update` already rewrites everything.
- **Go side.** `PoolKVSKeys` and the generated maps (`tools/genconfigschema`) change shape; the
  generator is the right place to emit both the Go encoder and the JS decoder from the schema.
- **Migration.** A device with the old per-key layout must keep working (read the merged key, fall
  back to per-key), and the old keys must be deleted once migrated, or they remain as class G.

### H2, bake never-edited values at upload time

- Substitution belongs in `tools/genconfigschema` or the upload step, driven by the schema, never in
  hand-edited JS.
- **Version-marker consequence (#863, PR #884).** The marker `script/<name>.js` is the sha1 of the
  packaged source. A per-device substituted source has a different sha1, so the postinst
  `script update '*'` would see a mismatch on every run and re-upload the pump script forever,
  which is exactly the dangerous operation #863 and #864 describe. The marker must be defined as the
  sha1 of the packaged source *plus* the baked values (or of the substituted output), and the
  comparison in `UploadWithVersionDetailed` (`internal/myhome/shelly/script/ops.go`) must compute it
  the same way. PR #884's `--include-pump` guard should land first.
- **Operational cost.** Changing a baked value becomes an upload of the pump script. Only class A
  qualifies without observation; B and C need the edit-frequency query first.
- H2 removes both the key and its read; H1 removes only the per-key reads. If H1 is applied, H2's
  marginal gain is the bytes of a few numbers in one string, so **H1 first, H2 only if H1 is not
  enough**.

## Phase 1c: ranked init-path allocation cuts (proposals, not implemented)

Ranked by expected `mem_peak` effect over risk. "Expected" is reasoning from the documented
allocation sources, not a measurement; each line says what would confirm or refute it.

| rank | cut | where | expected effect | risk | measure how |
|---|---|---|---|---|---|
| 0 | **Locate the peak first.** Sample `mem_peak` at fixed points: after `loadConfig`, after `finishContinueInit`, after the four init steps, after the first `handleDailyCheck` (forecast). | probe on `development`, then Pro1 | none; decides which of the rows below matters | none on `development` | #883 bench, reboot between arms |
| 1 | `loadConfig`: replace the per-key anonymous `KVS.Get` callback with one named callback that takes the key index through `Shelly.call`'s `userdata` (the `CALL_SLOTS` wrapper already forwards `ud`) | `pool-pump.js:317` | removes 22 closures that each pin `key`, `schema`, `kvsKey` and `loadNextKey`'s frame; this is the §2.1 pattern `shelly-heap-allocation.md` names as the corroborating cause of the 5-vs-24-key 7.9 KB. Plausibly hundreds of bytes of peak, possibly more | low: one function, logic unchanged, emulator tests cover it | baseline vs arm, production-like 30 keys |
| 2 | **Forecast payload.** Ask Open-Meteo for less: `timeformat=unixtime` (24 ISO time strings become numbers), or `daily=temperature_2m_max,sunrise,sunset` without `hourly` if the peak hour can be derived otherwise | `setForecastURL`, `pool-pump.js:901`; parse at `onForecast` | ~4 B of peak per response byte (`memory.md`); the 24 hourly time strings are a large share of the ~861 B body. Matters only if row 0 shows the peak is the forecast, which is likely now that steady `mem_free` (7154) no longer leaves the ~4.4 KB the fetch needs beside init garbage | medium: changes parsing of a remote payload; must degrade if the field is missing (internet-optional rule) | row 0 sample around the first daily check |
| 3 | H1: merge classes A to C into one KVS value | schema, generator, `loadConfig`, `ctl pool` | removes 20 of 22 init round-trips and their transients; the largest structural lever if per-key cost is what the 7.9 KB was | medium-high: Go and JS layout change, migration, value-length limit | #883 H1 arms, CSV vs JSON |
| 4 | Delete class G keys (unread legacy keys) on the pump | device KVS | unknown: depends on whether an *unread* key costs script heap at all (open question 2) | low, but it is a write on the production pump | first measure on `development` whether unread keys cost anything |
| 5 | Init steps: the four `initSteps` closures and the `queueTask(function() { next(); })` wrappers become named functions; `step(function(){...})` passes a shared named continuation | `pool-pump.js:3179-3270` | a handful of closures, each pinning `finishContinueInit`'s frame (including the `initSteps` array) | low | same bench |
| 6 | `Schedule.List` at init: release `result` as soon as `onWindowJobs` has run, and avoid holding it across `queueTask` | `pool-pump.js:3207-3245` | the #480 wrapper made every job's `code` longer, so this response grew; ~670 B measured for one extra parse (2026-08-12) | low | same bench |
| 7 | H2 for class A only | generator + upload + marker | small after H1; larger without it | high operationally (marker, re-upload of the pump) | only after H1 is measured |
| 8 | `computeFlowRate` lookup object rebuilt per call; `log()` arguments built while logging is off | `pool-pump.js:2586`, `log()` call sites | steady-state garbage, not init; low peak effect | low | not a priority for #864 |

Not proposed: further minified-size cuts. The bisect shows the size is flat and the 1519 B trim of
#421 moved the peak by zero.

## Phase 2: measure on `development` (not started; owner, #883)

Set `development` up as #883 describes (30 production-like keys, `watchdog.js` resident). Run row 0
first, then rows 1, 2, 3 as separate arms against a baseline arm in every series, rebooting between
arms, N ≥ 3 each. Record results on #883. Only relative deltas transfer: `development` is a Plus1 on
firmware 1.7.5.

## Phase 3: re-measure on a Pro1 (not started; owner authorization per step)

Kept candidates are re-measured on `filtration-hiver` under #864's rules and the standing
authorizations in `.claude/skills/shelly/references/field-discipline.md`, outside the 23:00-00:30
window, relay OFF, with before-state (`Script.GetStatus`, full KVS map) captured to a file. No pump
script push before #863's guard (PR #884) is merged.

## Open questions (need the owner or a device)

1. **Where is the peak?** Init (`loadConfig`) or the first daily check (forecast fetch) right after
   it? Row 0 decides the ranking between rows 1 and 2.
2. **Does an unread KVS key cost script heap?** The 5-vs-24-key measurement cannot separate keys
   *stored* from keys *read*. A `development` arm with the same 22 reads and +8 unread keys answers
   it, and decides whether row 4 is worth a write on the pump.
3. **What are the ~6 keys on the pump beyond the 24 schema keys?** A read-only `KVS.List` on the pump
   (paged), owner's call.
4. **Edit frequency** of classes B and C, from `events.db` on the NAS (`config_changed` rows and
   `ctl pool` history), read-only, owner's call. Required before H2 for anything beyond class A.
5. **The +1092 B step between 2026-08-16 and 2026-09-05** on `filtration-hiver`: was the device's
   KVS state different between those readings? If the keys were the same, building `e230ac82` and
   `66eefa9a` on `development` with the same KVS would bisect it on hardware.

## Key files

- `internal/shelly/scripts/pool-pump.js`: `loadConfig` (233-361), `CALL_SLOTS` wrapper (421-562),
  `setForecastURL`/`onForecast` (901-975), init (3119-3271).
- `internal/shelly/scripts/pool-pump.schema.json`: the 24 fields; `tools/genconfigschema` generates
  the JS `CONFIG_SCHEMA` and the Go `PoolKVSKeys`.
- `internal/myhome/shelly/script/pool.go`: the full-set KVS write of `ctl pool add/update`.
- `internal/myhome/shelly/script/ops.go`: version marker comparison (H2 consequence).
- `docs/shelly-heap-allocation.md`, `.claude/skills/shelly/references/memory.md`: method and the
  recorded measurements quoted above.
