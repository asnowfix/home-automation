# Reducing JS Heap Use in Shelly Device Scripts

A practical guide to fitting scripts into a Shelly Gen2 device's JavaScript heap, grounded in this
repo's own hardware measurements (issues #421, #429, #433; PRs #426, #430).

**Companion document**: [`433-pool-pump-heap-proposal.md`](433-pool-pump-heap-proposal.md) — the
concrete, prioritised change list for `pool-pump.js` that this guide's method produced.

**Prerequisites**: `AGENTS.md` sections "JS Heap Budget", "Measuring one script's footprint
(differential method)", "Testing memory on a spare device", "Resource Limits", "Resource Limit
Workarounds", "Data Storage Patterns"; `CLAUDE.md` "Shelly JavaScript" for the ES5/engine
constraints every snippet here obeys.

---

## 0. The one-paragraph version

A Shelly Gen2 device has **one JavaScript heap of ~23030 bytes shared by every script on it**
(measured identically on a Pro1 at fw 2.0.0 and a Plus 1 at fw 1.7.5). What kills a script is not
how much memory it holds steadily — it is **`mem_peak`**, the high-water mark of live data *plus*
uncollected garbage. Two measurements in this repo say the same thing: trimming **1519** minified
bytes moved the peak by **zero**, while replacing **one** per-call closure with a fixed pool moved
it by **~1050 bytes** and turned an `out_of_memory` into a working script. Therefore: **hunt
allocation, not bytes.** Preallocate at load time and mutate in place; that trade is explicitly
sanctioned in this repo, and PR #426 measured it converting **+3878 bytes of resident** footprint
into **−980 bytes of peak** — a straight win, because only the peak decides survival.

---

## 1. Where the heap actually goes

Three tenants share the ~23 KB. They behave completely differently, and conflating them is the
single most common reason a size-reduction effort produces no result.

### 1.1 Code residency (bytecode + literal data)

The engine compiles your script to bytecode and keeps it resident for the script's lifetime, along
with the string literals it references. This scales with **minified** source size — comments and
whitespace are stripped before upload and cost nothing on-device.

The conversion rate is poor. From PR #430's esbuild top-level mangling on `pool-pump.js` at `main`:

| | minified | `mem_peak` | `mem_free` | implied `mem_used` |
|---|---|---|---|---|
| tdewolff (current) | 36155 | 22330 | 10276 | 12754 |
| esbuild + top-level mangle | 31010 | 21686 | 12040 | 10990 |
| **delta** | **−5145** | **−644** | **+1764** | **−1764** |

(`mem_used` is derived: `mem_used + mem_free` reads ~23030 at any instant, confirmed by a raw
reading of `{"mem_used":16632,"mem_peak":21350,"mem_free":6398}` in `AGENTS.md`.)

So **~5.1 KB of source bought ~1.8 KB of resident and only ~0.6 KB of peak** — a static→resident
conversion ratio of roughly **0.34**, and a static→peak ratio of roughly **0.13**. Use 0.34 as the
default multiplier when estimating what a size cut is worth, and expect the peak effect to be
about a quarter of that again.

Caveat worth knowing: mangling shortens *identifiers*, which the engine may store once and
reference repeatedly. **Unique literal data — long string constants — plausibly converts closer to
1:1**, so removing 2 KB of string literals is likely worth more than removing 2 KB of identifiers.
This is inferred, not measured; §7 gives the experiment that settles it.

### 1.2 Retained state (the live set)

Every object, array, string and closure your script still holds a reference to. This is what
`mem_used` measures at an idle moment. It is the only tenant you can reason about by reading the
code alone.

Two non-obvious contributors dominate here in practice:

- **Configuration schemas.** `pool-pump.js`'s `CONFIG_SCHEMA` is 28 entries × 4–6 properties, plus
  ~1.5 KB of `description:` strings that **no code ever reads** (verified: no `.description` access
  in any script; `tools/extract-pool-defaults` regexes only `default:` out of the *source*). It is
  alive for the entire duration of the async KVS load — i.e. exactly during the peak window — and
  only then set to `null`.
- **KVS key count.** Measured on a Plus 1 running identical code: **5 keys → `mem_used` 13748; 24
  keys → `out_of_memory`. ~7.9 KB of footprint was config-driven, not code-driven** (AGENTS.md
  "Testing memory on a spare device"). 19 extra `CONFIG` properties and their value strings account
  for maybe 600 bytes. The other ~7.3 KB is discussed in §1.4 — and it is the single most important
  number in this document.

### 1.3 Transient allocation (the garbage)

Everything created and dropped inside a function: object literals, string concatenations, parsed
JSON, closures passed to `Shelly.call`, arrays returned by `split()`. In a desktop JS engine this is
free. Here it is not, for one reason: **garbage is not collected between statements.** The engine
collects at safe points, and an async burst — 24 sequential `KVS.Get` round-trips, a 4-step init
chain, a task queue draining 10 entries — can allocate a great deal before reaching one.

`mem_peak` is therefore approximately:

```
mem_peak  ≈  max over all time of ( live set  +  garbage allocated since the last collection )
```

That formula is the whole guide. It explains why size cuts underperform (they shrink only the first
term, weakly) and why removing one per-call allocation overperforms (it shrinks the second term by
the *burst length* times the per-item cost).

### 1.4 The arena probably never shrinks

The 7.9 KB figure in §1.2 cannot be explained by 19 extra retained config values. The most likely
explanation is that the engine's allocation arena **grows on demand and never gives memory back**.
Under that model a transient burst does not merely risk a momentary `out_of_memory` — it
**permanently raises the script's resident floor** for the rest of its life.

If that is right, then `mem_used` is itself a high-water-mark-shaped quantity, transient allocation
is even more expensive than the naive reading suggests, and "it settles down after boot" is false
comfort. This is the highest-value open question in this document. §7.3 gives the experiment.

**Status: inferred from the 5-vs-24-key measurement. Not verified. Do not cite as fact.**

---

## 2. Allocation sources, ranked by cost

Ranked by observed or estimated cost *per occurrence × plausible frequency* in this repo's scripts.

### 2.1 A closure created per call or per tick — worst by a wide margin

A function value created at runtime is not just an object. It **captures its enclosing scope**,
which pins that entire call frame — every local variable in it — alive for as long as the closure
lives, and transitively pins the frames above it in the scope chain.

This is the #421 finding, and it is the strongest evidence in the repo:

> The original #421 fix wrapped every `Shelly.call` in a brand-new closure — one function object
> per call, purely to decrement a counter. On a device already ~700 bytes from its ceiling, this
> was the difference between `out_of_memory` and a working script. Replacing it with `CALL_SLOTS`
> moved `mem_peak` **~1050 bytes below `main`'s own peak**.

And the corroborating one: `main`'s `loadConfig` creates a fresh anonymous `KVS.Get` callback on
every iteration of a 24-key sequential load. Each captures `key`, `schema`, `kvsKey` and, through
the scope chain, `loadConfig`'s own frame. With prompt collection the difference between 5 and 24
keys would be near zero. It was 7.9 KB.

### 2.2 Object and array literals rebuilt per invocation

```js
// pool-pump.js:1924 — allocates 1 object + 4 properties on EVERY call,
// to look up exactly one number.
function computeFlowRate() {
  var speedRpms = {eco: CONFIG.ecoRpm, mid: CONFIG.midRpm, high: CONFIG.highRpm, max: CONFIG.highRpm};
  var rpm = speedRpms[CONFIG.preferredSpeed];
  ...
}
```

`computeFlowRate` is reached from the solar tick (every 30 s), from both solar target checks, and
from every runtime checkpoint (every 60 s while the pump runs). The lookup table is rebuilt each
time and discarded immediately.

### 2.3 `JSON.parse` of a payload you barely use

Parsing materialises the **whole** payload as an object graph — one object cell plus one property
cell per key, recursively — and then you read one field and drop it all.

The worst instance in this repo is not the one #433 names. `parseSwitchStatus` (`pool-pump.js:1636`)
`JSON.parse`s a full Shelly `status/switch:N` payload — `id`, `source`, `output`, `apower`,
`voltage`, `current`, `aenergy{total, by_minute[3], minute_ts}`, `temperature{tC, tF}` — in order to
read **one boolean**. And per `docs/pool-pump.md:387`, every device subscribes to **four** peer
status topics, *including its own* (a documented redundancy). See §7.5 for the measurement that
would size this precisely.

### 2.4 String concatenation, especially in a loop

Strings are immutable. `s += x` allocates a brand-new string of the combined length and abandons the
old one. A `k`-argument logging call therefore allocates roughly `2k−1` intermediate strings whose
total size is **O(n²/2)** in the final length.

```js
// The accumulator pattern in every script's log() — pool-pump.js:510, garden.js:217, watchdog.js:31
function log() {
  if (!CONFIG.enableLogging) return;
  var s = "";
  for (var i = 0; i < arguments.length; i++) {
    ...
    s += String(a);          // new string every iteration
    if (i + 1 < arguments.length) s += " ";   // and again
  }
  print(SCRIPT_PREFIX, s);
}
```

Note that the early return protects the accumulator but **not the call site**. Anything you compute
as an argument is computed whether or not logging is on:

```js
log("Unhandled component event:", JSON.stringify(info));   // pool-pump.js:2378 — always stringifies
```

### 2.5 Callbacks captured in `Shelly.call`

Every `Shelly.call(m, p, function (res, err) { ... })` allocates a closure that stays alive for the
whole RPC round-trip — which on this hardware is long. With up to 5 concurrent calls, up to 5
closures and 5 pinned frames coexist. The fix is §3.2's `userdata` pattern.

The `params` object is a second, unavoidable-looking allocation per call: `{key: PREFIX + k, value:
String(v)}` is one object, two properties, and two fresh strings, every write.

### 2.6 Timer callbacks and event-handler closures

A timer callback registered once at load costs once. A timer *created per operation* costs per
operation — and the 5-timer limit means you cannot do that anyway. The single-recurring-timer +
task-queue pattern in `AGENTS.md` is already the right shape; the residual cost is that **every
`queueTask(function () { ... })` call site allocates a closure**, and `pool-pump.js` has ~20 of
them, several on recurring paths.

### 2.7 `split()`, `slice()`, and friends

`topic.split(":")` (`pool-pump.js:1675`, per MQTT message) allocates an array plus one string per
piece. `lastIndexOf(':')` + `slice()` allocates one short string. Cheap to fix, small win, no risk.

---

## 3. Static/startup allocation is the preferred technique

**Allocate once at load, then mutate in place.** This is explicitly allowed here, and the numbers
justify it: PR #426's `CALL_SLOTS` traded **+3878 bytes of resident footprint for −980 bytes of
peak** and turned a dead script into a live one. Resident is a fixed, predictable cost you pay once.
Peak is a lottery you lose exactly once, at the worst possible moment, in production.

### 3.1 The worked example — `CALL_SLOTS`

From `fix/421-pool-pump-crash-catchup-and-heap:internal/shelly/scripts/pool-pump.js`, lightly
abridged. Read it as a template, not as pool-pump-specific code.

```js
// Allocated ONCE at load. Sized to the device's real 5-call ceiling + 1 spare,
// because calls fired straight from event/timer handlers bypass the task queue's throttle.
var CALL_SLOTS = [];
for (var CALL_SLOT_INIT_I = 0; CALL_SLOT_INIT_I < 6; CALL_SLOT_INIT_I++) {
  CALL_SLOTS.push({cb: null, ud: null, used: false});
}

function acquireCallSlot(cb, ud) {
  for (var i = 0; i < CALL_SLOTS.length; i++) {
    if (!CALL_SLOTS[i].used) {
      CALL_SLOTS[i].used = true;
      CALL_SLOTS[i].cb = cb;
      CALL_SLOTS[i].ud = ud;
      return CALL_SLOTS[i];
    }
  }
  // Never drop a callback: fall back to a fresh record if the pool is somehow exhausted.
  return {cb: cb, ud: ud, used: true};
}

// ONE completion handler for the whole script. Not one per call.
function sharedCallDone(result, error_code, error_message, slot) {
  CALLS_IN_FLIGHT--;
  var cb = slot ? slot.cb : null;
  var ud = slot ? slot.ud : null;
  if (slot) { slot.used = false; slot.cb = null; slot.ud = null; }
  if (typeof cb === 'function') { cb(result, error_code, error_message, ud); }
}

// Fire-and-forget calls need no slot at all — this path allocates nothing per call.
function decrementOnlyCallDone() { CALLS_IN_FLIGHT--; }

var RAW_CALL = Shelly.call;
Shelly.call = function (method, params, callback, userdata) {
  CALLS_IN_FLIGHT++;
  N_SEEN++;
  if (typeof callback !== 'function') {
    return RAW_CALL(method, params, decrementOnlyCallDone, null);
  }
  return RAW_CALL(method, params, sharedCallDone, acquireCallSlot(callback, userdata));
};
```

Three properties make this work, and any pool you build should have all three:

1. **Fixed size, allocated at load.** The loop runs once, at script start, when the heap is at its
   emptiest and no other burst is in flight.
2. **Claimed in place.** `acquireCallSlot` mutates an existing record; it does not build one.
3. **Always released, on every path.** `sharedCallDone` decrements and frees the slot *before*
   invoking the user callback, so a throwing callback cannot leak a slot.

And one safety property worth copying verbatim: the fallback in `acquireCallSlot` **allocates rather
than drops**. A memory optimisation must never be able to lose work.

### 3.2 `userdata` — passing state without a closure

`Shelly.call`'s fourth argument is handed back to the completion handler untouched. That is enough
to replace almost every per-call closure with one shared named handler:

```js
// BEFORE — one fresh closure per key, capturing key/schema/kvsKey and, via the
// scope chain, the entire enclosing loadConfig frame. 24 of these on a real boot.
function loadNextKey() {
  var key = configKeys[keyIndex];
  var schema = CONFIG_SCHEMA[key];
  var kvsKey = CONFIG_KEY_PREFIX + schema.key;
  keyIndex++;
  Shelly.call("KVS.Get", {key: kvsKey}, function (result, err) {
    /* ...uses key, schema, kvsKey... */
    queueTask(loadNextKey);
  });
}

// AFTER — one handler for the whole load, defined once at top level.
// Per-key context travels as userdata: a string that already exists, not a new allocation.
var CONFIG_LOAD_KEYS = null;
var CONFIG_LOAD_INDEX = 0;

function loadNextConfigKey() {
  if (CONFIG_LOAD_INDEX >= CONFIG_LOAD_KEYS.length) { finishLoadConfig(); return; }
  var key = CONFIG_LOAD_KEYS[CONFIG_LOAD_INDEX];
  CONFIG_LOAD_INDEX++;
  var schema = CONFIG_SCHEMA[key];
  Shelly.call("KVS.Get", {key: CONFIG_KEY_PREFIX + schema.key}, onConfigKVSResult, key);
}

function onConfigKVSResult(result, err, errMsg, key) {
  var schema = CONFIG_SCHEMA[key];   // re-derive rather than capture
  /* ... */
  queueTask(loadNextConfigKey);      // a named function, not a closure
}
```

Note the two secondary wins: the loop state moved to module-level `var`s (so there is no enclosing
frame left to pin), and `queueTask` now receives a **named top-level function** instead of a fresh
closure.

**Prefer passing a key and re-deriving over passing a bundle.** `onConfigKVSResult` receives one
string and looks the schema back up. Passing `{key: k, schema: s, kvsKey: x}` would have
reintroduced a per-call object.

### 3.3 Free memory explicitly when a phase ends

`pool-pump.js` already does this and it is worth imitating:

```js
CONFIG_SCHEMA = null;    // after the KVS load completes — never needed again
COMPONENT_NAMES = null;  // after component names are applied
```

Extend it to phase-scoped working state:

```js
var doneCb = CONFIG_LOAD_DONE;
CONFIG_LOAD_KEYS = null;
CONFIG_LOAD_MISSING = null;
CONFIG_LOAD_DONE = null;
doneCb(true);
```

---

## 4. Retained-state reduction

- **Never duplicate `CONFIG` into `STATE`.** Read `CONFIG.x` where you need it. A mirrored value is
  a property cell, a string, and a correctness hazard.
- **Prefer numeric codes to retained strings.** `STATE.scheduleMode` holds `"summer"`/`"winter"`
  forever and is compared by value in five places; `0`/`1` with a `MODE_SUMMER = 1` constant is a
  packed number. Small individually, but this pattern repeats.
- **Drop anything recomputable.** `pool-pump.js` already does the hard version of this well: it
  keeps `STATE.maxForecastTemp` and discards the 24-element hourly array
  (`docs/pool-pump.md` "Weather Forecast (Memory-Optimized)"). Look for the same shape elsewhere.
- **Delete documentation strings from runtime objects.** A `description:` field in a config schema
  is read by humans reading the *source*. Make it a `//` comment: the minifier strips comments, so
  the documentation reaches the reader and costs the device nothing. ~1.5 KB in `pool-pump.js`,
  ~0.4 KB each in `garden.js` and `heater.js`. `heater.js:418–421` already does the weaker version
  of this — `CONFIG_SCHEMA[name].description = null;` after the load — which is worth copying if you
  must keep the field, but deleting it outright is strictly better: nulling still pays the full cost
  *during* the load (the peak window) and still pays the bytecode cost forever.
- **Treat every KVS key as expensive.** The 5→24-key measurement (§1.2) makes config keys the most
  expensive unit of function on this hardware. Pack related constants into one key
  (`"31,2900,2000,2600,2900"`, parsed once) rather than adding a key per knob, and mark keys the
  script never reads at runtime `cliOnly: true` so the loader skips them entirely — `pool-pump.js`
  already does this for `mqtt-topic` and `night-duration`.
- **Delete dead code.** It is pure resident cost with zero benefit. In `pool-pump.js`,
  `createSchedules()`, `clearNonUpdateSchedules()` and `loadValue()` are unreachable — the pool's
  schedules are created Go-side by `internal/myhome/shelly/script/pool.go:391`. (`garden.js` is
  different: `myhome/ctl/garden/setup.go:188` really does `Script.Eval` its copies. Check before
  deleting.)

---

## 5. Anti-patterns, with before/after

All snippets are ES5/Espruino-legal: `var` only, no arrow functions, every function defined before
it is referenced, and no empty `catch (e) {}` (which becomes a syntax error after minification).

### 5.1 Rebuilt lookup table

```js
// BEFORE — 1 object + 4 properties per call
function computeFlowRate() {
  var speedRpms = {eco: CONFIG.ecoRpm, mid: CONFIG.midRpm, high: CONFIG.highRpm, max: CONFIG.highRpm};
  var rpm = speedRpms[CONFIG.preferredSpeed];
  if (!rpm) rpm = CONFIG.highRpm;
  return CONFIG.maxFlowRate * (rpm / CONFIG.maxRpm);
}

// AFTER — allocates nothing
function speedToRpm(speed) {
  if (speed === "eco") return CONFIG.ecoRpm;
  if (speed === "mid") return CONFIG.midRpm;
  return CONFIG.highRpm;              // "high", "max", and any unknown value
}

function computeFlowRate() {
  return CONFIG.maxFlowRate * (speedToRpm(CONFIG.preferredSpeed) / CONFIG.maxRpm);
}
```

If the table is genuinely constant, hoist it to a module-level `var` built once at load instead —
that is the §3 trade, and it is fine.

### 5.2 `JSON.parse` for one field

```js
// BEFORE — materialises the entire status payload to read one boolean
function parseSwitchStatus(message) {
  var data = null;
  try {
    data = JSON.parse(message);
  } catch (e) {
    if (e && false) {}
    return null;
  }
  if (!data || !("output" in data)) return null;
  return data.output;
}

// AFTER — allocates nothing; returns null when the field is absent, exactly as before
function parseSwitchStatus(message) {
  if (message.indexOf('"output":true') !== -1) return true;
  if (message.indexOf('"output":false') !== -1) return false;
  return null;
}
```

**Only do this when you control or can pin the payload format.** Write a test that feeds real
captured payloads through both versions and asserts identical results, and — where the producer is
in this repo — a Go test asserting the marshalled shape the device parser expects. Fail closed: on
no match, return `null` and let the caller ignore the message, never guess a default.

### 5.3 Substring extraction instead of a full parse

```js
// Extract one numeric field without building an object graph.
// Returns NaN when absent — callers must check, and must not act on NaN.
function numField(message, key) {
  var k = '"' + key + '":';
  var i = message.indexOf(k);
  if (i === -1) return NaN;
  i = i + k.length;
  var j = i;
  while (j < message.length) {
    var c = message.charAt(j);
    if (c === "," || c === "}" || c === "]") break;
    j++;
  }
  return Number(message.slice(i, j));
}
```

The `'"' + key + '":'` concatenation is itself an allocation. On a hot path, hoist the search
strings to module-level constants built once at load:

```js
var K_AVAILABLE_W = '"available_w":';
var K_TS = '"ts":';
```

### 5.4 Topic parsing

```js
// BEFORE — allocates an array plus one string per segment, per message
var parts = topic.split(":");
if (parts.length >= 2) {
  var n = Number(parts[parts.length - 1]);
  if (!isNaN(n)) id = n;
}

// AFTER — one short string
var c = topic.lastIndexOf(":");
if (c !== -1) {
  var n = Number(topic.slice(c + 1));
  if (!isNaN(n)) id = n;
}
```

### 5.5 Work done for a log line that is discarded

```js
// BEFORE — JSON.stringify runs even when logging is disabled
log("Unhandled component event:", JSON.stringify(info));

// AFTER — log() already stringifies object arguments, and only when it will print
log("Unhandled component event:", info);
```

Same for concatenation at the call site:

```js
log("pro3 switch:" + id + " state updated via MQTT:", on);   // BEFORE: 2 strings, always
log("pro3 switch:", id, "state updated via MQTT:", on);      // AFTER: 0 strings when disabled
```

### 5.6 A closure per queued task

```js
// BEFORE — a fresh closure per queued item; several of these fire every 60 s
queueTask(function () { storeValue("runtime-sec", Math.round(sec)); });
queueTask(function () { storeValue("turnover-today", computeTurnoverToday(sec)); });

// AFTER — extend queueTask to carry one argument, and pass named top-level functions.
// (This changes the queue contract; do it once, with tests, not ad hoc.)
var TASK_ARGS = [];

function queueTask(task, arg) {
  TASK_QUEUE.push(task);
  TASK_ARGS.push(arg);
  if (!TASK_TIMER) { TASK_TIMER = Timer.set(200, true, processTaskQueue); }
}

function storeRuntimeSec(sec) { storeValue("runtime-sec", Math.round(sec)); }
function storeTurnover(sec)   { storeValue("turnover-today", computeTurnoverToday(sec)); }

queueTask(storeRuntimeSec, sec);
queueTask(storeTurnover, sec);
```

`processTaskQueue` then calls `task(TASK_ARGS[TASK_INDEX])` and must clear both arrays together on
drain. Keep `AGENTS.md`'s manual-shift rule in mind: `[].shift()` is unavailable, and the existing
index-based drain is already the right shape.

### 5.7 The free subset of 5.6 — trampolines that wrap nothing

```js
if (typeof cb === 'function') queueTask(function () { cb(); });   // BEFORE
if (typeof cb === 'function') queueTask(cb);                      // AFTER — identical behaviour
```

`processTaskQueue` invokes tasks with no arguments, so the wrapper is pure overhead. There are
roughly a dozen of these in `pool-pump.js`'s forecast and schedule paths. Zero-risk, do it first.

### 5.8 Recomputed key strings

```js
// BEFORE — a new concatenated string on every write, every 60 s, forever
function storeValue(key, value) {
  Shelly.call("KVS.Set", {key: CONFIG_KEY_PREFIX + key, value: valueStr});
}

// AFTER — hoist the full keys used on recurring paths, once at load
var KEY_RUNTIME_SEC = CONFIG_KEY_PREFIX + "runtime-sec";
var KEY_TURNOVER    = CONFIG_KEY_PREFIX + "turnover-today";
```

The `{key: ..., value: ...}` params object remains. Reusing one preallocated params object across
calls **would** remove it, but only if the firmware serialises `params` synchronously at call time.
That is unverified — see §7.4 before relying on it.

---

## 6. Measurement recipe

Nothing in §2–§5 should be believed on this hardware until it has been measured. The method is the
differential one from `AGENTS.md`, with the discipline that makes it trustworthy.

### 6.1 Set up a valid proxy

Per `AGENTS.md` "Testing memory on a spare device", a spare device is only valid if it is loaded
**exactly** like production:

1. **Match the resident scripts.** The heap is shared. Stopping `watchdog.js` "to make room"
   invalidates the whole test — this exact mistake produced a green spare-device result followed
   immediately by a production `out_of_memory`.
2. **Match the KVS configuration.** This is the big one — the 5-vs-24-key difference was 7.9 KB.
   ```bash
   B="myhome ctl shelly call -B tcp://192.168.1.2:1883 -T 60s"
   $B <prod-device> KVS.GetMany '{"match":"script/<name>/*"}'
   # then KVS.Set each key on the spare, overriding any device-id key to the spare's own id
   ```
3. **Record both firmware versions** (`Shelly.GetDeviceInfo` → `ver`). Firmware has not explained an
   OOM difference in practice, but PR #426 found a build that ran on a Plus 1 at 1.7.5 and failed on
   a Pro1 at 2.0.0 — so **a Plus 1 is not a sufficient proxy for the Pro1**. Confirm on the target.

### 6.2 Take the baseline

```bash
B="myhome ctl shelly call -B tcp://192.168.1.2:1883 -T 60s <device>"
$B Script.GetStatus '{"id":2}'
# -> {"running":true,"mem_used":16632,"mem_peak":21350,"mem_free":6398}
```

Any script id returns the same device-wide numbers, so you can keep polling id 1 while stopping
id 2. `mem_peak` is a high-water mark **since the script started** — always `Script.Stop` +
`Script.Start` before a measurement run, or you are reading a stale peak from a previous build.

### 6.3 Isolate one script's footprint

```bash
$B Script.GetStatus '{"id":1}'   # baseline mem_free
$B Script.Stop       '{"id":2}'
$B Script.GetStatus  '{"id":1}'  # delta = script 2's footprint
# ...then Script.Start everything you stopped. On a production device the resident
# scripts are load-bearing (watchdog.js handles MQTT-failure reboots and firmware updates).
```

### 6.4 Change one thing at a time

This is where most of the value is, and where #421's two false starts came from.

- Upload variant, `Script.Stop`, `Script.Start`, wait for init to finish, read
  `{mem_used, mem_peak, mem_free}`. Record all three, plus the minified byte count.
- **Let the peak-setting event actually happen.** For `pool-pump.js` the peak is set during init,
  so a post-init reading is meaningful; for a script whose peak comes from a rare coincidence, let
  it run through one.
- **Change one variable per run.** A run that changes two optimisations tells you their sum and
  nothing else — and if they cancel, it tells you they did nothing.
- **Keep a table.** `AGENTS.md` "JS Heap Budget" and issue #433 both carry one; add rows rather
  than starting a new table, because the value of these numbers is entirely in comparability.

### 6.5 Decide what you are looking at

| Symptom | Diagnosis | Attack with |
|---|---|---|
| `mem_used` high, `mem_peak` ≈ `mem_used` + small | **size/retention problem** | §1.1 minification, §4 retained state, dead code |
| `mem_peak` ≫ `mem_used` | **allocation problem** | §2, §3 — pools, `userdata`, no per-call literals |
| `mem_free` < ~5 KB at idle | one allocation from death | both; `AGENTS.md` rule of thumb is ≥5 KB |
| Runs on the spare, dies in production | proxy invalid | §6.1 — KVS keys and resident scripts first |
| Size cut produced no peak change | you had an allocation problem | stop trimming, go to §2 |

---

## 7. Q&A

**When is a closure worth it?**
When it is created **once**, at load or at subscription time, and lives for the script's lifetime —
an event handler, a timer callback, a module-level helper. It is not worth it when it is created per
call, per tick, or per loop iteration; there, use a named top-level function plus `userdata` (§3.2)
or a preallocated slot (§3.1). The rule of thumb: *if you can count the closures your script will
ever create on your fingers, they are free; if the count scales with anything, they are not.*

**How do I measure one script's footprint?**
§6.3. Stop it, diff `mem_free`. There is no per-script API — the heap is device-wide.

**How do I tell whether I have a size problem or an allocation problem?**
§6.5. Short version: compare `mem_peak` to `mem_used`. If the gap is large, the gap is your problem,
and no amount of minification will close it.

**Does minification help?**
Yes, but weakly and only against the *residency* tenant. Measured: 5145 minified bytes bought 1764
bytes of resident and 644 bytes of peak. Always minify — but never expect it to rescue a script
whose problem is transient allocation, and never upload minified while debugging (`--no-minify`),
because minification mangles the identifiers in crash traces. PR #430 adds a `demangle` command for
when you must.

**Is it safe to preallocate on a device with 23 KB?**
Yes, and it is the recommended technique here. PR #426's `CALL_SLOTS` cost **+3878 bytes resident**
and returned **−980 bytes of peak**, converting a dead script into a live one. Preallocate at load,
when the heap is emptiest. Size the pool from a real device limit (5 concurrent RPCs, 5 timers) plus
a small margin, and always include a fallback that allocates rather than drops work.

**What does `mem_peak` vs `mem_free` tell me?**
`mem_peak` is the high-water mark since the script started — it decides whether the script survives.
`mem_free` is the instantaneous headroom — it tells you how much slack a *future* burst has.
`mem_used + mem_free` ≈ 23030 while any script runs (with all scripts stopped `mem_free` reaches
~25200, so the VM itself costs ~2 KB — treat "total heap" as approximate and always compare
like-for-like states). Aim for `mem_free` ≥ ~5 KB at idle and `mem_peak` ≤ ~20 KB. A script booting
with 1–2 KB free is one allocation from `out_of_memory` and will die unpredictably later.

**How do I test this without touching production?**
§6.1, and honestly: you cannot fully. A Plus 1 at fw 1.7.5 was proven *not* to be a valid proxy for
a Pro1 at fw 2.0.0 (PR #426). Use the spare for iteration with matched scripts and matched KVS, then
confirm the winner on the target hardware during a window when the controlled equipment can safely
be off — and have the last-known-good script ready to re-upload. Remember that with the script dead,
every device `Schedule` job (they all use `script.eval`) becomes a no-op and the equipment is left
uncontrolled.

**Can I split a big script into two smaller ones to fit?**
**No.** The heap is one device-wide pool. A second script adds a second VM context, its own
bytecode, its own copy of shared helpers (`log`, `storeValue`, config loading) and needs IPC to talk
to the first. It strictly increases total heap use. What *does* work is moving a script off the
device entirely, or moving logic to the daemon — see the `pool-pump.js` proposal's structural
options.

**The minifier changes my identifiers — how do I debug a crash trace?**
Upload with `--no-minify` while debugging (`go run ./myhome ctl shelly script upload <device>
<script.js> --no-minify`). If the unminified build does not fit — which is the case for
`pool-pump.js` — use PR #430's symbol map and `myhome ctl shelly script demangle`.

**Is it Espruino or mJS?**
`AGENTS.md:77` says "a modified version of Espruino". PR #426's own hardware log says "Wrapper
installs on mJS". The documented constraints (no `[].shift()`/`unshift()`, `let`/`const` unsafe,
`Array.prototype.slice.call(arguments)` unreliable) match mJS, not Espruino. This matters for
estimating §1.1, so §7.1 below gives the experiment. Everything else in this guide holds either way:
both engines are mark-and-sweep, both have immutable strings, and in both a closure pins its
enclosing scope.

---

## 8. Open questions and the experiments that settle them

No device access was available while writing this. Everything below is a claim this document
*relies on* but could not verify. Each has a cheap, non-destructive experiment.

### 8.1 Which engine is it?
**Why it matters**: decides whether string literals are foreign pointers into bytecode (mJS, cheap)
or owned heap strings (Espruino, expensive) — a ~3× swing in what removing 1.5 KB of `description:`
strings is worth.
**Experiment**: one `Script.Eval` — `typeof ffi` (mJS-only global) and `typeof E` / `typeof
process` (Espruino-only). Read-only, no state change.

### 8.2 Does `mem_used` include bytecode, and at what rate?
**Why it matters**: the 0.34 static→resident ratio in §1.1 is a single data point extrapolated to
every size estimate in this repo.
**Experiment**: upload a variant with 4 KB of pure string-literal padding in a never-referenced
top-level `var`; diff `mem_used`. Then a control variant with 4 KB of extra *comments* (stripped by
the minifier), which must show zero change.

### 8.3 Does the arena shrink after a burst? (§1.4)
**Why it matters**: this is the central mechanism claim of the whole document. If garbage is
collected promptly and the arena returns memory, then per-call allocation is far less important than
argued here — though the #421 measurement would then need another explanation.
**Experiment**: drive a task-queue loop that allocates a known object N times, restarting the script
between runs, and read `mem_peak` for N = 1, 5, 20, 50. Linear growth in N ⇒ garbage accumulates
across ticks. Separately, read `mem_used` immediately after boot and again after 10 idle minutes: if
it does not fall back, the arena does not shrink.

### 8.4 Can a preallocated `params` object be reused across `Shelly.call`s?
**Why it matters**: gates §5.8's second half and roughly 2 allocations per KVS write.
**Experiment**: fire two `KVS.Set` calls back to back reusing one params object with different
key/value pairs; confirm both keys land with the correct values.

### 8.5 How big and how frequent are Shelly `status/switch:N` payloads really?
**Why it matters**: §2.3 ranks this as the largest recurring transient in `pool-pump.js`, on an
assumed ~200–250 bytes at roughly one message per minute per channel, times four subscriptions.
**Experiment**: `mosquitto_sub -t '<device>/status/switch:0' -v` for five minutes; count messages
and bytes. Broker-only, no device write.

### 8.6 Is the 5→24-key +7.9 KB still true after the `CALL_SLOTS`/`userdata` rework?
**Why it matters**: it is the evidence base for "KVS keys are expensive". If the rework already
collapsed it, then most of that cost was per-key closures — which would *confirm* §2.1 while
removing config-key count as a remaining lever.
**Experiment**: on the spare, run the PR #426 build with 5 keys and with the full 24, restarting
between, and compare `mem_used` and `mem_peak`.

---

## 9. Checklist for a new or modified script

- [ ] Every function value created at runtime is created **once**, not per call/tick/iteration.
- [ ] Sequential async chains pass context via `Shelly.call`'s `userdata` to one named handler.
- [ ] No object or array literal inside a function that runs more than a few times.
- [ ] No `JSON.parse` of a payload you read fewer than ~3 fields from, where you control the format.
- [ ] No work (`JSON.stringify`, `+`) done at a `log()` call site that `log()` may discard.
- [ ] No `description:` or other human-only strings inside runtime objects — use `//` comments.
- [ ] Phase-scoped state (`CONFIG_SCHEMA`, working arrays) set to `null` when the phase ends.
- [ ] Every new KVS key justified; unread-at-runtime keys marked `cliOnly`.
- [ ] Pools sized from a real device limit, with a fallback that allocates rather than drops work.
- [ ] Measured on the **target** hardware with matched resident scripts and matched KVS, one change
      per run, `Script.Stop`/`Start` before each reading, results appended to the existing table.
- [ ] `mem_free` ≥ ~5 KB at idle and `mem_peak` ≤ ~20 KB before calling it done.
