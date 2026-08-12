# Heap Proposal for `pool-pump.js` — closing issue #433

**Purpose**: a prioritised, specific change list that makes `internal/shelly/scripts/pool-pump.js`
fit the Shelly Pro1's JS heap **with the #405 solar path enabled**, which is what issue #433 blocks
on today.

**Method and rationale**: [`shelly-heap-allocation.md`](shelly-heap-allocation.md). Read at least
its §0 and §6 before acting on anything here — every estimate below rests on the allocation model
described there, and on a static→resident conversion ratio derived from a single measurement.

**This document is analysis only. No `.js` or Go source was changed.** Implementation is a separate,
reviewed task.

**Related**: #433 (this problem), #421 / PR #426 (the crash fix and the allocation-over-size lesson),
PR #430 (esbuild top-level mangling), #403 (the daemon aggregator publishing the solar payload),
#401 / #406 (the campaign this unblocks).

---

## 1. The bar to clear

Measured on the production Pro1 `filtration-hiver` (`shellypro1-ec62608c0230`, fw 2.0.0) with
`watchdog.js` resident and the production 24-key KVS config, per #433 and `AGENTS.md`:

| build | solar | minified | `mem_peak` | `mem_free` | result |
|---|---|---|---|---|---|
| v0.11.9 (no solar code at all) | n/a | 30409 | 17234 | 11956 | comfortable |
| `main` @ `ae4c5da` | disabled | 36155 | 22330 | 10276 | runs, ~700 B headroom |
| PR #426 (#421 fix) | disabled | 37739 | 21350 | 6398 | stable overnight |
| PR #426 | **enabled** | 37739 | 22274 **and climbing** | — | **`out_of_memory`** |
| PR #430 mangling, on `main` | disabled | 31010 | 21686 | 12040 | runs |

Total heap is **~23030 bytes**, shared device-wide.

(The v0.11.9 row is quoted from #433. `AGENTS.md` "JS Heap Budget" records the same build as
`mem_peak` 17136 / `mem_free` 12068 — a ~100 B discrepancy between two separate readings of the
same build, which is a useful reminder that a single reading has noise of that order. Treat
differences under ~200 B as not measured.)

**The gap.** Death occurs at ~23030. The solar build was last seen at 22274 and rising, so the
minimum saving is **≥800 B of peak** just to survive — but surviving at 22.5 KB is not a fix, it is
the same lottery #421 already lost twice. `AGENTS.md`'s rule of thumb is `mem_free` ≥ 5 KB at idle.

> **Target: `mem_peak` ≤ 20000 with solar enabled — a saving of ~2.5–3.5 KB.**

Note what the table already tells you: PR #426 has **higher** resident use than `main` (16632 vs
12754) yet a **lower** peak (21350 vs 22330). That is the `CALL_SLOTS` trade — +3878 resident bought
−980 peak — and it is the shape every recommendation below tries to copy.

---

## 2. What enabling solar actually adds

Per #433, setting `script/pool-pump/solar-enabled=true` adds, at
`internal/shelly/scripts/pool-pump.js`:

1. `MQTT.subscribe("myhome/energy/solar/available", onSolarAvailable)` — `:1554`
2. the retained `SOLAR` state object — `:1522`
3. a 30 s tick timer running `checkSolarHysteresis` — `:2293`
4. `JSON.parse(message)` on every message, ~60 s cadence — `:1534`
5. transitively, `computeFlowRate()` **twice per evaluation** via `solarHardCeilingReached()` and
   `solarSoftTargetReached()` — `:1561`, `:1570`

Item 5 is not listed in #433 and is the cheapest real allocation in the set.

---

## 3. Ranked proposal

Ranked by (estimated saving ÷ risk). "Peak effect" is the estimated movement in `mem_peak`, which is
the number that decides survival; where a change is primarily a residency reduction that happens to
sit inside the peak window, that is stated.

Estimation conventions, so the numbers can be argued with:
- Static byte savings are converted to resident at the **0.34** ratio measured in PR #430
  (−5145 minified → −1764 `mem_used`). Unique string-literal data may convert closer to 1:1; where
  that matters the range spans both models.
- Per-object costs assume a property cell of ~20–24 B and an object cell of ~8 B, typical of a
  32-bit NaN-boxed embedded engine. **Unverified** — see `shelly-heap-allocation.md` §8.

### P1 — Delete `description:` from `CONFIG_SCHEMA`; keep the text as `//` comments
**Rank 1. Largest single saving, essentially zero risk.**

- **Site**: `pool-pump.js:37–217`, the 28 entries of `CONFIG_SCHEMA`.
- **Mechanism**: 28 property cells plus ~1.5 KB of literal string data vanish from an object that is
  alive for the **entire 24-key async KVS load** — precisely the window in which `mem_peak` is set.
  Comments are stripped before upload, so the documentation still reaches every human reading the
  source and costs the device nothing.
- **Evidence it is dead weight**: the only `.description` access anywhere in
  `internal/shelly/scripts/` is `heater.js:418–421`, which *deletes* them
  (`CONFIG_SCHEMA[name].description = null;` — "Drop description strings now that config is loaded").
  Nothing ever reads one. `tools/extract-pool-defaults/main.go` regexes only `default:` out of the
  *source text*, never the runtime object, and nothing in `internal/`, `myhome/` or `pkg/`
  references the field.
- **Independent corroboration**: someone already found this lever and applied the weaker form of it
  in `heater.js`. Nulling after load is strictly dominated by deleting outright, because it still
  pays the full cost **during** the load — which on `pool-pump.js` is exactly the peak window — and
  still pays the bytecode cost forever. Deleting is also less code.
- **Estimate**: ~2.0 KB of minified source removed. Resident saving **−700 B** (conservative, at the
  0.34 ratio) to **−2300 B** (if descriptions materialise as owned property strings). Peak effect
  equals the resident saving here, because the object is live at peak. **Take −1200 B as the
  planning figure.**
- **Risk**: **very low.** No behavioural surface. Also applies to `garden.js` (−434 B of text) and
  `heater.js` (−394 B) for free.
- **Verdict**: **do it first.** Best ratio in the list by a wide margin.

### P2 — Stop `JSON.parse`ing switch-status payloads, and stop self-subscribing
**Rank 2. Largest recurring transient in the script, and it exists on `main` too.**

- **Sites**: `parseSwitchStatus` `:1636–1646`; `subscribePro1Status` `:1658–1667`;
  `subscribePro3Status` `:1688–1699`; the `topic.split(":")` in `onPro3StatusMessage` `:1675`.
- **Mechanism**: a Shelly `status/switch:N` payload is a full component status — `id`, `source`,
  `output`, `apower`, `voltage`, `current`, `aenergy{total, by_minute[3], minute_ts}`,
  `temperature{tC, tF}` — roughly 200–250 characters. `JSON.parse` materialises ~4 objects and
  ~15–18 property cells (~450–550 B) plus parser temporaries, on **every** message, **to read one
  boolean**. Replace with two `indexOf` probes (`shelly-heap-allocation.md` §5.2) which allocate
  nothing, and replace `split(":")` with `lastIndexOf`/`slice` (§5.4).
- **Multiplier**: `docs/pool-pump.md:387` documents that `ctl pool add` writes both `pro3-id` and
  `pro1-id` on every device, so **each device subscribes to 4 peer status topics including its
  own** — a Pro1 parses its own switch status plus 3 Pro3 channels. The self-subscription is pure
  waste on both device types and can simply be skipped when the peer id equals `STATE.myDeviceId`.
- **Estimate**: **−450 to −550 B of transient per message**, times ~4 subscriptions at roughly
  1 msg/min each (see §5 open questions — this cadence is assumed, not measured). Peak effect
  **−300 to −800 B**; take **−500 B**. Dropping the self-subscription additionally removes 1 of 4
  message streams outright.
- **Risk**: **low**, with two things to get right: (a) the substring probe must not be fooled by
  another field literally containing `"output":true` — no such field exists in a Shelly switch
  status, but assert it in the emulator test against captured real payloads; (b) the probe must
  return `null` on no match so the caller ignores the message, exactly as the `JSON.parse` version
  does on a parse failure. Fail closed, never guess.
- **Verdict**: **do it.** Note this benefits the solar-disabled build too, so it also buys margin
  back for PR #426 independently of #433.

### P3 — Remove the per-call object literal in `computeFlowRate()`
**Rank 3. Small but free, and it sits directly on the path #433 blames.**

- **Site**: `pool-pump.js:1924–1929`.
- **Mechanism**: `var speedRpms = {eco:…, mid:…, high:…, max:…}` allocates 1 object + 4 property
  cells (~100 B) on every call, purely to look up one number. Callers:
  `computeTurnoverToday` `:1053` (every runtime checkpoint, 60 s while running),
  `solarHardCeilingReached` `:1561` and `solarSoftTargetReached` `:1570` (both reachable on every
  solar tick *and* every solar message), `computeRunHours` `:1933`. With solar enabled and the pump
  running, that is up to 3 allocations per 30 s window. Replace with an `if`/`else` chain
  (`shelly-heap-allocation.md` §5.1).
- **Estimate**: **−100 to −300 B** of peak; take **−200 B**.
- **Risk**: **very low.** Pure refactor; `pool_pump_test.go` already exercises the run-hours and
  turnover arithmetic.
- **Verdict**: **do it.**

### P4 — Drop `sources` from the solar payload (daemon-side)
**Rank 4. Free and correct, but #433 over-rates it as "cheapest first".**

- **Site**: `myhome/daemon/solar_aggregate.go:37–41` (`SolarAvailablePayload.Sources`) and
  `:127–151` (`buildPayloadLocked`).
- **Mechanism**: the live payload measured in #433 is
  `{"available_w":60,"ts":1785908472,"sources":[{"name":"beem","watts":60,"stale":false}]}` — **87
  bytes**. Without `sources` it is `{"available_w":60,"ts":1785908472}` — **34 bytes**. The device
  therefore holds 53 fewer bytes of message string, and `JSON.parse` builds 2 fewer objects, 5 fewer
  property cells and 2 fewer owned key strings (~140 B). The struct's own doc comment already says
  `Sources` is "Not consumed by pool-pump.js today — informational only."
- **Estimate**: **−150 to −250 B of transient per message**; peak effect **−100 to −200 B**.
- **Risk**: **zero on the device** — a pure daemon change. The only cost is observability, fully
  recoverable by publishing `sources` on its own topic (e.g. `myhome/energy/solar/sources`,
  retained) so `mosquitto_sub` and the web UI keep working. Preferable to a config flag: no new
  option to document in four files.
- **Verdict**: **do it, and expect little.** #433 lists it first on cost, which is right — it is
  free. But it is **not** a fix on its own: it is at best a quarter of the minimum ≥800 B gap.
  Doing it and stopping there is the exact mistake #421 made when it trimmed 1519 bytes and still
  OOM'd.

### P5 — Parse only `available_w` and `ts`; no `JSON.parse` on the solar path
**Rank 5. Completes P4; together they make the solar message path allocate almost nothing.**

- **Site**: `onSolarAvailable` `:1531–1550`.
- **Mechanism**: the parsed object is read twice and dropped two statements later. Two `indexOf` +
  `slice` + `Number` extractions allocate only short strings; hoist the two search keys
  (`'"available_w":'`, `'"ts":'`) to module-level constants so even those are allocated once
  (`shelly-heap-allocation.md` §5.3).
- **Estimate**: with P4 also applied, **−70 to −120 B/message**; without P4, **−200 to −350
  B/message**. Peak effect **−100 to −200 B**; take **−150 B**.
- **Risk**: **low-medium.** The parser must tolerate integer and float formatting, key order,
  absent keys, and must **fail closed**: on a failed extraction leave `SOLAR.publishedTs` untouched
  so the existing staleness check at `:1586` takes over and the forecast schedule keeps running —
  which is exactly what the current `catch` path does. Mitigations: an emulator test over several
  payload shapes, plus a Go test in `myhome/daemon` asserting the marshalled shape the device parser
  expects, so the two cannot drift.
- **Verdict**: **do it, together with P4 and not before it** — P4 shrinks what P5 has to handle.

### P6 — Reuse a preallocated object for the parsed solar reading
**Verdict: do NOT do this. Recorded so it is not re-proposed.**

`SOLAR` already stores plain numbers (`availableW`, `lastMsgTs`, `publishedTs`, `aboveStartSince`,
`belowStopSince`) — **nothing parsed is retained**. Once P5 removes `JSON.parse` there is no parsed
object left to pool. Adding a pool here would add resident cost for zero transient saving, which is
the `CALL_SLOTS` trade run backwards. #433's suggestion 3 phrases this as "avoid retaining parsed
objects"; the script already does not.

### P7 — Reduce `log()`'s cost
**Rank 6 as code; rank 0 as an experiment.** Three separable pieces.

- **P7a — set `script/pool-pump/logging` to `false` on the Pro1 and re-measure.** One KVS key,
  instantly reversible, no code change. `log()` returns before building any string
  (`:511`), so all 154 call sites become nearly free. **This is the cheapest available experiment
  and should be run before writing any code**, because it isolates how much of the peak is
  log-string churn and therefore how much of P7b/P7c is worth doing.
  Estimate: unknown; plausibly **−500 to −2000 B**, dominated by the ~200 log calls during init,
  which is where the peak is set. Risk: **very low** (loses production visibility until re-enabled).
- **P7b — fix the three call sites that do work eagerly**: `:2378` and `:2381` call
  `JSON.stringify(info)` *before* `log()` can decide to discard it; `:884` does the same for
  `config`. Pass the object and let `log()` stringify it only when it will print
  (`shelly-heap-allocation.md` §5.5). Also the 8 call sites that concatenate with `+`.
  Estimate **−100 to −300 B**; risk **very low**.
- **P7c — rewrite `log()` to drop the `s +=` accumulator.** Each `+=` allocates a new immutable
  string; a `k`-argument call allocates ~`2k−1` intermediates totalling O(n²/2) bytes. `print()`
  accepts multiple arguments (already used at `:527`), so dispatching on `arguments.length` for the
  common 1–4 arity cases removes the accumulator entirely.
  Estimate **−100 to −400 B**; risk **medium** — it changes log formatting and separator behaviour,
  so gate it behind a test that pins the output of representative calls.

### P8 — Delete the unreachable `createSchedules()`, `clearNonUpdateSchedules()`, `loadValue()`
**Rank 7. Free residency, but verify the runbook first.**

- **Sites**: `:1715–1778`, `:1780–1867`, `:582–599`.
- **Mechanism**: none of the three is called from anywhere in `pool-pump.js`, and the pool's
  schedules are created **Go-side** by `internal/myhome/shelly/script/pool.go:391`. A repo-wide
  search finds external `Script.Eval("clearNonUpdateSchedules(function(){createSchedules(null)})")`
  only for **`garden.js`**, at `myhome/ctl/garden/setup.go:188` — nothing equivalent for the pool.
  ~3.4 KB raw / ~1.3 KB minified of bytecode that can never run.
- **Estimate**: **−300 to −500 B** resident at the 0.34 ratio; take **−350 B**. No transient effect.
- **Risk**: **low**, with one check: confirm no human runbook or `docs/` procedure invokes them by
  hand before deleting. If in doubt, delete `loadValue()` (unambiguously dead) and keep the other
  two pending confirmation.
- **Also worth checking locally, no device needed**: whether PR #430's esbuild IIFE wrapper already
  tree-shakes them, which would make this change free of source churn.

### P9 — Lengthen or condition the solar tick
**Rank 8. Small, safe, and reduces the chance of a bad coincidence.**

- **Site**: `:2292–2294`.
- **Mechanism**: the tick exists only so staleness is noticed when messages stop arriving. Staleness
  is judged against `CONFIG.solarStaleMs` (default **300000 ms**), so a 30 s tick is 10× finer than
  the decision it feeds. Raising it to 60–120 s cuts tick-driven allocation proportionally and eases
  the 5-timer budget.
- **Estimate**: **−50 to −150 B** of peak (mostly realised via P3); take **−80 B**.
- **Risk**: **very low.** Worst case a stale-solar stop is noticed up to 2 minutes later — well
  inside the 10-minute `solarStopDelayMs`.

### P10 — Remove per-tick closures from the task-queue path
**Rank 9 overall, but its first sub-item is rank ~1 by ratio.**

- **Sites**: `queueTask` `:448`, `processTaskQueue` `:429`, and ~20 `queueTask(function () { ... })`
  call sites. Recurring ones: `persistRuntimeState` `:1040`, `:1043` (two closures every 60 s while
  running); `saveState` `:937`, `:945`; `turnOffAllSwitchesNext` `:1113` and
  `turnOffOtherOutputsNext` `:1157` (one per switch per transition).
- **P10a — the free subset.** Roughly a dozen sites are `queueTask(function () { cb(); })`, where
  `cb` takes no arguments and `processTaskQueue` invokes tasks with no arguments: `:631`, `:637`,
  `:647`, `:657`, `:689`, `:697`, `:717`, `:720`, `:724`, `:730`, `:738`, `:1874`, `:1899`. These
  are `queueTask(cb)` with identical behaviour and no allocation.
  Estimate **−100 to −250 B**; risk **very low**. **Do this one early.**
- **P10b — the contract change.** Give `queueTask` an optional single argument stored in a parallel
  preallocated array, so recurring callers pass a named top-level function plus a value instead of
  a closure (`shelly-heap-allocation.md` §5.6). Note that `TASK_QUEUE` is only reset when it fully
  drains (`:436`), so during a burst **every** queued closure is alive simultaneously — this is the
  same shape of problem `CALL_SLOTS` solved on the RPC side.
  Estimate **−150 to −400 B**; risk **medium** — touches ~20 call sites and the queue contract,
  which PR #426 also modifies. Sequence it *after* PR #426 lands, behind that PR's emulator tests.

### P11 — Precompute the KVS key strings used on recurring paths
**Rank 10.**

- **Sites**: `storeValue` `:567–580`; callers `:938`, `:946`, `:1041`, `:1044`.
- **Mechanism**: every write concatenates `CONFIG_KEY_PREFIX + key` afresh and builds a
  `{key, value}` params object plus a `String(value)`. `persistRuntimeState` does this twice every
  60 s while the pump runs, forever. Hoist the full key strings for `runtime-sec`,
  `turnover-today`, `active-output` and `schedule-mode` to module-level `var`s built once at load.
- **Estimate**: **−100 to −250 B**; take **−150 B**.
- **Risk**: **low** for the key hoisting. Reusing a single preallocated `params` object would remove
  more, but depends on the firmware serialising `params` synchronously at call time — **unverified**
  (see §5.4 below). Do not ship that half without the experiment.

---

## 4. Is #433 achievable by allocation work alone?

### 4.1 The arithmetic

| item | planning estimate | risk |
|---|---:|---|
| P1 — drop `description:` from `CONFIG_SCHEMA` | −1200 B | very low |
| P2 — no `JSON.parse` of switch status; no self-subscription | −500 B | low |
| P3 — `computeFlowRate` literal | −200 B | very low |
| P4 — drop `sources` (daemon-side) | −150 B | zero (device) |
| P5 — no `JSON.parse` on solar path | −150 B | low-medium |
| P7b — eager work at `log()` call sites | −200 B | very low |
| P8 — delete unreachable functions | −350 B | low |
| P9 — 30 s → 60–120 s solar tick | −80 B | very low |
| P10a — `queueTask(cb)` trampolines | −175 B | very low |
| P10b — `queueTask` argument passing | −275 B | medium |
| P11 — hoist recurring KVS key strings | −150 B | low |
| **subtotal, code changes only** | **≈ −3430 B** | |
| P7a — logging off in production (config, not code) | −800 B? | very low |
| PR #430 mangling on the #426 build (37739 → 31318) | ≈ −800 B peak, ≈ −2200 B resident | medium (unverified on device) |

Plausible range on the code-only subtotal: **−2.0 KB (pessimistic) to −5.5 KB (optimistic)**.

### 4.2 Verdict

**Yes — #433 looks achievable by allocation and residency work alone, but only if most of the list
lands, and the margin is not comfortable.** The planning subtotal (~3.4 KB) is about equal to the
target saving (~2.5–3.5 KB), which means the pessimistic case falls short.

Three honest caveats:

1. **The largest single item, P1, is a residency reduction, not an allocation one.** The guide's own
   thesis says allocation has the better leverage — and P2, P3, P5, P9, P10 and P11 are all
   allocation work — but the biggest single number here comes from deleting 1.5 KB of documentation
   strings that happen to be alive during the peak window. Both levers are needed.
2. **The estimates rest on an unverified engine model.** Every per-object figure assumes property
   cells of ~20–24 B and objects of ~8 B. If the engine turns out to store literal strings as
   pointers into bytecode rather than owned heap strings, P1 shrinks toward its −700 B floor and the
   subtotal drops to ~2.9 KB. `shelly-heap-allocation.md` §8.1 and §8.2 settle this with two
   uploads.
3. **Measure each item.** #421 burned two iterations on plausible-but-worthless changes. Land these
   in the order given, one measurement per item, appending rows to #433's table. If the running
   total reaches ≤20000 with solar on, stop — the rest is not needed.

**Recommended sequence**: P7a (experiment, no code) → P1 → P10a → P3 → P7b → P2 → P8 → P4 → P5 →
P9 → P11 → P10b. Re-measure after each. Stop when `mem_peak` ≤ 20000 with solar enabled.

### 4.3 If measurement falls short — structural options

**S1 (recommended fallback): publish a *decision*, not a *measurement*.**

Move the hysteresis into the daemon, which already owns the Beem poll, the thresholds and every
timer it needs, and have the aggregator publish a retained, **JSON-free** payload on a new topic —
e.g. `myhome/energy/solar/pump` carrying `1,1785908472` (decision, unix seconds). The device side
collapses to roughly:

```js
function onSolarPump(topic, message) {
  var c = message.indexOf(",");
  if (c < 1) return;
  var ts = Number(message.slice(c + 1)) * 1000;
  if (Date.now() - ts > CONFIG.solarStaleMs) return;   // stale: schedule keeps running untouched
  if (message.charAt(0) === "1") doStart(CONFIG.preferredSpeed, "Solar");
  else doStop("Solar");
}
```

What this removes from the device: the `SOLAR` object, `checkSolarHysteresis`,
`solarHardCeilingReached`, `solarSoftTargetReached`, the 30 s tick timer, the `JSON.parse`, and
**six of the 28 KVS config keys** (`solar-start-w`, `solar-stop-w`, `solar-start-delay`,
`solar-stop-delay`, `solar-min-turnover`, `solar-max-turnover`) — which also shrinks `CONFIG_SCHEMA`
and, more importantly, shortens the boot-time KVS load burst that sets the peak. Estimated total:
**the whole ≥800 B solar delta, plus whatever six fewer config keys are worth** (the 5→24-key
measurement implies ~400 B/key, though that figure predates the `CALL_SLOTS`/`userdata` rework and
is probably much smaller now — see `shelly-heap-allocation.md` §8.6).

The daemon needs `runtimeTodaySec` to evaluate the turnover targets — it already has it: the script
mirrors `runtime-sec` and `turnover-today` to KVS for exactly this reason (`:1041`, `:1044`, and
`myhome/daemon/pool_notices.go`).

**Degraded mode**, which `CLAUDE.md` requires to be stated explicitly in the PR: if the daemon dies,
the retained decision goes stale, the device ignores it, and the **entirely on-device**
forecast-driven schedule continues unchanged. That is *identical* to today's behaviour — the current
`checkSolarHysteresis` already returns early on staleness (`:1586–1590`). No local capability is
lost. What moves off-device is only the solar *overlay*, whose input (the Beem reading) comes from
the daemon in the first place and is unavailable without it either way.

This does partially walk back #405's stated intent of putting the hysteresis on the device, so it
needs the maintainer's decision, not an implementer's.

**S2: split the solar path into a second script — do NOT do this.**

The heap is one device-wide pool; that is the premise of #433 itself. A second script adds a second
VM context, its own bytecode, its own copies of `log`, `storeValue`, KVS access and config loading,
and would need IPC (KVS or MQTT loopback) to reach `doStart`/`doStop`. It **increases** total heap
use. Recorded here explicitly so it is not proposed again.

The one variant that does work is the inverse — move something *else* off the Pro1, e.g. stop
`watchdog.js` (measured at 2156 B on a Plus 1, quoted as 3554 B in PR #426). That is PR #426's
option 1, it is a production behaviour change, and it trades pump *reliability* (watchdog handles
MQTT-failure reboots and firmware updates) for pump *features*. Not recommended.

**S3: shrink the device's config surface generally.**

28 KVS keys is a lot, and the 5→24-key measurement makes keys the most expensive unit of function on
this hardware. Several are effectively installation constants: `eco-rpm`, `mid-rpm`, `high-rpm`,
`max-rpm`, `max-flow-rate`, `pool-volume`, `max-temp`. Packing them into one key
(`pump-spec = "31,2900,2000,2600,2900,46,35"`, parsed once at load with `indexOf`/`slice`) turns 7
round-trips into 1. Estimated **−6 KVS round-trips of load-burst garbage**; risk **medium** — CLI,
`docs/configuration.md`, `myhome-example.yaml` and the generated defaults all change, and it makes
the config less legible via `KVS.GetMany`. Only worth it if P1–P11 and S1 both fall short.

---

## 5. What could not be verified without hardware

No device access was available while producing this document. In addition to the general list in
`shelly-heap-allocation.md` §8, these are specific to this proposal:

### 5.1 Every per-item byte estimate
**Claim**: the figures in §3 and §4.1.
**Confidence**: order-of-magnitude only. They rest on assumed cell sizes and on a single measured
static→resident ratio (0.34).
**Experiment**: the sequence in §4.2 — one change per upload, `Script.Stop`/`Start`, record
`{mem_used, mem_peak, mem_free}` and minified size, append to #433's table. This is the only thing
that turns these estimates into facts.

### 5.2 P1's size — do the `description:` strings actually occupy heap?
**Claim**: −700 B (conservative) to −2300 B (if owned strings).
**Experiment**: `shelly-heap-allocation.md` §8.2 — upload a control variant with 4 KB of unreferenced
string-literal padding and diff `mem_used`. Settles the whole 0.34-vs-1.0 conversion question, which
moves the §4.1 subtotal by ~1.6 KB.

### 5.3 P2's multiplier — status message size and cadence
**Claim**: ~200–250 bytes per `status/switch:N` payload, roughly one per minute per channel, times
4 subscriptions.
**Experiment**: `mosquitto_sub -t 'shellypro1-ec62608c0230/status/switch:0' -v` (and the three Pro3
channels) for five minutes; count messages and bytes. Broker-only, no device write. If the real
cadence is once per *state change* rather than once per minute, P2 drops several ranks and P1
becomes the only item that matters.

### 5.4 P11's second half — can one `params` object be reused across `Shelly.call`s?
**Claim**: the firmware serialises `params` synchronously at call time.
**Experiment**: two back-to-back `KVS.Set` calls reusing one mutated params object with different
key/value pairs; confirm both keys land correctly. Do not ship the reuse without this.

### 5.5 Whether the peak is set during init at all
**Claim**: `mem_peak` for `pool-pump.js` is set during the boot sequence (24-key KVS load + 4-step
`initSteps` + `applyComponentNames` + `verifySchedules`), which is why P1's peak-window argument
works and why P7a is expected to be large.
**Evidence for**: `main` peaks at 22330 while idling at `mem_used` 12754 — a ~9.6 KB gap that has to
come from a burst, and init is the largest one. But #433 reports the solar build's peak "climbing"
*after* boot, which suggests at least one later contributor.
**Experiment**: poll `Script.GetStatus` every 2 s from `Script.Start` for the first 60 s and plot
`mem_peak`. If it saturates during init, target init; if it keeps climbing hours later, there is a
leak or a slow accumulation that none of P1–P11 addresses, and that becomes the priority instead.
**This experiment should be run before the change sequence, alongside P7a.** It is the cheapest way
to discover that the whole ranking above is aimed at the wrong window.
