# Writing Shelly Scripts

Contents:
- [The JavaScript engine](#the-javascript-engine)
- [Function definition order](#function-definition-order)
- [The MQTT.subscribe stack trap](#the-mqttsubscribe-stack-trap)
- [Minifier-safe patterns](#minifier-safe-patterns)
- [Wrapping: everything that can throw](#wrapping-everything-that-can-throw)
- [Callback depth and the 5-RPC limit](#callback-depth-and-the-5-rpc-limit)
- [The task queue](#the-task-queue)
- [Deferring events during async state rebuilds](#deferring-events-during-async-state-rebuilds)
- [MQTT.publish has no callback](#mqttpublish-has-no-callback)
- [Resource limits](#resource-limits)
- [Lifecycle logging](#lifecycle-logging)
- [Emitting events for operators](#emitting-events-for-operators)

---

## The JavaScript engine

Shelly devices run a **modified Espruino** — most of ES5 plus a few ES6 features. Write to this
list, not to what your editor accepts.

**Supported:** global scope `var`/`let`, `Function.prototype.bind`, `String`, `Number`, `Function`,
`Array`, `Math`, `Date`, `new`, `delete`, `Object.keys`, `Object.assign`, exceptions, ES5 array
methods (`isArray`, `map`, `filter`, `forEach`, `reduce`, `indexOf`), and `ArrayBuffer`/`AES` on
Gen 3/4 with firmware 1.6.0+.

**Not supported:** hoisting (deliberately — it would need two-pass parsing), ES6 classes (function
prototypes work), Promises and `async`, regular expressions on some boards, and `\u` escapes (use
`\xHH`).

**Quirks worth knowing:** `arguments.length` returns the number *passed* when that exceeds the
number defined, otherwise the number defined. `delete` works only without brackets. Strings are byte
arrays, not UTF-16.

### Arrays

`push` and `pop` work. **`shift` and `unshift` do not.** `Array.prototype.slice.call(arguments)` may
fail — use a loop.

```javascript
// BROKEN — shift() is not supported
array.shift();

// WORKING — manual shift
var newArray = [];
for (var i = 1; i < array.length; i++) {
  newArray.push(array[i]);
}
array = newArray;

// BROKEN — may fail on the arguments object
var args = Array.prototype.slice.call(arguments);

// WORKING
var args = [];
for (var i = 0; i < arguments.length; i++) {
  args.push(arguments[i]);
}
```

Prefer `var` throughout. `let`/`const` work on Espruino v2.14+, but `var` is safe everywhere and the
consistency is worth more than the scoping.

---

## Function definition order

**No hoisting.** A function must be defined before any line that references it — including lines
that only pass it as a callback.

```javascript
// BROKEN — onEventData is not defined yet at the point subscribeEvents is written
function subscribeEvents() {
  Shelly.addEventHandler(onEventData);
}
function onEventData(eventData) { /* ... */ }

// WORKING — definition precedes reference
function onEventData(eventData) { /* ... */ }
function subscribeEvents() {
  Shelly.addEventHandler(onEventData);
}
```

---

## The MQTT.subscribe stack trap

**Calling anything on the line after `MQTT.subscribe()` returns reliably kills the script** with
`Too much recursion - the stack is about to overflow` — even with ~23 KB of heap free. The failing
call is only ever the innermost frame in the trace, never the cause; it is simply the first thing to
touch the stack on return from the subscribe.

Measured on a Pro1 (`mezzanine`, 2026-08-12, issue #474), three controlled arms on byte-verified
code:

```javascript
// BROKEN — crashes init every time, with solar-enabled=true
MQTT.subscribe("myhome/energy/solar/available", onSolarAvailable);
log("Subscribed to myhome/energy/solar/available");

// WORKS — the same log call, one line earlier
log("Subscribing to myhome/energy/solar/available");
MQTT.subscribe("myhome/energy/solar/available", onSolarAvailable);

// ALSO WORKS — nothing after the subscribe at all
MQTT.subscribe("myhome/energy/solar/available", onSolarAvailable);
```

This cost a full day of production filtration: the pump ran with solar disabled because the script
would not start at all. **The emulator cannot catch it** — goja has no comparable stack limit.

**Rule:** log *before* the subscribe, and make `MQTT.subscribe()` the last statement in its function.

---

## Minifier-safe patterns

Minification generally works if you follow two rules. Use `--no-minify` while debugging so error
messages point at real code.

**Use `in`, not `!== undefined`.** The minifier rewrites the latter into syntax the engine rejects.

```javascript
// BROKEN with the minifier
var v = obj.prop !== undefined ? obj.prop : null;

// WORKING
var v = ("prop" in obj) ? obj.prop : null;
```

**Never write an empty catch block.** `catch (e) {}` is minified to `catch {}` — ES2019 optional
catch binding — which is a syntax error on the device. Reference the parameter so the minifier keeps
it:

```javascript
// BROKEN
try { data = JSON.parse(str); } catch (e) {}

// WORKING — parameter referenced, block still a no-op
try { data = JSON.parse(str); } catch (e) { if (e && false) {} }

// ALSO WORKING — you actually use it
try { data = JSON.parse(str); } catch (e) { log('Parse error:', e); }
```

Apply this to **every** catch block, including ones you are sure will never fire.

`Function.prototype.bind()` is fully supported and survives minification.

---

## Wrapping: everything that can throw

An uncaught exception does not fail one operation — it **terminates the entire script**. On
`filtration-hiver` that means the pump stops responding to every Schedule job, because they all
dispatch through `script.eval`. Issue #480 made this non-optional after the same failure mode was
measured in three separate places.

```
queueTask(function(){ null.x })       -> script DEAD
Script.Eval with a bad expression     -> script DEAD ("FACTS" is not defined)
throw inside an addEventHandler cb    -> script DEAD
```

Wrapping costs nothing — the return value survives:

```
(function(){ try { return String(2+2) } catch(e) { return "ERR:"+e } })()   ->   "4"
```

### Schedule jobs

Every `Schedule.Create`/`Schedule.Update` `code` field must be built by a shared per-script wrapper,
never assembled by hand at the call site — so a job added later cannot slip through unwrapped.

```javascript
function wrapScheduleCall(handlerCall) {
  return "(function(){try{" + handlerCall + "}catch(e){log('schedule handler error:',e)}})()";
}
// ...
code: wrapScheduleCall('handleMorningStart()')
```

If you read the `code` field back off a live `Schedule.List` to identify which job is which, match
by **substring containment** (`code.indexOf('handleX()') !== -1`) — the wrapped field no longer
equals the bare handler call.

### The task queue dispatcher

Wrap the single `task()` invocation inside `processTaskQueue`, not each call site. That protects
every `queueTask()` caller at once:

```javascript
var task = TASK_QUEUE[TASK_INDEX];
TASK_INDEX++;
try {
  task();
} catch (e) {
  log("queued task error:", e);
}
```

### Handler callbacks

Every `Shelly.addEventHandler`, `Shelly.addStatusHandler` and `MQTT.subscribe` callback needs the
same protection. **Wrap the body in place** — `try { ...existing body... } catch (e) { log(...) }`
inside the same function — rather than through a higher-order wrapper.

A HOF returning `function(x){ try{fn(x)}catch(e){...} }` adds one call frame to *every* dispatch
through it, and the main event dispatcher is usually the busiest handler in the script. On a device
already near its stack ceiling that frame can be the one that kills you. In-place wrapping adds zero
depth. If the callback is a named function, wrap that function's own body.

### Ad-hoc probes

**Never send an unwrapped `Script.Eval` to a live device, including for debugging.** A typo in a
throwaway diagnostic is exactly as fatal as a bug in production code.

Two further traps:

- **Match the probe to the installed version.** A diagnostic written for the solar build, evaluated
  against v0.11.9 which has no solar code, killed the production pump with
  `Uncaught ReferenceError: "SOLAR" is not defined`. Confirm what is on the device first. Prefer
  `typeof X !== "undefined" ? ... : "n/a"` over a bare reference.
- **Beware stale queued diagnostics.** A probe scheduled before a script swap can fire after it and
  be evaluated against the *new* script. Cancel pending background checks before changing what runs.

After any `Script.Eval` against a production device, re-check `Script.GetStatus`. An eval that killed
the script leaves it `running: false` and the pump uncontrolled.

#### The wrapper *does* contain a `ReferenceError` — measured, 2026-09-02

Wrapping is not merely a hedge against thrown values. A `ReferenceError` from an undefined identifier
raised **inside** the `try` is caught, on both platforms in the fleet:

| device | app / firmware | probe | result |
|---|---|---|---|
| `development` | Plus1 / 1.7.5 | `(function(){try{NOSUCH()}catch(e){if(e&&false){}}})()` | caught, script alive |
| `development` | Plus1 / 1.7.5 | `(function(){try{return NOSUCH}catch(e){if(e&&false){}}})()` | caught, script alive |
| `filtration-hiver` | **Pro1 / 2.0.0 (production)** | wrapped ref to an undefined name | **caught**, returned `ReferenceError: "to" is not defined` as a value, script alive |

This matters because a 2026-08-30 incident was written up as *"a `ReferenceError` escapes the
`try`/`catch` on this firmware"*, and that framing argues against wrapping — including against #574,
which wraps every schedule job's code and is now verified in production. **That claim does not
reproduce.** Whatever killed the script that day, the reference most likely sat somewhere the `try`
did not cover, or something else ended the script.

So: rule 1 stands and is *stronger* than it looked. Wrapping buys real containment, including against
the most likely handler failure — a renamed or missing function.

#### The safe probe form

Still use it. It is cheap, and it makes a probe survive being pointed at the wrong build:

```js
(function(){try{var u;var o={};
  o.water=(typeof F_WATER===typeof u)?null:F_WATER;
  o.win=(typeof F_WIN_START===typeof u)?null:F_WIN_START;
  return JSON.stringify(o)}catch(e){return String(e)}})()
```

`typeof X === typeof u` (with `var u` left undefined) tests existence **without naming a value**, and
the form uses **no quote characters at all**, so the surrounding JSON needs no escaping. Both traps —
version mismatch and quote escaping — disappear together.

---

## Callback depth and the 5-RPC limit

**No more than 2–3 levels of nested anonymous functions.** Per Shelly's own documentation: "A
limitation of the javascript engine that it cannot parse too many levels of nested anonymous
functions. With more than 2 or 3 levels the device crashes when attempting to execute the code."

The symptom is `Uncaught Error: Too many calls in progress`, and it has **two distinct causes** that
both exhaust the same 5-concurrent-RPC budget:

**Cause A — nesting depth**, as above.

**Cause B — `Shelly.call` inside a `for` loop.** On a real device `Shelly.call` is asynchronous and
returns immediately, so the loop dispatches every iteration before any response arrives — exhausting
the budget with zero nesting depth.

```javascript
// DANGEROUS — fires N concurrent Shelly.call invocations
for (var i = 0; i < items.length; i++) {
  Shelly.call("KVS.Set", { key: keys[i], value: vals[i] }, onDone);
}
```

Three ways out, in order of preference: use the task queue; extract named top-level functions and
pass them by reference; or use the synchronous `Shelly.getComponentStatus()` /
`Shelly.getComponentConfig()` where they suffice.

Extraction, when the queue does not fit:

```javascript
// BROKEN — 5+ levels of nesting
function loadData(callback) {
  Shelly.call("KVS.List", {}, function(resp, err) {
    for (var i = 0; i < list.length; i++) {
      (function(k) {
        Shelly.call("KVS.Get", {key: k}, function(gresp, gerr) { /* more nesting */ });
      })(list[i]);
    }
  });
}

// WORKING — named functions, one level each
function processKey(k, map, onComplete) {
  Shelly.call("KVS.Get", {key: k}, function(gresp, gerr) {
    onComplete();
  });
}
function loadData(callback) {
  Shelly.call("KVS.List", {}, function(resp, err) {
    var pending = list.length;
    function onKeyProcessed() {
      pending--;
      if (pending === 0) callback();
    }
    for (var i = 0; i < list.length; i++) {
      processKey(list[i], map, onKeyProcessed);
    }
  });
}
```

---

## The task queue

One recurring timer draining a FIFO. This is the answer to the timer budget, to nesting depth, and
to sequencing async work — define it once per script.

```javascript
var TASK_QUEUE = [];
var TASK_INDEX = 0;
var TASK_TIMER = null;

function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
    if (TASK_TIMER) {
      Timer.clear(TASK_TIMER);
      TASK_TIMER = null;
    }
    TASK_QUEUE = [];
    TASK_INDEX = 0;
    return;
  }
  var task = TASK_QUEUE[TASK_INDEX];
  TASK_INDEX++;
  try {
    task();
  } catch (e) {
    log("queued task error:", e);
  }
}

function queueTask(task) {
  TASK_QUEUE.push(task);
  if (!TASK_TIMER) {
    // NOTE: this recurring timer counts against the 5-timer-per-script budget.
    TASK_TIMER = Timer.set(200, true, processTaskQueue);
  }
}
```

Note the index-based drain rather than `shift()`, which the engine does not support.

**Never create a one-shot `Timer.set(delayMs, false, fn)` to delay a continuation.** Each one
occupies a timer slot for its whole duration. `queueTask(fn)` gives you the same minimum 200 ms
delay using the queue's existing timer. This applies after `MQTT.publish` (which has no callback),
after any fire-and-forget `Shelly.call` needing a settle delay, and to any inter-step sequencing.

```javascript
// AVOID — wastes a timer slot
Timer.set(200, false, function() { callback(null); });

// PREFER
queueTask(function() { callback(null); });
```

---

## Deferring events during async state rebuilds

**Never let an event handler read shared state while an async operation is rebuilding it.**

When a script rebuilds a shared structure across several async steps — a KVS reload chain
(`KVS.List` → N×`KVS.Get`), a sequential fetch, a settings migration — any event arriving mid-chain
reads stale or half-built state and silently drops work. Espruino is single-threaded but its event
loop interleaves callbacks freely, so this is a real-device logic bug, not a test artifact.

The pattern is a guard flag plus deferral through the task queue:

```javascript
var STATE = {
  myData: {},
  reloading: false   // true while the async rebuild chain is in progress
};

// 1. Set the flag at the top of the chain
function reloadFromKVS(callback) {
  STATE.reloading = true;
  Shelly.call("KVS.List", { prefix: "..." }, onListResponse.bind(null, callback));
}

// 2. Clear it in EVERY exit path
function onAllDone(newData, callback) {
  STATE.reloading = false;     // clear BEFORE installing new state
  STATE.myData = newData;
  if (callback) callback(true);
}

function onListResponse(callback, resp, err) {
  if (err) {
    STATE.reloading = false;                       // error exit
    if (callback) callback(false);
    return;
  }
  if (!resp || !resp.keys || !resp.keys.length) {
    STATE.reloading = false;                       // empty-list exit
    STATE.myData = {};
    if (callback) callback(true);
    return;
  }
  processKeysSequentially(resp.keys, {}, callback, 0);
}

// 3. Defer any handler that reads the shared state
function handleIncomingEvent(topic, payload) {
  if (STATE.reloading) {
    queueTask(function() { handleIncomingEvent(topic, payload); });
    return;
  }
  // ... normal processing using STATE.myData
}
```

**Why `queueTask` rather than polling the flag:** `onAllDone` itself runs inside a task-queue slot,
because the last step of `processKeysSequentially` enqueues it. Anything queued *during* the reload
lands behind it in the same FIFO, so the deferred event is guaranteed to run after the flag clears
and the new state is installed — by ordering, with no timing assumption.

Checklist for any async state rebuild:

- [ ] flag set at the **top** of the function that starts the chain
- [ ] flag cleared in **every** exit: normal completion, empty-result early return, error return
- [ ] every handler reading that state checks the flag and re-queues via `queueTask`
- [ ] the completion step clears the flag **before** installing new state, so deferred events see it

---

## MQTT.publish has no callback

`MQTT.publish(topic, payload, qos, retain)` takes **exactly 4 parameters** and returns a boolean. A
function passed as a 5th argument is silently ignored and never invoked.

```javascript
// BROKEN — continuation never runs
MQTT.publish(topic, payload, 0, false, function(success) { doNextStep(); });

// CORRECT — fire and forget, sequence through the queue
MQTT.publish(topic, payload, 0 /*at-most-once*/, false /*dont-retain*/);
queueTask(function() { doNextStep(); });
```

---

## Resource limits

Per script:

- **5 timers** — the task queue's drain timer is one of them
- **5 event subscriptions**
- **5 status change subscriptions**
- **5 concurrent RPC calls** — both nesting depth and for-loop dispatch consume this
- **10 MQTT topic subscriptions**
- **5 registered HTTP endpoints**

Per device, scripts that may be enabled simultaneously — hardware dependent, add rows as you meet
them:

| Model | Max enabled scripts |
|---|---|
| Shelly 1 Mini G3 (`shelly1minig3`) | 3 |

Exceeding it returns `"Reached the maximum N of enabled scripts"` (error code -108).

**Do not block.** Scripts share the main system task with firmware; a long loop causes
communication failures and device crashes. If a script crashes the device, the system disables that
script at next boot.

---

## Lifecycle logging

Always log start, initialization complete, and stop:

```javascript
log("Script starting...");
// ... init ...
log("Script initialization complete");

Shelly.addEventHandler(function(eventData) {
  if (eventData && eventData.info && eventData.info.event === "script_stop") {
    log("Script stopping");
  }
});
```

`log()` is gated on `CONFIG.enableLogging`. A diagnostic that must always be visible has to use
`print()` directly.

---

## Emitting events for operators

Call `Shelly.emitEvent(name, data)` for any change a human operator might want to know about. These
flow through the device's standard `NotifyEvent` MQTT notification (`<device_id>/events/rpc`), are
picked up by `internal/myhome/shelly/gen2/listener.go`, and land in the events database where the
web UI and `myhome ctl events` can show them.

**Rule of thumb: if it would make sense as a line in a physical maintenance logbook, emit it.**

| Category | Examples | Severity |
|---|---|---|
| Schedule decisions | daily run-window computed, summer/winter mode selected | `info` |
| Start/stop | pump started at speed X, pump stopped | `info` |
| Safety actions | water-supply protection activated, anti-cycling fuse tripped | `warn` |
| Restoration | pump restored after water supply turned off | `info` |

```javascript
// Fire-and-forget — does NOT count against the 5-concurrent-RPC limit.
Shelly.emitEvent("pool.run_window", {
  mode: "summer",
  max_temp_c: 28.5,
  run_hours: 7.3,
  start_h: 9.5,   // fractional hour — 9.5 means 09:30
  stop_h: 16.8
});
```

The second argument is stored **nested under a `"data"` key** in the event's `data` column — proven
by captured real-device traffic in
`pkg/shelly/mqtt/testdata/notify_event__shelly1minig3__script_3__remote_button_event.json`:

```json
{
  "component": "script:1",
  "event": "pool.run_window",
  "id": 1,
  "ts": 1234567890,
  "data": { "mode": "summer", "max_temp_c": 28.5, "run_hours": 7.3, "start_h": 9.5, "stop_h": 16.8 }
}
```

**Naming:** `<script_name>.<verb>`, lowercase with underscores — `pool.pump_start`,
`pool.fuse_tripped`. Use the script's `SCRIPT_NAME` constant as the prefix so events group in the
UI. Name the specific occurrence, not a generic noun: `pool.pump_start`, never `pool.event`.

**Payloads:** enough context to understand the event without cross-referencing other state.
`snake_case` fields. Units in the name where non-obvious (`max_temp_c`, `run_hours`, not `temp`,
`run`). No timestamps — the event already has `ts`. Keep under ~200 bytes; no arrays or large
structures.

**Severity** is assigned in `listener.go:severityFor()`. New names default to `"info"`. If your event
represents a degraded state or a safety interrupt, add it there with `"warn"` or `"alarm"` **in the
same commit as the JS change**.
