# The JS Heap — the binding constraint on large scripts

Contents:
- [One shared pool](#one-shared-pool)
- [mem_peak decides survival](#mem_peak-decides-survival)
- [Measuring one script's footprint](#measuring-one-scripts-footprint)
- [mem_peak is a high-water mark — reboot between arms](#mem_peak-is-a-high-water-mark--reboot-between-arms)
- [The cost of HTTP.GET + JSON.parse](#the-cost-of-httpget--jsonparse)
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
