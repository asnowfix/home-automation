---
name: shelly
description: Shelly device work in this project — writing or reviewing Shelly JavaScript, uploading scripts, reading or writing KVS, measuring script memory, capturing device debug output, and running experiments against live hardware. Use this whenever you touch a .js file under internal/shelly/scripts/, call any Shelly RPC method, plan a test involving a physical device, or debug a script crash. The constraints here are non-obvious, empirically measured, and several have taken down the production pool pump for a full day. Do not write Shelly script code from general JavaScript knowledge — the interpreter is a modified Espruino whose limits no emulator reproduces.
---

# Shelly Devices

These devices run a real house. `filtration-hiver` is the **production pool pump**; if its script
dies, the pool does not filter, and every Schedule job on the device silently becomes a no-op
because they all dispatch through `script.eval`. A crash is not a failed test — it is a day of lost
filtration.

Everything below was measured on real hardware, usually after something broke. The emulator
(`goja`, in `internal/shelly/scripts`) does not reproduce the stack, heap, or timing limits that
cause these failures, so **a green test suite is not evidence that a script will run on a device.**

---

## Before you touch a device

Read `references/field-discipline.md` in full before your first live experiment. The short version:

- **One experiment per device at a time.** Two workstreams against `mezzanine`, and two `make test`
  runs on one machine, each produced hours of confounded results that had to be thrown away.
- **A result on one device does not transfer to the other.** PR #477 bought enough stack margin to
  pass on `mezzanine` and still failed on `filtration-hiver`.
- **Check what is actually installed before probing it.** `mem_used`/`mem_peak`/`mem_free` from
  `Script.GetStatus` fingerprint a build; `Script.GetCode` can be grepped for a version marker.
- **A `Script.Start` on `mezzanine` switches its relay ON**, because it restores the saved active
  output. Never restart it casually.
- **Measured, not remembered.** Report what you observed this session. If you are repeating a number
  from an issue, say so.

Device authorizations are bounded and explicit — which devices, which operations, which hours. They
live in `references/field-discipline.md`. Anything not listed there is not authorized.

---

## The kill list

Each of these has actually killed a running script. They are cheap to obey and expensive to
rediscover.

1. **Never send an unwrapped `Script.Eval`** — including a "quick" ad-hoc probe. An uncaught throw
   terminates the whole script. Always
   `(function(){try{ ... }catch(e){return "ERR:"+e}})()`. The return value survives wrapping, so
   there is no case where skipping it is justified. This killed the production pump once and
   `mezzanine` twice.
2. **Never leave a callback unwrapped.** A throw inside any `addEventHandler`, `addStatusHandler`,
   `MQTT.subscribe` callback, or a queued task kills the script exactly the same way. Wrap the body
   **in place**, not with a higher-order function — a wrapping HOF adds a call frame to every
   dispatch, and these devices die of stack depth.
3. **Never call anything on the line after `MQTT.subscribe()`.** It reliably overflows the stack
   even with ~23 KB of heap free. Put the log line *before* the subscribe.
4. **Define functions before use.** There is no hoisting. This applies to callback references too.
5. **Never write an empty `catch (e) {}`.** The minifier turns it into `catch {}`, which the engine
   rejects. Reference the parameter: `catch (e) { if (e && false) {} }`.
6. **`mem_peak` DOES reset when the script restarts — but it is only a measurement once init has
   settled.** Measured 2026-08-16 on two Pro1s, restarting the script made `mem_peak` *decrease*
   (mezzanine 21644 → 21420, filtration-hiver 21644 → 21448), which a monotonic high-water mark
   cannot do. It then climbs back through init and settles ~20 s later, **above** where it started
   (21602 and 21826). So a reading at +5 s understates the peak, and a pre-restart reading is not
   comparable with a post-restart one unless both are settled. Reboot between arms anyway — it is
   still the only way to clear other scripts' drift from the shared pool.
7. **Debug output truncates at ~128 characters and is NUL-separated.** Write on-device diagnostics
   as several short lines. Do not use `netcat` to capture it — it silently stops recording.
8. **UDP debug degrades the device and causes crashes on its own.** Enable it around a specific
   experiment, disable it after.

Rules 1, 2 and 3 are the ones that cost whole days. If you remember nothing else, remember that
**anything that can throw on a device must be wrapped where it throws.**

---

## Where to look

| You are doing this | Read |
|---|---|
| Writing or reviewing Shelly JavaScript | `references/scripting.md` |
| A script won't start, or you see `out_of_memory` | `references/memory.md` |
| Choosing where state lives, or naming a KVS key | `references/storage.md` |
| Calling RPC, uploading scripts, using the CLI or MCP tools | `references/api.md` |
| Running an experiment on live hardware, or capturing debug output | `references/field-discipline.md` |

Read the one that matches. They are independent — you do not need all five.

---

## The two constraints behind most of the rules

Nearly every rule above descends from one of two hard limits. Understanding them lets you predict
problems this document has not catalogued yet.

**The JS heap is one shared pool, ~23030 bytes, across every script on the device.** Not per-script.
So another script's footprint is your problem, `mem_peak` readings are device-wide, and a spare
device is only a valid proxy if it carries the same resident scripts *and* the same KVS
configuration — production's 24 config keys cost ~7.9 KB more runtime state than a test device's 5.
Aim for `mem_free` ≥ ~5 KB steady state; a script that boots with 1–2 KB free is one allocation from
dying unpredictably later. Details and the measurement protocol: `references/memory.md`.

**The interpreter's stack is shallow and its concurrency budget is 5.** Five timers, five event
subscriptions, five concurrent RPCs, ten MQTT subscriptions. Nested anonymous functions beyond 2–3
levels crash. `Shelly.call` in a `for` loop exhausts the RPC budget with zero nesting depth, because
it returns immediately and the loop dispatches every iteration before any response arrives. The
answer to almost all of this is the **task queue**: one recurring timer draining a FIFO, which
replaces per-operation timers, sequences async work without nesting, and gives you a deferral
mechanism with no timing assumptions. Pattern and its several uses: `references/scripting.md`.

---

## Quick commands

Full CLI and RPC reference in `references/api.md`. The ones you need constantly:

```bash
B="myhome ctl shelly call -B tcp://<broker>:1883 -T 60s <device>"

$B Script.GetStatus '{"id":N}'      # running? mem_used/mem_peak/mem_free — also a build fingerprint
$B Script.List      '{}'            # which scripts exist, and their ids
$B Schedule.List    '{}'            # a script with no schedule jobs will never fire

myhome ctl shelly script upload <device> <script.js> --no-minify   # always --no-minify when debugging
myhome ctl shelly script probe  <device> <script>                  # settled mem_peak measurement
```

`KVS.Get` and `KVS.GetMany` **cannot unmarshal numeric or boolean values** (#468) — read them out of
the error payload.
