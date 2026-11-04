# pool-pump.js — call-site census (measure/inline-single-callsite)

Base commit: `origin/main` `07ce00c1` (2026-08-23). Every function defined in
`internal/shelly/scripts/pool-pump.js`, its real call-site count, and whether it is safe to
inline on this branch. This is an experiment, not a proposal — see the branch/PR description
for the measured hypothesis this tests.

**Updated 2026-08-24**: branch merged with `origin/main` `39815c12` (#533/#537, three commits
ahead of the original base) to reconcile with upstream changes to the same two functions this
census flags as inlined/deferred — `migrateLegacyRuntimeState` (inlined here) and
`loadRuntimeStateFromStorage` (deferred here, unaffected as a candidate). #533/#537 wrapped
several `log()` calls inside those functions in `queueTask(...)` to keep them off the synchronous
`loadState()` chain (#530). The merge (via `git merge`, auto-resolved with no textual conflicts)
preserves those deferrals inside the already-inlined body — see the code comments at
`loadRuntimeStateFromStorage()` for the reconciled result. No entry in the table below changed:
the same 13 functions are inlined, the same set is deferred/flagged/never-inlinable, and the
`#530` stack-depth reasoning that kept `loadRuntimeStateFromKVS` untouched is unaffected. The
inlining of `migrateLegacyRuntimeState` now *removes* one call frame from its own two deferred
warnings' effective depth (they used to run one frame deeper than
`reconcileRuntimeState`'s own logging; after inlining there is no separate frame to be deeper
than) — a headroom improvement, not a regression.

## Method

A naive `grep -c functionName` overcounts (matches comments, doc references) and undercounts
nothing structurally, but does not distinguish a *direct call* from a *function value handed to
an external dispatcher* (`Shelly.call`, `queueTask`, `MQTT.subscribe`, `addEventHandler`) — both
look like one textual occurrence. Every candidate below with exactly one non-definition
occurrence was read in context (not just counted) to classify it. Two additional traps, beyond
the two hard constraints in the issue, showed up doing this by hand and are called out because
they generalize to future inlining passes:

- **Callback-by-reference on a hot path.** A function passed *by name* to `Shelly.call`,
  `queueTask`, or `MQTT.subscribe` still has exactly one call site, but inlining it means turning
  a persistent, once-allocated named function into an anonymous closure literal that is
  re-created every time the surrounding statement executes. For a function on this script's
  hottest paths (every RPC call, every queued task) that trades a fixed one-time cost for
  recurring allocation churn — not a clear heap win, and a stack-depth-adjacent risk on a device
  already this close to its ceiling. Not inlined regardless of call count.
- **Non-tail early return.** A function with an early `return` that is called as a bare/void
  statement — not the last statement of its caller, and not itself supplying the caller's return
  value — cannot be inlined by simple text substitution: the early `return` becomes a return from
  the *caller*, silently skipping whatever code follows the original call site. Found twice in
  this file (`checkTrack()`, `configureComponentNames()`); both are flagged unsafe below with the
  specific code that would have been skipped.

## Census

Legend for **Call site**: `direct` = plain `foo(...)` call; `direct/tail` = plain call that is
the last statement of its caller (early returns inside are therefore safe to inline verbatim);
`by-ref` = the function's *name* is passed as a value to an external dispatcher.

| Function | Real call sites | Where from | Inlinable | Notes |
|---|---|---|---|---|
| `initConfig` | 1 | direct, top-level init statement | **yes — done** | 3-line `for` loop, no return value |
| `shouldRefreshForecast` | 1 | direct, inside `if (...)` | **yes — done** | single boolean expression, no side effect beyond the `todayDateString()` call it already made |
| `getMaxForecastTemp` | 1 | direct, assigned to var | **yes — done** | trivial `return STATE.maxForecastTemp;` |
| `ensureForecastUrl` | 1 | direct/tail, only statement of `performDailyModeCheck` | **yes — done** | 3 return points, but tail position makes early returns harmless |
| `fetchAndCacheForecast` | 1 | direct/tail, tail of the `if`-branch inside `ensureForecastUrl`'s callback | **yes — done** | same tail-position reasoning, nested one level deeper |
| `migrateLegacyRuntimeState` | 1 | `return migrateLegacyRuntimeState();` — tail of `loadRuntimeStateFromStorage` | **yes — done** | textbook tail-call inline: body pasted in place of the `return` |
| `computeTurnoverToday` | 1 | nested inside `storeValue("turnover-today", computeTurnoverToday(sec))` — **argument list** | **yes — done, restructured** | hoisted to a temp var (`turnoverToday`) *before* the `storeValue()` call per constraint 1; never folded into the argument list |
| `turnOffOtherOutputsFailed` | 1 | direct/tail, `if (error_code) { ...; return; }` branch inside `setOutput`'s callback | **yes — done** | 3-line body, nothing follows in that branch |
| `handleInputEvent` | 1 | direct/tail, one branch of the shared event dispatcher | **yes — done** | dispatcher's own `else if` guard already matches the function's internal guard — folding removed a now-redundant check |
| `handleButtonEvent` | 1 | direct/tail, one branch of the shared event dispatcher | **yes — done** | same as above |
| `turnOffOtherOutputs` | 1 | direct, single statement in `applyOutput` | **yes — done** | pure one-line wrapper around `turnOffOtherOutputsNext(STATE.outputs, 0, ...)` |
| `fuseRecord` | 1 | direct, single-statement `if` body in `applyOutput` | **yes — done** | pure one-line wrapper around `FUSE_CHANGES.push(Date.now())` |
| `verifySchedules` | 1 | direct/tail, "Step 4/4" of the init step array | **yes — done** | its entire body was already one statement (a `Shelly.call`); `cb` renamed to the step's own `next` |
| `loadStorageObject` | 1 | direct, `var obj = loadStorageObject(...)` — **not tail** | no | `try/catch` with 3 return points; assigned to a var with code following — needs an IIFE or if/else restructuring to inline without an early-return bug; deferred |
| `loadStorageNumber` | 1 | direct, `var legacySec = loadStorageNumber(...)` inside the now-inlined `migrateLegacyRuntimeState` body — **not tail** | no | 3 return points, not tail; low-stakes (one-time legacy migration path) but deferred to control risk/time in this pass |
| `epochSecondsForDateString` | 1 | direct, `var legacyTs = epochSecondsForDateString(...)` — **not tail** | no | 4+ return points; deferred, same reasoning as above |
| `loadRuntimeStateFromStorage` | 1 | direct, `var fromStorage = loadRuntimeStateFromStorage();` in `loadState` — **not tail** | no | 2 return points; inlining also interacts with the just-inlined `migrateLegacyRuntimeState`, compounding restructuring risk in the runtime-persistence path — deferred |
| `loadRuntimeStateFromKVS` | 1 | direct/tail, tail of `loadState`'s async-fallback branch | **technically yes, deliberately deferred** | no early returns, genuinely safe by the tail-call rule and would *reduce* one frame of stack depth — but this exact chain (`loadState` → `loadRuntimeStateFromKVS` → `reconcileRuntimeState`) is the one the `#530` comment in the source names as having *zero remaining stack headroom* on the synchronous init path. Not touched without hardware validation. |
| `configureComponentNames` | 1 | direct, bare statement in `continueInit` — **not tail** | **no — unsafe** | has `if (!names) { log(...); return; }`; naive inlining turns that `return` into a return from `continueInit`, **skipping the `loadState(finishContinueInit)` call right after it** — a real init-sequencing bug, not just a style issue |
| `checkTrack` | 1 | direct, bare statement in `runNextStep` — **not tail** | **no — unsafe** | 2 early returns; naive inlining would skip the trailing `log('Current mode...')` and `queueTask(handleDailyCheck)` lines that follow the call today |
| `applyComponentNames` | 1 | direct/tail, "Step 3/4" of the init step array | no (deferred) | tail position, technically safe — but 44+ lines including an inner named `processNext` function; the fixed heap saving from removing one function+binding does not scale with body size, so the code-motion risk isn't worth it in this pass |
| `solarSoftTargetReached` | 1 | direct, inside `if (solarSoftTargetReached() && SOLAR.availableW < ...)` | no (deferred) | calls `ensureRuntimeDay()` (side effect) before returning a bool; inlining into a `&&` short-circuit safely requires hoisting to a temp *guarded by the same short-circuit condition* — doable but safety-adjacent (solar hysteresis) and deferred for time |
| `solarHardCeilingReached` | 1 | direct, inside `if (CONFIG.solarEnabled && solarHardCeilingReached())` in `desiredOutput()` | no (deferred) | same side-effect/short-circuit reasoning as above |
| `runPendingCallback` | 1 | **by-ref**: `queueTask(runPendingCallback)` | **no — never** | hot path (every RPC-slot release); callback-by-reference, see "Method" above |
| `decrementOnlyCallDone` | 1 | **by-ref**: passed as the callback arg to `RAW_CALL(...)` inside the `Shelly.call` wrapper | **no — never** | hot path (every unwrapped `Shelly.call`) |
| `sharedCallDone` | 1 | **by-ref**: passed as the callback arg to `RAW_CALL(...)` inside the `Shelly.call` wrapper | **no — never** | hot path (every wrapped `Shelly.call`) |
| `onForecast` | 1 | **by-ref**: 3rd arg to `Shelly.call("HTTP.GET", ...)` | **no — never** | callback-by-reference |
| `onDeviceLocation` | 1 | **by-ref**: 3rd arg to `Shelly.call('Shelly.DetectLocation', ...)` | **no — never** | callback-by-reference |
| `onSolarAvailable` | 1 | **by-ref**: 2nd arg to `MQTT.subscribe(...)` | **no — never** | callback-by-reference; also protected by the MQTT.subscribe-stack-trap convention (must remain a stable function reference, last statement in its setup function) |
| `handleDailyCheck` | 2 (1 in-file `queueTask(handleDailyCheck)` + 1 Schedule-job string) | in-file `by-ref` **and** external, by name string, from the device scheduler | **no — never** | Schedule-invoked; per the issue, never inlinable regardless of in-file count |
| `handleMorningStart` | 0 in-file / Schedule-job string only | external only | **no — never** | Schedule-invoked by name string (`wrapScheduleCall('handleMorningStart()')`), confirmed at the `updateScheduleMode()` job-rewrite site |
| `handleEveningStop` | 0 in-file / Schedule-job string only | external only | **no — never** | same as above |
| `handleNightStart` | 0 in-file / Schedule-job string only | external only | **no — never** | same as above |
| `handleNightStop` | 0 in-file / Schedule-job string only | external only | **no — never** | same as above |

No function in the file has **zero** call sites — no dead code found by this pass.

The remaining ~65 functions in the file (not listed above) have 2+ genuine call sites and are
out of scope for single-call-site inlining by definition.

## Inlined this pass (13 functions)

`initConfig`, `shouldRefreshForecast`, `getMaxForecastTemp`, `ensureForecastUrl`,
`fetchAndCacheForecast`, `migrateLegacyRuntimeState`, `computeTurnoverToday`,
`turnOffOtherOutputsFailed`, `handleInputEvent`, `handleButtonEvent`, `turnOffOtherOutputs`,
`fuseRecord`, `verifySchedules`.

## Deferred, not inlined (with reason)

`loadStorageObject`, `loadStorageNumber`, `epochSecondsForDateString`,
`loadRuntimeStateFromStorage`, `loadRuntimeStateFromKVS`, `applyComponentNames`,
`solarSoftTargetReached`, `solarHardCeilingReached` — each is a genuine single-call-site
candidate but requires non-trivial control-flow restructuring (non-tail multi-return, or a
side effect inside a `&&` short-circuit) to inline correctly. `loadRuntimeStateFromKVS` is the
one exception that is *mechanically* safe (tail position, no early returns) but sits inside the
`#530`-flagged fragile stack-depth chain and was left alone on that basis alone.

## Flagged unsafe (would change behaviour if inlined naively)

`configureComponentNames`, `checkTrack` — both have an early `return` used as a bare
non-tail statement; naive inlining turns that `return` into a return from the *caller*,
silently skipping real code that runs today. Documented per-function above.

## Never inlinable regardless of call count

`runPendingCallback`, `decrementOnlyCallDone`, `sharedCallDone`, `onForecast`,
`onDeviceLocation`, `onSolarAvailable` — callback-by-reference on hot paths (see "Method").
`handleDailyCheck`, `handleMorningStart`, `handleEveningStop`, `handleNightStart`,
`handleNightStop` — invoked by the device's Schedule mechanism via a name string
(`script.eval` job code), entirely outside this file's own call graph.
