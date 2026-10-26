# Storage — where state lives

Contents:
- [Three mechanisms, three purposes](#three-mechanisms-three-purposes)
- [The common mistake](#the-common-mistake)
- [The load-and-cache pattern](#the-load-and-cache-pattern)
- [KVS key naming](#kvs-key-naming)

---

## Three mechanisms, three purposes

Choosing wrong here does not fail loudly — it produces configuration that silently cannot be changed
from outside, or state that silently vanishes on restart.

**`Script.storage` — script-internal persistent data**

- **For:** data managed ONLY by the script itself.
- **API:** `Script.storage.getItem(key)` / `Script.storage.setItem(key, value)`.
- **Example:** `heater.js` stores `coolingRate` and `forecastUrl` — values it learned or computed.
- **Not accessible** from external commands or other scripts.
- **Persists** across script restarts and device reboots.
- **Not covered by a KVS copy.** `STATE.scheduleMode` lives here, which is why copying a device's
  entire KVS onto a spare does not reproduce its behaviour, and why writing a `schedule-mode` KVS
  key has no effect.

**KVS — external configuration**

- **For:** data set by EXTERNAL commands and tools.
- **API:** `Shelly.call('KVS.Get', {key: key}, cb)` / `Shelly.call('KVS.Set', {key: key, value: v}, cb)`.
- **Example:** follow configurations set by `myhome ctl follow blu`.
- **Accessible** from external tools and other scripts.
- **Persists** across script restarts and device reboots.
- **Costs heap.** Config-driven runtime state is real: 24 `script/pool-pump/*` keys cost ~7.9 KB more
  than 5 did. See `memory.md`.
- `KVS.Get` and `KVS.GetMany` **cannot unmarshal numeric or boolean values** (#468) — read them out
  of the error payload.

**In-memory variables — runtime cache**

- **For:** fast access to data loaded from KVS or `Script.storage`.
- **Example:** `var FOLLOWS = {}` in `blu-listener.js`, `blu-publisher.js`, `status-listener.js`.
- **Populated by** a loader such as `loadFollowsFromKVS()`, refreshed on KVS change events.
- **Does NOT persist** across script restarts.

---

## The common mistake

**Using `Script.storage` for data that should come from KVS.** This breaks external configuration,
because `Script.storage` is script-private — the value cannot be read or set by `myhome ctl`, by
another script, or by you during an experiment. The symptom is a setting that appears to be ignored:
you write the KVS key, nothing changes, and the script keeps using its own private copy.

Ask: *who writes this value?* If the answer includes anything outside the script, it belongs in KVS.

---

## The load-and-cache pattern

The architecture for follow-style scripts:

1. An external command sets KVS: `myhome ctl follow blu device mac` → key `follow/shelly-blu/<mac>`.
2. The script loads on startup: `loadFollowsFromKVS()` reads KVS and populates `FOLLOWS`.
3. The script watches for changes: a KVS change event re-runs the loader.
4. The script reads `FOLLOWS` in the hot path — fast, in-memory, no RPC.

```javascript
// In-memory cache (fast access)
var FOLLOWS = {};

function loadFollowsFromKVS() {
  Shelly.call('KVS.List', {}, function(result, error_code, error_message) {
    if (error_code !== 0) {
      log('KVS.List failed:', error_message);
      return;
    }
    var keys = result.keys || [];
    for (var i = 0; i < keys.length; i++) {
      if (keys[i].indexOf('follow/') === 0) {
        loadSingleKey(keys[i]);
      }
    }
  });
}

function loadSingleKey(key) {
  Shelly.call('KVS.Get', {key: key}, function(result, error_code, error_message) {
    if (error_code === 0 && result && result.value) {
      var data = JSON.parse(result.value);
      FOLLOWS[key] = data;
    }
  });
}

// Watch for KVS changes
Shelly.addEventHandler(function(eventData) {
  if (eventData && eventData.info && eventData.info.component === 'kvs') {
    loadFollowsFromKVS();  // Reload cache
  }
});

function getFollows() {
  return FOLLOWS;
}
```

Two things this sketch omits that a real implementation needs — see `scripting.md`:

- The `for` loop dispatching one `Shelly.call` per key will exhaust the 5-concurrent-RPC budget on
  any non-trivial key count. Route each iteration through `queueTask`.
- Any handler reading `FOLLOWS` while this chain is in flight sees half-built state. Use the
  `reloading` guard flag and defer through `queueTask`.

---

## KVS key naming

**Keys use only lowercase letters, digits, hyphens and slashes: `[0-9a-z-/]`.**

Hierarchical, slash-separated:

```
script/<script-name>/<purpose>     # script-owned data
follow/<category>/<identifier>     # follow configuration
state/<category>/<identifier>      # follow state
```

Examples:

- `script/heater/config` — main configuration for the heater script
- `script/heater/cooling-rate` — learned cooling-rate coefficient
- `script/heater/last-cheap-end` — temperature at end of the cheap window
- `follow/shelly-blu/e8:e0:7e:d0:f9:89` — BLU device follow configuration
- `state/shelly-blu/e8:e0:7e:d0:f9:89` — BLU device state data

Rules:

1. **Hyphens, not underscores** — `cooling-rate` ✓, `cooling_rate` ✗
2. **Lowercase only** — `script/heater/config` ✓, `script/Heater/Config` ✗
3. **Slashes for hierarchy** — `script/heater/config` ✓, `script.heater.config` ✗
4. **Descriptive but concise** — `last-cheap-end` ✓, `last_cheap_electricity_window_end_time` ✗
5. **Consistent prefixes** — script keys start `script/`, follow keys `follow/`

This buys namespace isolation between scripts, prefix-based discovery, URL-safety without encoding,
and no surprises on case-insensitive filesystems.
