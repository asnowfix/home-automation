# The JS Heap — the binding constraint on large scripts

Contents:
- [One shared pool](#one-shared-pool)
- [The JsVar model: fixed-size cell allocation](#the-jsvar-model-fixed-size-cell-allocation)
- [Memory costs: what is free, what is not](#memory-costs-what-is-free-what-is-not)
- [mem_peak decides survival](#mem_peak-decides-survival)
- [Measuring one script's footprint](#measuring-one-scripts-footprint)
- [mem_peak resets on restart, but only settles seconds later — reboot between arms](#mem_peak-resets-on-restart-but-only-settles-seconds-later--reboot-between-arms)
- [The cost of HTTP.GET + JSON.parse](#the-cost-of-httpget--jsonparse)
- [String concatenation and runtime allocation](#string-concatenation-and-runtime-allocation)
- [Function declarations vs. concatenated strings](#function-declarations-vs-concatenated-strings)
- [Practical memory-reduction techniques](#practical-memory-reduction-techniques)
- [Measurement tools](#measurement-tools)
- [Testing on a spare device](#testing-on-a-spare-device)

---

## One shared pool

**The JS heap is ONE pool shared across every script on the device.** It is not per-script. Read it
from any script's status — the numbers are device-wide:

```bash
myhome ctl shelly call -B tcp://<broker>:1883 -T 60s <device> Script.GetStatus '{"id":N}'
# -> {"running":true,"mem_used":16632,"mem_peak":21350,"mem_free":6398}
```

`mem_used + mem_free` is the **total heap** — measured at **23030 bytes** on both a Shelly Pro1
(fw 2.0.0) and a Shelly Plus 1 (fw 1.7.5). A script that cannot start reports
`{"running":false,"errors":["out_of_memory"]}`.

Three consequences that catch people out:

- Another script's footprint is your problem.
- Any script id returns the same device-wide numbers, so you can keep polling id 1 while stopping
  id 2.
- A spare device is only a valid proxy if it is loaded like production — see below.

---

## The JsVar model: fixed-size cell allocation

To predict memory costs, you must understand how Espruino allocates memory at a low level. The entire
heap is divided into uniform blocks called **JsVar**, not variable-size malloc allocations. This model
explains why a single byte costs more than a byte.

**Source**: [Espruino Internals](https://www.espruino.com/Internals), [EspruinoDocs Internals.md](https://github.com/espruino/EspruinoDocs/blob/master/info/Internals.md), [jsvar.h](https://github.com/espruino/Espruino/blob/master/src/jsvar.h)

### JsVar structure: 16 bytes

A single JsVar is a fixed 16-byte block (on Shelly; size varies 10–16 bytes across devices) containing:

```
Bytes 0–3:    varData (stores int, float, or pointer data in-place)
Bytes 4–5:    nextSibling (link to next sibling variable)
Bytes 6–7:    prevSibling (link to previous sibling)
Bytes 8–9:    firstChild (link to first child variable)
Bytes 10–11:  Reference count (tracks how many pointers reference this var)
Bytes 12–13:  lastChild (link to last child)
Bytes 14–15:  Flags (type: int, string, function, object, etc.)
```

**Key insight: Every variable, even a single boolean, consumes one full 16-byte block.** There is no
per-variable header overhead on top of that (unlike malloc, which adds 4–8 bytes), but the fixed size
means small values are expensive.

**Source**: [Performance.md](https://www.espruino.com/Performance), [Architecture of JavaScript variables Wiki](https://github.com/espruino/Espruino/wiki/Architecture-of-JavaScript-variables)

### How strings chain across cells

Strings do not fit into one cell. A string uses a **chain of JsVars**:

- **Short strings** (≤3 chars on 16-byte JsVar): stored directly in the `varData` field.
- **Medium strings** (4–~12 bytes): occupies the data field plus overflow into `next`/`prev` pointer fields.
- **Long strings**: linked chain of `StringExt` blocks appended to the main string's JsVar.

Example: a 50-byte string needs:
- 1 main JsVar (16 bytes) holding metadata + first ~12 bytes of content
- ~3 additional StringExt JsVars (~16 bytes each) holding the rest

**Total RAM cost**: 64 bytes for a 50-byte string = **1.28× overhead**.

When you concatenate strings at runtime (`s1 + s2`), Espruino allocates a new string chain. The old
strings remain in RAM as long as anything references them (they are not collected until garbage
collection detects they are unreachable).

**Source**: [Performance.md](https://www.espruino.com/Performance), [forum: Internals discussion](https://forum.espruino.com/conversations/265821/)

### How objects and arrays chain

Arrays and objects use **2 JsVars per element** — one for the key, one for the value:

```js
var obj = { a: 1, b: 2 };  // uses 1 (header) + 2×2 (a=1, b=2) = 5 JsVars = 80 bytes
var arr = [1, 2];          // uses 1 (header) + 2×2 (index 0, index 1) = 5 JsVars = 80 bytes
```

Long object keys (>10 characters) require additional JsVars to store the overflow characters.

**Typed arrays are more efficient**: `Uint8Array(100)` uses 1 header + 1 ArrayBuffer + 1 Flat String
+ ~7 data blocks = ~9 JsVars total (144 bytes) to store 100 bytes of raw data = **1.44× overhead**.
Normal arrays storing 100 integers would use 101 × 2 × 16 bytes = 3232 bytes.

**Source**: [Performance.md](https://www.espruino.com/Performance)

---

## Memory costs: what is free, what is not

Understanding what **does** and **does not** consume RAM in Espruino is crucial to predicting the cost
of changes.

### What is free (or nearly free)

**Uncalled function definitions** (when in flash): A function you define but never call consumes almost
zero RAM if the code is stored in flash (not in RAM). The function metadata takes ~1 JsVar, but the
function body is executed directly from flash.

```js
function largeUnusedFunc() {
  // 10 KB of code here doesn't touch RAM if in flash
}
// Cost in RAM: ~16 bytes (metadata only)
```

When code is uploaded via Shelly's Script.PutCode, the minified source is stored in flash. Function
definitions in that flash are **executed in-place** without copying to RAM.

**Source**: [Performance.md](https://www.espruino.com/Performance), [Saving code](https://www.espruino.com/Saving), [Modules discussion #6602](https://github.com/orgs/espruino/discussions/6602)

**Comments and whitespace**: Minification removes both. The minified output contains no comments, no
excess whitespace, and reserved words are compressed to single tokens. This saves flash space but does
not directly affect RAM usage of parsed code.

**Long variable names**: After minification, identifier names are mangled to single or double
characters (`a`, `b`, `_a`, etc.). This saves flash space. In RAM, each name is stored as a string,
but mangled names are shorter strings, so they cost less.

**Source**: [Shelly memory optimization techniques](https://github.com/LeivoSepp/Shelly-Memory-Optimization/blob/main/README.md)

### What is expensive (per-call overhead dominates)

**String concatenation** (`s1 + s2` or `s += fragment`): Each concatenation creates a fresh string
JsVar chain in RAM. If you build a 1135-byte string via 31 concatenations (`var s = 'a' + 'b' + ...`),
you pay:

- The final 1135-byte string: ~70 JsVars = ~1120 bytes
- Intermediate temporary strings during construction: unpredictable, depends on evaluation order
- **Worst case**: ~2× the final size

The entire result is allocated at once and kept alive. If the string is referenced by a top-level variable
(like a config value), it is **never garbage-collected** even if no code uses it.

**Source**: [Performance.md](https://www.espruino.com/Performance), repository measurements in the existing `memory.md` this file

**Closures per-call**: Allocating a closure (anonymous function) for every RPC callback creates a
new JsVar + captured-variable chain per call. If your code does `Shelly.call(..., (r) => { ... })`,
a closure is allocated and then deallocated per call.

Measured cost of 39 closures (one per device RPC during init) was **~1050 bytes peak**.

**Source**: Repository measurements on pool-pump.js; see pool_pump_crash_fix.md in this codebase's
MEMORY.md history

**Object literals per-call**: Building a new object `{ a: 1, b: 2 }` allocates JsVars for the object,
key, key string, value, and value container. Doing this in a hot path (called N times per second)
compounds the pressure.

**JSON.parse() of large payloads**: Parsing a JSON response allocates a JsVar **per value** in the
parsed tree. A 250-byte response with 40 fields becomes 40+ JsVars in RAM simultaneously. See
[The cost of HTTP.GET + JSON.parse](#the-cost-of-httpget--jsonparse) below.

**Source**: [Shelly optimization: use string search instead of JSON.parse](https://github.com/LeivoSepp/Shelly-Memory-Optimization/blob/main/README.md)

### Lazy parsing: peak settles slowly

Espruino does **lazy parsing** of function bodies. The function definition is parsed when the code is
uploaded/executed, but the **function body is not fully parsed until the function is called** (or
during garbage collection sweeps).

This means `mem_peak` keeps climbing for 10–20 seconds after a script starts because the init code
exercises code paths that trigger parsing of functions, which weren't parsed before.

**Consequence**: A single `Script.GetStatus` immediately after start is not a measurement. The peak
keeps moving upward until init fully settles.

**Source**: Repository measurements on pool-pump.js and other real devices (see sections below on
measuring and rebooting)

---

## mem_peak decides survival

Not `mem_used`, and **not the source file size.** Measured for `pool-pump.js` on a Pro1 with
`watchdog.js` resident alongside it:

| version | minified | mem_peak | mem_free | result |
|---|---|---|---|---|
| v0.11.9 | 30409 | 17136 | 12068 | comfortable |
| main (`ae4c5da`) | 36155 | 22330 | 10276 | runs, ~700 B peak headroom |
| + #421 fix, first cut | 40002 | — | — | **out_of_memory** |
| + static size trim (−1519 B) | 38483 | — | — | **out_of_memory** |
| + fixed call-slot pool | ~38500 | 21350 | 6398 | runs |

**Two lessons, both learned the hard way:**

1. **Peak/transient allocation dominates static size.** Trimming 1519 minified bytes changed
   nothing. Removing a *single per-call allocation* moved peak by ~1050 bytes and turned OOM into a
   working script. Before optimising size, hunt per-call and per-tick allocation: a closure created
   per RPC, an object literal built per call, string concatenation in a hot path.

2. **Prefer a fixed pool to per-call allocation.** `pool-pump.js` tracks in-flight RPCs with
   `CALL_SLOTS`: a small array of `{cb, ud, used}` records allocated **once** at load, claimed in
   place, and passed through `Shelly.call`'s `userdata` to ONE shared completion handler. The
   earlier version allocated a fresh closure on every `Shelly.call` — 39 during init alone.

**Rule of thumb:** aim for `mem_free` ≥ ~5 KB steady-state. A script that boots with 1–2 KB free is
one allocation away from `out_of_memory` and will die unpredictably later.

---

## Measuring one script's footprint

Because the heap is shared, no API reports a single script's cost. Measure by difference: read
`mem_free`, stop the script, read `mem_free` again.

```bash
B="myhome ctl shelly call -B tcp://<broker>:1883 -T 60s <device>"
$B Script.GetStatus '{"id":1}'        # baseline mem_free
$B Script.Stop       '{"id":2}'
$B Script.GetStatus  '{"id":1}'       # delta = script 2's footprint
$B Script.Stop       '{"id":1}'
$B Script.GetStatus  '{"id":1}'       # delta = script 1's footprint
# ...then Script.Start both again to leave the device as you found it
```

Measured on a Shelly Plus 1 (fw 1.7.5):

| state | `mem_free` | implied footprint |
|---|---|---|
| `watchdog.js` + `myhome-link.js` running | 21350 | — |
| `myhome-link.js` stopped | 23044 | `myhome-link.js` ≈ **1694 B** |
| `watchdog.js` also stopped | 25200 | `watchdog.js` ≈ **2156 B** |

Two caveats:

- **`mem_used + mem_free` is not constant.** It reads ~23030 with a script running but `mem_free`
  reaches ~25200 with all scripts stopped — the running VM itself costs roughly 2 KB. Treat "total
  heap" as approximate and always compare like-for-like states.
- **Always restart what you stopped.** On a production device the resident scripts are load-bearing;
  `watchdog.js` handles MQTT-failure reboots and firmware updates.

---

## mem_peak resets on restart, but only settles seconds later — reboot between arms anyway

**Corrected 2026-08-16.** This section previously stated that `mem_peak` does not reset when you stop
and restart a script. That is wrong, and it was measured wrong in both directions.

Restarting the script makes `mem_peak` **decrease** — which a monotonic high-water mark cannot do —
and it then climbs back through init, settling ~20 s later *above* where it started. Measured on two
Pro1 devices the same evening, same build:

| | before restart | t+5 s | t+20 s (settled) |
|---|---|---|---|
| `mezzanine` | 21644 | **21420** | 21602 |
| `filtration-hiver` | 21644 | **21448** | 21826 |

Two consequences, and the second is the one that bites:

- A **pre-restart reading is not comparable with a post-restart one** unless both are settled. The
  21644 above was itself taken too early, right after a deployment — the true settled peak for that
  build on `filtration-hiver` is **21826**, 182 bytes higher.
- **A single `Script.GetStatus` is still not a measurement**, for the reason given below: the peak
  keeps nudging upward for tens of seconds as init exercises lazily-parsed code paths.

The old claim was inferred from three consecutive measurements of different `pool-pump.js`
configurations all returning an identical 22778. That observation stands; the explanation does not.
The likeliest cause is sampling before the peak settled, or three arms that genuinely peaked alike —
not a mark that survives a restart.

**Reboot between arms regardless.** A restart clears the script's own peak but not other resident
scripts' contribution to the shared pool, and the heap is one device-wide pool. The protocol below
remains correct.

The protocol that works, **per arm**:

1. delete the script and force-upload it (`--force`; see the version-marker trap in #449),
2. `Script.SetConfig {"id":N,"config":{"enable":false}}` so it does not auto-start,
3. **`Shelly.Reboot`**, and wait for the device to come back,
4. `Script.Start`, then sample.

`myhome ctl shelly script probe <device> <script>` does the sampling part: it polls until the peak
stops moving and refuses to report an unsettled run as a measurement. **A single `Script.GetStatus`
call is not a measurement** — on a Pro1, `pool-pump.js` reaches its peak within ~400 ms of start, but
lazily-parsed functions keep nudging it upward for tens of seconds as init exercises new code paths.

---

## The cost of HTTP.GET + JSON.parse

Fetching and parsing an HTTP response is the single most expensive thing these scripts do per byte.
Measured on `mezzanine` (Pro1, fw 2.0.0, 2026-08-08) with a purpose-built script that does nothing
but `Shelly.call('HTTP.GET', ...)` and `JSON.parse(res.body)`, so its `mem_peak` is essentially the
fetch's own transient. Each arm rebooted the device first.

| arm | response | `mem_peak` |
|---|---|---|
| control, no fetch (39 B script) | — | 140 |
| fetch + parse (285 B script) | 251 B | 1946 |
| fetch + parse (285 B script) | 861 B | 4396 |

Both fetch arms ran byte-identical code, so the difference is purely payload:

- **~4.0 bytes of peak heap per byte of response**
- **~940 bytes fixed** (the `HTTP.GET` machinery plus the small test script itself)

The multiplier exists because the response string and the parsed object graph are alive
simultaneously, and every number in a JSON array becomes its own JsVar. Predicted peaks: a 2 KB
response ≈ 9 KB, a 5 KB response ≈ 21 KB — consistent in magnitude with the garden OOM in #271,
where ~5 KB peaked around 28 KB and ~2 KB survived at ~18.5 KB.

**Practical consequences:**

- Ask the API for the narrowest data you can. `forecast_days=1` versus `past_days=3` is not
  cosmetic; it is kilobytes of heap.
- A response that would be fine alone can still OOM a script whose *load* already peaked near the
  ceiling. Check the free heap **after** load, not the total.
- Conversely, a fetch cannot set a new peak if it fits in the heap left free after load. On
  `pool-pump.js` the load peak is 22778, leaving ~9.6 KB free, and its ~861 B forecast needs
  ~4.4 KB — so shrinking the payload does **not** lower that script's peak. This is why #271 does
  not unblock #433.

---

## String concatenation and runtime allocation

String concatenation at runtime is fundamentally different from string constants in source code.

### Runtime concatenation allocates new JsVars

When you execute `var s = 'a' + 'b' + 'c'`, Espruino:

1. Parses each literal `'a'`, `'b'`, `'c'` into JsVar string objects in flash or ROM
2. **Allocates a temporary string** for `'a' + 'b'` in RAM (new JsVar chain)
3. **Allocates another temporary** for `(result) + 'c'` in RAM
4. **Allocates the final result** assigned to `s`, which is a JsVar chain

The intermediates are deallocated after use, but if any are aliased (kept in a variable), they remain
in RAM.

Measured on the repository: adding a ~1950-byte MQTT subscription (which builds a 31-fragment
concatenated string via `+` operator) increased `mem_used` by **+5432 B**. The string itself is only
~1135 bytes minified; the ratio (5432 ÷ 1135 ≈ 4.8×) reflects transient allocations during init.

**Source**: Repository measurements on pool-pump.js

### Code at upload time vs. runtime

When you upload a Shelly script, the source is minified and stored in flash. String **constants in
source** (like `var CONST = "..."`):

- If the string is only referenced in dead code, it doesn't activate in RAM
- If the string is referenced at load time, one JsVar chain is allocated and kept alive
- The original source is stored in flash, not copied to RAM

By contrast, string **built via concatenation at runtime**:

- Each `+` operation allocates temporary JsVars
- The final result is kept alive for the duration of the script (if assigned to a top-level variable)
- The source code for the fragments (e.g. 31 small string fragments) is stored in flash, but the
  concatenated result is **entirely in RAM**

**This is why the working hypothesis is plausible**: a function definition + `fn.toString()` might use
less RAM than building a concatenated string, because the function body can remain in flash.

**Source**: [Saving code](https://www.espruino.com/Saving), [Modules discussion #6602](https://github.com/orgs/espruino/discussions/6602)

---

## Function declarations vs. concatenated strings

This is the core question raised by the campaign: when you have a transform or other logic that must be
**published as text**, should you:

**Option A: Build it via concatenation**
```js
var FORECAST_TRANSFORM =
  'function(body){' +
  'var out={max_temp_c:null,...};' +
  ... 31 fragments ...
  '}';  // ~1135 bytes
```

**Option B: Declare it as a function and publish `fn.toString()`**
```js
function forecastTransform(body) {
  var out = {max_temp_c: null, ...};
  ...
  return result;
}
var FORECAST_TRANSFORM = forecastTransform.toString();
```

### Analysis

**Option A (concatenation):**
- Final string: ~1135 bytes of JsVar chains in RAM
- Fragments: 31 string constants in flash
- Total RAM during build: ~1120 bytes (the result) + transient allocations during `+` chains
- Permanent footprint: ~1135 bytes

**Option B (function + toString):**
- Function definition: ~1 JsVar (metadata) in RAM + function body in flash
- `fn.toString()` returns: ???

The critical unknown is **what does `fn.toString()` return?** There are two possibilities:

1. **Native string to flash** (most efficient): `.toString()` returns a native string that references
   the function's source in flash. Overhead: ~16 bytes per reference (header JsVar) + the chain of
   that reference. RAM cost: ~50–100 bytes.

2. **Copy to RAM** (less efficient): `.toString()` copies the function source from flash into RAM,
   allocating a new JsVar chain. RAM cost: same as Option A (~1135 bytes).

### What the documentation says (confirmed sources)

- Functions stored in flash via "Save on Send" use **native strings pointing to flash** ([Saving code](https://www.espruino.com/Saving))
- Native strings **reference** data in flash rather than copying to RAM ([Modules discussion #6602](https://github.com/orgs/espruino/discussions/6602))
- `E.toString()` can convert data *from* flash storage to a string ([Saving code](https://www.espruino.com/Saving))

**However**, the documentation does **not** explicitly state what `.toString()` on a regular JS
function returns, or whether it allocates a new JsVar chain or returns a reference to the original
source.

### The measurement on pool-pump.js

The campaign's real-world data point: deleting three uncalled functions reduced minified size by
−2530 B but resident `mem_used` by only −392 B (ratio 0.15×). Adding a ~1950 B MQTT subscription
(which builds the 31-fragment string via concatenation) increased `mem_used` by +5432 B (ratio 2.8×).

**Interpretation:**
- Uncalled function definitions in flash cost almost nothing resident (~392 B per 2530 B deleted
  suggests metadata overhead only)
- String concatenation costs ~2.8× the final size, suggesting both the result and transient
  allocations during construction

This suggests Option B (function declaration) *might* be more efficient, but **only if** `.toString()`
returns a native string to the flash-resident source.

**Source**: Repository campaign #401 measurements

### Direct answer (with confidence level)

**Most likely**: Option B uses **less heap than Option A**, but the savings are modest: ~1000–1100
bytes vs. ~1135 bytes. The benefit is that the function body remains in flash, and assuming
`.toString()` returns a reference (not a copy), the stringified result is small.

**Confidence**: **Medium (60–70%)**. The documentation confirms that native strings reference flash
and function bodies execute from flash, but it does not explicitly state whether `.toString()` on a
regular JS function returns a flash reference or a RAM copy.

### Empirical test to settle it on-device

To definitively answer the question, run this on a Shelly device with both your production scripts
running (to simulate real heap pressure):

```js
// Before any changes, record baseline:
// Script.GetStatus({"id":N}) → mem_used_before

// Then:
function testTransform() {
  var s = "function(body){" +
    "var out={x:1};return out;}";
  return s;
}

Script.GetStatus({"id":N}) → mem_used_after_func

// Now call it:
var result = testTransform.toString();
Script.GetStatus({"id":N}) → mem_used_after_tostring

// Compare deltas:
// Δ1 = mem_used_after_func - mem_used_before  (function definition cost)
// Δ2 = mem_used_after_tostring - mem_used_after_func  (toString() cost)
```

If Δ2 is small (~16–50 bytes), the string is a flash reference. If Δ2 is large (~400–1000 bytes), it
was copied to RAM.

---

## Practical memory-reduction techniques

Ranked by expected savings in resident `mem_used` on a typical Shelly script:

### Tier 1: Replace per-call allocations with pools (1000+ bytes)

**Impact**: Highest. Eliminating a single closure created per callback or per call saves up to
~1050 bytes peak. Allocate once, reuse.

**Example**: Instead of `Shelly.call(method, params, (r) => { ... handle r ... })`, use a CALL_SLOTS
array and one shared handler.

**Source**: Repository pool-pump.js optimization, memory.md history

### Tier 2: Avoid JSON.parse on large payloads (500–2000 bytes)

**Impact**: Very high. A 5 KB JSON response peaks at ~20 KB. Use string search to extract needed
values instead.

**Example**:
```js
// Bad: JSON.parse creates 40+ JsVars for a 250-byte response
var data = JSON.parse(response);
var temp = data.temp;

// Good: extract via string search
var temp = extractValue(response, '"temp":');
```

**Source**: [Shelly optimization guide](https://github.com/LeivoSepp/Shelly-Memory-Optimization/blob/main/README.md)

### Tier 3: Move large strings to flash or lazy-load them (200–500 bytes)

**Impact**: High. If a large string is only needed at specific times, define it inside a function (lazy-load) rather than at global scope.

**Example**:
```js
// Bad: 1135-byte string allocated at load
var FORECAST_TRANSFORM = 'function(body){...}';

// Possibly better: function definition costs almost nothing if uncalled
function getForecastTransform() {
  return forecastTransform.toString();  // or inline the string
}
```

Caveat: This only helps if the string is not used every script invocation. If called on every MQTT
message, the savings are minimal.

**Source**: [Shelly optimization guide](https://github.com/LeivoSepp/Shelly-Memory-Optimization/blob/main/README.md), [Performance.md](https://www.espruino.com/Performance)

### Tier 4: Minify and shorten identifiers (50–200 bytes)

**Impact**: Moderate. The minifier converts reserved words to tokens and mangles variable names. Each
byte of identifier saved saves a byte in flash; minified code also executes slightly faster.

**Implementation**: Run through the minifier. For Shelly, you likely already do this.

**Source**: [Performance.md](https://www.espruino.com/Performance)

### Tier 5: Use Typed Arrays instead of normal arrays (50–500 bytes for bulk data)

**Impact**: Moderate. Storing bulk data (e.g., historical readings) in `Uint8Array` is 3–5× more
efficient than a normal JS array.

**Example**:
```js
// Bad: 100 integers
var readings = [12, 14, 16, ..., 98];  // ~3200 bytes (100 × 2 × 16)

// Good: 100 bytes
var readings = new Uint8Array([12, 14, 16, ..., 98]);  // ~144 bytes
```

**Source**: [Performance.md](https://www.espruino.com/Performance)

### Tier 6: Use `print()` instead of `console.log()` (10–50 bytes per call)

**Impact**: Minimal. `print()` is a built-in with lower overhead than `console.log()`.

**Source**: [Shelly optimization guide](https://github.com/LeivoSepp/Shelly-Memory-Optimization/blob/main/README.md)

---

## Measurement tools

Shelly Gen2 devices expose **only** the following memory monitoring API:

### Script.GetStatus({"id": N}) → mem_used, mem_peak, mem_free

Returns device-wide heap statistics:

- **`mem_used`**: Bytes currently in use across all scripts
- **`mem_peak`**: Peak bytes used since the script was started (monotonic high-water mark, resets on
  restart)
- **`mem_free`**: Available bytes (≈ 23030 − mem_used on Shelly Pro1/Plus1)

**Note**: These are **device-wide** totals, not per-script. No per-script breakdown is available.

**Upstream Espruino exposes** (but Shelly may not):

- **`E.getSizeOf(object, depth)`**: Returns the number of JsVar blocks used by `object` and
  optionally its children (if depth > 0). Example: `E.getSizeOf(myString)` → 7 blocks = 112 bytes.
- **`process.memory()`**: Returns `{ free: N, used: N, total: N, history: [...], gc_count: N }`.
- **`E.dumpVariables()`**: Prints all variables and their size.
- **`trace(variable)`**: Prints the type and structure of a variable.

**Availability on Shelly**: **Unconfirmed**. These functions may not be exposed in Shelly's Espruino
fork. Do not rely on them without testing first.

**Source**: [Performance.md](https://www.espruino.com/Performance), [Shelly Script API](https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Script/)

---

## Testing on a spare device

A spare device is only a valid proxy if it is loaded **exactly** like production. Getting this wrong
produced a green result on a spare followed immediately by an OOM on the real one.

1. **Match the resident scripts.** Stopping other scripts to "make room" invalidates the test — the
   heap is shared. Production had `watchdog.js` resident; the spare had it stopped.

2. **Match the KVS configuration — this is the big one.** With 5 `script/pool-pump/*` keys the
   script used `mem_used 13748`; with production's 24 keys the identical code OOM'd. **~7.9 KB of
   footprint was config-driven runtime state, not code.** Copy the real config across first:

   ```bash
   myhome ctl shelly call -B tcp://<broker>:1883 -T 60s <prod-device> KVS.GetMany '{"match":"script/<name>/*"}'
   # then KVS.Set each key on the spare, overriding any device-id key to the spare's own id
   ```

   Note what a KVS copy does **not** carry: `Script.storage` values (e.g. `STATE.scheduleMode`) live
   outside KVS entirely, and Schedule jobs are not KVS at all. A device with the right config and no
   schedule jobs still does nothing.

3. **Record both firmware versions** (`Shelly.GetDeviceInfo` → `ver`). They differ across the fleet
   (Pro1 2.0.0 vs Plus 1 1.7.5). Firmware did NOT explain an OOM difference in practice — record it,
   but do not reach for it as an explanation before checking scripts and KVS.

---

## Summary: The per-byte multiplier paradox

The core insight is that **byte-counting is not enough to predict memory cost**. A 31-fragment
concatenated string is ~1135 bytes but peaks at ~5400 bytes. This is not because Espruino is
inefficient; it is because:

1. **JsVar overhead**: Each JsVar is 16 bytes minimum. A 1-byte integer costs 16 bytes.
2. **Transient allocations**: Building the string via 30 `+` operations allocates intermediates.
3. **Chaining**: Long strings need multiple JsVar blocks linked together.

The working explanation for the measurements in this document:

- Uncalled functions → almost free (metadata only, body in flash)
- String constants in source → cheap (allocated once, then constant)
- Concatenated strings at runtime → expensive (temporaries + final allocation)
- JSON parsing → very expensive (per-value JsVar allocation)
- Per-call allocations → most expensive (compounded if called N times per second)

**This is why the pool-pump.js campaign succeeded**: eliminating per-call closures (one allocation
each, 39 during init) saved ~1050 bytes of peak, while static code trimming saved nothing. Peak is
driven by *transient* allocations, not *static* size.

---

## Shelly vs. upstream Espruino: known divergences

Shelly's fork of Espruino exposes **only** Script.GetStatus for memory monitoring (no E.getSizeOf,
process.memory, or E.dumpVariables confirmed to work). All core JsVar mechanics and string allocation
model are identical to upstream Espruino.

---

## What remains unknown

1. **Exact behavior of `fn.toString()` on a regular function** (when the function body is in flash):
   Does it return a native string referencing the flash, or does it allocate a RAM copy?

2. **Availability of E.getSizeOf and process.memory on Shelly** (documented for upstream Espruino
   but not confirmed working on Shelly's fork).

3. **String interning**: Whether identical string literals are shared at runtime (unlikely in
   Espruino, but unconfirmed).

4. **Exact per-concatenation overhead**: How many bytes of temporary allocation does each `+`
   operation add?

5. **Lazy parsing threshold**: Exactly which code paths are deferred, and do all devices parse with
   the same laziness?

---

## When you looked

This document was last updated **2026-08-20**, based on primary sources:

- Espruino official documentation: https://www.espruino.com/Internals, https://www.espruino.com/Performance
- Espruino GitHub: jsvar.h, Performance.md, Architecture of JavaScript variables Wiki
- Shelly Gen2 API documentation: https://shelly-api-docs.shelly.cloud/gen2/
- Repository measurements: pool-pump.js campaign #401, existing memory.md history
- Shelly community optimization guide: https://github.com/LeivoSepp/Shelly-Memory-Optimization/

The JsVar model is stable across Espruino versions. Recent Espruino releases (2v20+) have minor
tunings but no fundamental changes to the memory model described here.

