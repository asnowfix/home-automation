// pool-pump.js
// ------------
//
// Unified pool pump control. Exactly ONE controller is wired to the pump at a
// time, and it never coordinates with, checks for, or commands another: having
// one controller drive another is an electrical anti-pattern. The multi-output
// design is kept because a Pro3 drives three speed stages, but only one device
// is ever connected.
//
// Pro3 (3-switch variator):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pumps)
//   - Input 1: High-water sensor (MQTT notification)
//   - Input 2: Max speed active (MQTT notification)
//   - Switch 0-2: Pump speed stages (eco/day/max, configurable via KVS)
//   - Button: Cycles through speeds
//
// Pro1 (1-switch):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pump)
//   - Input 1: High-water sensor (MQTT notification)
//   - Switch 0: Pump on/off
//   - Button: Toggles on/off
//
// Features:
//   - Schedule-driven automation: daily-check (sunrise), morning-start (SR+3h),
//     evening-stop (sunset), night-start (23:15), night-stop (00:15)
//   - Summer/winter mode based on weather forecast (Open-Meteo)
//   - Water supply protection with speed restoration
//   - Cross-device safety: grace delay prevents Pro3 and Pro1 from running simultaneously
//   - Physical button cycling with detached input mode

// === STATIC CONSTANTS ===
var SCRIPT_NAME = "pool-pump";
var CONFIG_KEY_PREFIX = "script/" + SCRIPT_NAME + "/";
var SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";

// Configuration schema
// Both Pro3 and Pro1 run this same script with shared KVS configuration
// Script compares preferred_device_id against its own device ID to decide if it should run
// >>> GENERATED: CONFIG_SCHEMA (source: schema JSON; regenerate via `make generate` — DO NOT EDIT BY HAND) >>>
var CONFIG_SCHEMA = {
  // Enable logging when true
  enableLogging: {
    key: "logging",
    default: true,
    type: "boolean"
  },
  // MQTT topic prefix (written by CLI, not used by script)
  mqttTopicPrefix: {
    key: "mqtt-topic",
    default: "pool/pump",
    type: "string",
    cliOnly: true
  },
  // Speed: 'eco', 'day', 'max', 'max'. Maps to switches based on device capabilities
  preferredSpeed: {
    key: "speed",
    default: "eco",
    type: "string"
  },
  // Pro3 switch ID for eco/low speed (0, 1, or 2)
  ecoSpeed: {
    key: "eco-speed",
    default: 2,
    type: "number"
  },
  // Pro3 switch ID for day speed (0, 1, or 2)
  daySpeed: {
    key: "day-speed",
    default: 1,
    type: "number"
  },
  // Pro3 switch ID for max speed (0, 1, or 2)
  maxSpeed: {
    key: "max-speed",
    default: 0,
    type: "number"
  },
  // Night run duration in ms (written by CLI, not used by script)
  nightRunDurationMs: {
    key: "night-duration",
    default: 3600000,
    type: "number",
    cliOnly: true
  },
  // Temperature threshold (°C) for summer mode (day schedule)
  temperatureThreshold: {
    key: "temp-threshold",
    default: 20,
    type: "number"
  },
  // Pool volume in m³
  poolVolume: {
    key: "pool-volume",
    default: 46,
    type: "number"
  },
  // Daily turnover target (number of full pool volumes to filter per day)
  turnover: {
    key: "turnover",
    default: 5,
    type: "number"
  },
  // Pump max flow rate in m³/h at max RPM
  maxFlowRate: {
    key: "max-flow-rate",
    default: 31,
    type: "number"
  },
  // Pump rated max RPM
  maxRpm: {
    key: "max-rpm",
    default: 2900,
    type: "number"
  },
  // Variator RPM setting for eco speed
  ecoRpm: {
    key: "eco-rpm",
    default: 2000,
    type: "number"
  },
  // Variator RPM setting for day speed
  dayRpm: {
    key: "day-rpm",
    default: 2600,
    type: "number"
  },
  // Temperature (°C) at which run time reaches one full turnover
  maxTemp: {
    key: "max-temp",
    default: 35,
    type: "number"
  },
  // Enable solar-driven start/stop via the daemon's solar-available MQTT event
  solarEnabled: {
    key: "solar-enabled",
    default: false,
    type: "boolean"
  },
  // Available solar power (W) required to trigger a solar start
  solarStartThresholdW: {
    key: "solar-start-w",
    default: 500,
    type: "number"
  },
  // Available solar power (W) below which a solar-driven run stops
  solarStopThresholdW: {
    key: "solar-stop-w",
    default: 200,
    type: "number"
  },
  // Solar must hold above start threshold this long (ms) before starting
  solarStartDelayMs: {
    key: "solar-start-delay",
    default: 300000,
    type: "number"
  },
  // Solar must hold below stop threshold this long (ms) before stopping
  solarStopDelayMs: {
    key: "solar-stop-delay",
    default: 600000,
    type: "number"
  },
  // Soft-stop target (pool volumes/day); solar keeps running past this while solar remains available
  solarMinTurnover: {
    key: "solar-min-turnover",
    default: 5,
    type: "number"
  },
  // Hard ceiling (pool volumes/day); pump always stops (and won't solar-start) once reached
  solarMaxTurnover: {
    key: "solar-max-turnover",
    default: 7,
    type: "number"
  },
  // Treat myhome/energy/solar/available as stale (fall back to schedule only) after this long (ms) without a message
  solarStaleMs: {
    key: "solar-stale-ms",
    default: 300000,
    type: "number"
  },
  // How long a manual override (button press, or an out-of-band switch change) holds the pump against the schedule/solar policy (ms)
  overrideMs: {
    key: "override-ms",
    default: 7200000,
    type: "number"
  }
};
// <<< GENERATED: CONFIG_SCHEMA <<<

// Component names by device type (inputs are static; switch names are built
// dynamically from CONFIG speed mapping — see buildSwitchNames)
var COMPONENT_NAMES = {
  "pro3": {
    inputs: [
      {id: 0, name: "water-supply", invert: true},
      {id: 1, name: "high-water", invert: false},
      {id: 2, name: "max-speed-active", invert: false}
    ]
  },
  "pro1": {
    inputs: [
      {id: 0, name: "water-supply", invert: true},
      {id: 1, name: "high-water", invert: false}
    ]
  }
};

// Build switch name list from CONFIG speed-to-switch mapping.
// Called after config is loaded so CONFIG.ecoSpeed etc. are available.
function buildSwitchNames() {
  if (STATE.deviceType === "pro3") {
    var names = [];
    names.push({id: CONFIG.ecoSpeed, name: "pump-eco"});
    names.push({id: CONFIG.daySpeed, name: "pump-day"});
    names.push({id: CONFIG.maxSpeed, name: "pump-max"});
    return names;
  } else if (STATE.deviceType === "pro1") {
    return [{id: 0, name: "pump-max"}];
  }
  return [];
}

// Runtime configuration values (initialized from defaults)
var CONFIG = {};

// Initialize CONFIG with defaults immediately so logging works.
// (initConfig() inlined -- single call site, measure/inline-single-callsite.)
for (var initConfigKey in CONFIG_SCHEMA) {
  CONFIG[initConfigKey] = CONFIG_SCHEMA[initConfigKey].default;
}

// Load configuration from KVS and validate required fields
function loadConfig(callback) {
  log("Loading configuration from KVS...");
  
  var missingRequired = [];
  var configKeys = [];
  
  // Build array of config keys to load (skip cliOnly — written by CLI, not needed at runtime)
  for (var key in CONFIG_SCHEMA) {
    if (!CONFIG_SCHEMA[key].cliOnly) {
      configKeys.push(key);
    }
  }
  
  var keyIndex = 0;
  
  // Process one key at a time using task queue
  function loadNextKey() {
    if (keyIndex >= configKeys.length) {
      // All keys loaded, validate
      if (missingRequired.length > 0) {
        log("ERROR: Missing required configuration:");
        for (var i = 0; i < missingRequired.length; i++) {
          log("  -", missingRequired[i]);
        }
        log("Script cannot start without required configuration.");
        log("Please run: ctl pool setup <device>");
        callback(false);
        return;
      }

      // Enumerate available outputs and inputs
      var availableOutputs = [];
      for (var oi = 0; oi < 4; oi++) {
        var swSt = Shelly.getComponentStatus("switch:" + oi);
        if (swSt && ("output" in swSt)) {
          availableOutputs.push(oi);
        }
      }
      STATE.outputs = availableOutputs;

      var availableInputs = [];
      for (var ii = 0; ii < 4; ii++) {
        var inSt = Shelly.getComponentStatus("input:" + ii);
        if (inSt && ("state" in inSt)) {
          availableInputs.push(ii);
        }
      }
      STATE.inputs = availableInputs;

      // Detect device type based on switch count
      if (availableOutputs.length >= 3) {
        STATE.deviceType = "pro3";
        log("Detected device type: Pro3 (3 switches)");
      } else if (availableOutputs.length === 1) {
        STATE.deviceType = "pro1";
        log("Detected device type: Pro1 (1 switch)");
      } else {
        STATE.deviceType = "unknown";
        log("WARNING: Could not detect device type");
      }
      log("Switches:", availableOutputs, "Inputs:", availableInputs);

      // Cache my device ID
      var deviceInfo = Shelly.getDeviceInfo();
      if (deviceInfo && deviceInfo.id) {
      } else {
        log("ERROR: Could not get device ID");
        callback(false);
        return;
      }

      log("Configuration loaded successfully");
      // Free schema object — only needed during KVS loading, not at runtime
      CONFIG_SCHEMA = null;
      callback(true);
      return;
    }
    
    var key = configKeys[keyIndex];
    var schema = CONFIG_SCHEMA[key];
    var kvsKey = CONFIG_KEY_PREFIX + schema.key;
    keyIndex++;
    
    // Load from KVS asynchronously
    Shelly.call("KVS.Get", {key: kvsKey}, function(result, err) {
      if (err) {
        log("WARNING: KVS.Get failed for", kvsKey, ":", err, "- using default");
        CONFIG[key] = schema.default;
        if (schema.required && CONFIG[key] === null) {
          missingRequired.push(key + " (" + kvsKey + ") - KVS error: " + err);
        }
        queueTask(loadNextKey);
        return;
      }

      if (result && ("value" in result) && result.value !== null && result.value !== "") {
        var value = result.value;
        
        // Parse value based on type
        if (schema.type === "boolean") {
          CONFIG[key] = value === "true" || value === true;
        } else if (schema.type === "number") {
          var num = Number(value);
          if (!isNaN(num)) {
            CONFIG[key] = num;
          } else {
            log("WARNING: Invalid number for", key, ":", value);
            CONFIG[key] = schema.default;
          }
        } else {
          CONFIG[key] = value;
        }
      } else {
        // Use default
        CONFIG[key] = schema.default;
        
        // Check if required
        if (schema.required && CONFIG[key] === null) {
          missingRequired.push(key + " (" + kvsKey + ")");
        }
      }
      
      // Queue next key
      queueTask(loadNextKey);
    });
  }
  
  loadNextKey();
}


// Script.storage keys for continuously evolving values (survives reboots, synchronous)
var STORAGE_KEYS = {
  forecastUrl:   "forecast-url",    // Open-Meteo forecast URL built from device location
  scheduleMode:  "schedule-mode",   // "summer" or "winter" — moved here from KVS because
                                    // Script.storage.getItem() is synchronous; KVS.Get is
                                    // async-only, so Shelly.call without a callback always
                                    // returns null and schedule mode was lost on every reboot
  runtime:       "runtime",         // #469: single JSON object {sec, ts} — cumulative pump-on
                                    // seconds today plus the epoch second of the last update.
                                    // Replaces the old loose-scalar pair below: a serialised
                                    // object cannot be silently misparsed as a number, and the
                                    // day a count belongs to is derived from ts, never stored
                                    // or parsed as a date string.
  runtimeSecLegacy:  "runtime-sec",  // pre-#469 format, read once for migration then unused
  runtimeDateLegacy: "runtime-date", // pre-#469 format, read once for migration then unused
  runStart:      "run-start"        // #550: epoch ms of the currently-open run interval, or
                                    // "null" when none is open. Mirrors STATE.runStartTs so a
                                    // script outage spanning a transition can be reconciled
                                    // against real hardware at the NEXT boot -- see
                                    // enforceOutputState()/reconcileMissedStop().
};

// State keys for KVS persistence (fire-and-forget writes only; reads use Script.storage,
// except runtimeSec/runtimeTs below, which loadRuntimeState() reads back asynchronously
// as a recovery path when Script.storage itself yields nothing valid — see #469)
var STATE_KEYS = {
  activeOutput: "active-output",    // -1 (all off), 0, 1, 2 for pro3; 0 for pro1
  runtimeSec:   "runtime-sec",      // mirrors STATE.runtimeTodaySec, rounded (also read by
                                    // the Go daemon, see myhome/daemon/pool_notices.go)
  runtimeTs:    "runtime-ts"        // epoch second STATE.runtimeTodaySec applies to; KVS
                                    // survives script reinstalls and can be hand-repaired
};

// === IN-FLIGHT RPC TRACKING (#421 Bug A) ===
// Real Gen2 firmware allows at most 5 concurrent Shelly.call RPCs per script
// (see CLAUDE.md "Per-script limits"); the 6th concurrent call raises an
// uncaught "Too many calls in progress" exception that kills the ENTIRE
// script, not just the offending call. processTaskQueue's 200ms tick (below)
// only serialises *when* queued task functions run — it has no idea how many
// of the underlying async RPCs those tasks fire are still in flight. That gap
// is the root cause of the live crash (saveState's active-output mirror, a
// storeValue() fire-and-forget write) documented in issue #421.
//
// CALLS_IN_FLIGHT tracks every Shelly.call this script makes. It is wired up
// once here by wrapping Shelly.call itself, rather than editing every call
// site in this file — every callback still receives exactly the arguments it
// always did, so this is purely additive bookkeeping.
var CALLS_IN_FLIGHT = 0;
var N_SEEN = 0;               // lifetime count — proves tracking actually runs
var MAX_CALLS_IN_FLIGHT = 4;  // stay one below the real 5-call device ceiling

// CALL_SLOTS is a small fixed pool of plain {cb, ud, used} records, allocated
// ONCE at script load. Each Shelly.call claims a free slot in place instead
// of allocating a brand-new closure per call — a per-call closure measurably
// raised peak heap on real hardware in issue #421's reference fix. Sized to
// the real 5-call device ceiling + 1 spare, not just MAX_CALLS_IN_FLIGHT:
// calls fired directly from event/timer handlers (not funneled through
// queueTask) are not subject to that throttle.
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
  // Pool exhausted (should not happen — see sizing note above): fall back
  // to a fresh record rather than losing the callback.
  return {cb: cb, ud: ud, used: true};
}

// Shared completion handler for calls that passed a callback. Decrements
// unconditionally before invoking the caller's callback, so a slot is always
// freed even if the callback itself throws (caught by processTaskQueue's
// try/catch below, or top-level error handling).
// Completions waiting to be delivered from the task queue, and the index of
// the next one. A head index rather than [].shift(), which Espruino lacks.
var PENDING_DONE = [];
var PENDING_DONE_HEAD = 0;

// Deliver one queued completion. Runs from processTaskQueue, i.e. on a fresh
// stack, which is the whole point — see sharedCallDone.
function runPendingCallback() {
  if (PENDING_DONE_HEAD >= PENDING_DONE.length) return;
  var slot = PENDING_DONE[PENDING_DONE_HEAD];
  PENDING_DONE[PENDING_DONE_HEAD] = null;
  PENDING_DONE_HEAD++;
  if (PENDING_DONE_HEAD >= PENDING_DONE.length) {
    PENDING_DONE = [];
    PENDING_DONE_HEAD = 0;
  }
  if (!slot) return;

  var cb = slot.cb;
  var ud = slot.ud;
  var r = slot.r;
  var ec = slot.ec;
  var em = slot.em;
  // Release the slot before invoking, so a callback that issues another call
  // can reuse it.
  slot.used = false;
  slot.cb = null;
  slot.ud = null;
  slot.r = null;
  slot.em = null;
  if (typeof cb === 'function') {
    cb(r, ec, em, ud);
  }
}

// Shared completion handler for calls that passed a callback.
//
// The callback is delivered from the task queue, NOT inline (#450). Shelly.call
// is reassigned to a JS wrapper, and the firmware invokes this handler from
// inside the call it is completing; invoking the caller's callback here meant a
// callback that issues another call nested inside the previous completion
// instead of unwinding. A loop of dependent calls — turnOffAllSwitches issuing
// one Switch.Set per output, or applyOutput chaining setOutput — therefore
// accumulated stack depth with no recursion anywhere in the source, until
// acquireCallSlot (a plain for loop) ran out of stack:
//
//   Uncaught Error: Too much recursion - the stack is about to overflow
//    in function "acquireCallSlot" ... "call" ... "turnOffAllSwitches"
//
// That killed the production pump twice in three days, each time silently
// turning every schedule job into a no-op.
//
// Only chains deeper than MAX_CALL_DEPTH are deferred. Deferring every
// completion would be simpler, but it adds a task-queue tick to every single
// RPC, which slows init and the schedule handlers noticeably for no benefit:
// a short chain cannot overflow anything. Bounding the depth keeps the common
// path exactly as fast as before and makes the failure impossible regardless
// of which caller drives it.
//
// The result is stashed on the slot the call already owns rather than in a new
// object: this runs on every RPC, and per-call allocation is what the heap on
// this device can least afford.
var CALL_DEPTH = 0;
var MAX_CALL_DEPTH = 3;

function sharedCallDone(result, error_code, error_message, slot) {
  CALLS_IN_FLIGHT--;
  if (!slot) return;
  if (typeof slot.cb !== 'function') {
    slot.used = false;
    slot.cb = null;
    slot.ud = null;
    return;
  }

  if (CALL_DEPTH >= MAX_CALL_DEPTH) {
    slot.r = result;
    slot.ec = error_code;
    slot.em = error_message;
    PENDING_DONE.push(slot);
    queueTask(runPendingCallback);
    return;
  }

  var cb = slot.cb;
  var ud = slot.ud;
  slot.used = false;
  slot.cb = null;
  slot.ud = null;

  // If cb throws, the decrement is skipped and every later completion is
  // deferred instead of nested. Degraded but safe: slower, never deeper.
  CALL_DEPTH++;
  cb(result, error_code, error_message, ud);
  CALL_DEPTH--;
}

// Shared completion handler for fire-and-forget calls (storeValue() and
// friends) — nothing to forward, so no slot is needed at all.
function decrementOnlyCallDone() {
  CALLS_IN_FLIGHT--;
}

var RAW_CALL = Shelly.call;
Shelly.call = function (method, params, callback, userdata) {
  CALLS_IN_FLIGHT++;
  N_SEEN++;
  if (typeof callback !== 'function') {
    return RAW_CALL(method, params, decrementOnlyCallDone, null);
  }
  return RAW_CALL(method, params, sharedCallDone, acquireCallSlot(callback, userdata));
};

// --- Self-check: fail LOUDLY, never silently (#421) ---------------------
// This depends on reassigning a method on the native Shelly object, which is
// not guaranteed to be permitted on every firmware. If the assignment is
// ever rejected, CALLS_IN_FLIGHT stays 0 forever, the throttle below never
// engages, and the crash this fix exists to prevent comes back — while every
// emulator test still passes, because goja allows a reassignment a device
// might refuse. That silent-inert failure is the dangerous case, so it is
// asserted here rather than assumed.
//
// print() is used deliberately instead of log(): log() is gated on
// CONFIG.enableLogging and would hide this exact message in production, and
// log() is defined later in this file (no hoisting on Espruino) so it does
// not exist yet at this point anyway.
// The device truncates each UDP debug line at ~128 chars, so every line
// below is kept short.
var P421 = "[pool-pump] #421: ";

function printHit() {
  print(P421 + "throttle INERT; >5 calls kills whole script as-is.");
  print(P421 + "DO NOT re-investigate -- read issue #421 first.");
}

function printFail() {
  print(P421 + "FATAL: Shelly.call wrapper did NOT install.");
  printHit();
  print(P421 + "alt fix: storeValue() callback should drive queue.");
}

var WRAP_OK = (Shelly.call !== RAW_CALL);
if (!WRAP_OK) {
  printFail();
}

function emitBroken(reason) {
  Shelly.emitEvent("pool.call_tracking_broken", {
    reason: reason,
    calls_tracked: N_SEEN
  });
}

// Runtime counterpart to the check above: the assignment can succeed while
// tracking still never happens (e.g. something captured Shelly.call before
// this point and calls the original directly). A healthy boot makes many
// KVS.Get calls, so N_SEEN must be well above zero by the end of init.
// Called from continueInit(), by which time log()/Shelly.emitEvent() exist.
function checkTrack() {
  if (!WRAP_OK) {
    // Repeated here as well as at install time: the install-time prints fire
    // before a debug capture attached after boot would see anything.
    printFail();
    emitBroken("wrapper-not-installed");
    return;
  }
  if (N_SEEN === 0) {
    print(P421 + "FATAL: wrapper OK but tracked 0 calls -- something");
    print(P421 + "calls the unwrapped Shelly.call, captured earlier.");
    printHit();
    emitBroken("zero-calls-tracked");
    return;
  }
  log("Tracking OK (#421): seen", N_SEEN, "max", MAX_CALLS_IN_FLIGHT);
}

// === TASK QUEUE (SINGLE TIMER FOR ALL SEQUENTIAL OPERATIONS) ===
var TASK_QUEUE = [];
var TASK_INDEX = 0;
var TASK_TIMER = null;

function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
    // No tasks left — stop timer and reset so queueTask() can restart it later
    if (TASK_TIMER) {
      Timer.clear(TASK_TIMER);
      TASK_TIMER = null;
    }
    TASK_QUEUE = [];
    TASK_INDEX = 0;
    return;
  }

  // Execute next task; new tasks queued by the task itself extend TASK_QUEUE
  // and will be picked up on subsequent timer ticks.
  // #480: an uncaught throw inside a queued task used to kill the whole
  // script (verified live on mezzanine: queueTask(function(){ null.x })
  // stopped the script). Wrapping this single call site protects every
  // queueTask() call in the script.
  var task = TASK_QUEUE[TASK_INDEX];
  TASK_INDEX++;
  try {
    task();
  } catch (e) {
    log("queued task error:", e);
  }
}

function queueTask(task) {
  // Simply append to queue
  TASK_QUEUE.push(task);
  
  // Start timer only if not already running
  if (!TASK_TIMER) {
    TASK_TIMER = Timer.set(200, true, processTaskQueue);
  }
}

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // Device configuration (auto-detected at startup)
  deviceType: null,           // "pro3" or "pro1"
  outputs: [],                // Array of available output IDs
  inputs: [],                 // Array of available input IDs

  // Component name mappings
  inputNames: {},             // {id: name}
  switchNames: {},            // {id: name}

  // Current state
  activeOutput: -1,           // Current active output (-1 = all off)

  // MQTT connection
  mqttConnected: false,
  
  // Forecast cache (in-memory, refreshed daily) - cleared after use to save memory
  forecastUrl: null,          // Open-Meteo forecast URL
  maxForecastTemp: null,      // Only store max temp, not full array (memory optimization)
  lastForecastFetchDate: null,// Date string (YYYY-M-D) of last fetch
  peakForecastHour: null,      // Hour-of-day (0-23) of max temperature in today's forecast
  sunriseHour: null,           // Fractional hour of today's sunrise (from forecast API)
  sunsetHour: null,            // Fractional hour of today's sunset (from forecast API)

  // Schedule mode
  scheduleMode: null,         // "summer" or "winter"

  // Runtime/turnover tracking (on-device, for ctl pool status — see #402)
  runtimeTodaySec: 0,         // cumulative pump-on seconds today (completed intervals only)
  runtimeTs: null,            // epoch second (Date.now()/1000) runtimeTodaySec applies to —
                              // #469: the calendar day is derived from this, never from a
                              // stored date string (see reconcileRuntimeState)
  runStartTs: null,           // Date.now() when the current ON interval began; null when off
  // #547: periodic checkpointing while running used to ride a dedicated
  // Timer.set(60000, true, ...) -- on a 7h32m production run that timer
  // returned a live handle but never fired again after the first tick (or
  // never at all), so the whole run's accrual survived only in RAM until the
  // stop. It now rides the task queue's own 200ms drain (see
  // runtimeFlushTick()) instead of a second, independently-scheduled timer.
  runtimeFlushActive: false,  // true while the checkpoint chain is running
  runtimeFlushDueTs: 0,       // epoch ms of the next scheduled checkpoint

  // Initialization flag
  initializing: true          // Prevents KVS writes during init
};

// === LOGGING ===
function log() {
  if (!CONFIG.enableLogging) return;
  var s = "";
  for (var i = 0; i < arguments.length; i++) {
    try {
      var a = arguments[i];
      if (typeof a === "object") {
        s += JSON.stringify(a);
      } else {
        s += String(a);
      }
    } catch (e) {
      s += String(arguments[i]);
      if (e && false) {}
    }
    if (i + 1 < arguments.length) s += " ";
  }
  print(SCRIPT_PREFIX, s);
}

// === SCRIPT.STORAGE HELPERS ===
// #469: loadStorageValue() used to guess a value's type (Number, then
// JSON.parse, then raw string) and callers silently accepted whatever came
// back. A restore that landed on the wrong type (e.g. a date string
// misparsed as a number) became null/0 with no warning, and the daily pump
// runtime accounting reset itself on every ordinary script restart. Callers
// now say what type they expect via loadStorageNumber/String/Object, and
// every fallback to a default is logged.
function storeStorageValue(key, value) {
  var valueStr;
  if (typeof value === "undefined" || value === null) {
    valueStr = "null";
  } else if (typeof value === "number" || typeof value === "boolean") {
    valueStr = value.toString();
  } else if (typeof value === "object") {
    valueStr = JSON.stringify(value);
  } else {
    valueStr = String(value);
  }
  Script.storage.setItem(key, valueStr);
}

// Returns a number, or null (with a warning) if the key is absent or does
// not hold a valid number.
function loadStorageNumber(key) {
  var v = Script.storage.getItem(key);
  if (v === null || v === undefined || v === "null") return null;
  var num = Number(v);
  if (isNaN(num)) {
    log("WARNING: Script.storage[" + key + "] expected a number, got:", v);
    return null;
  }
  return num;
}

// Returns the raw string, or null (with a warning) if the key is absent.
// Script.storage follows Web Storage semantics — getItem() always returns
// either a string or null, so no type-guessing is needed or attempted here.
function loadStorageString(key) {
  var v = Script.storage.getItem(key);
  if (v === null || v === undefined || v === "null") return null;
  if (typeof v !== "string") {
    log("WARNING: Script.storage[" + key + "] expected a string, got:", v);
    return null;
  }
  return v;
}

// Returns a parsed JSON object, or null (with a warning) if the key is
// absent, not valid JSON, or parses to a non-object (e.g. a bare number).
function loadStorageObject(key) {
  var v = Script.storage.getItem(key);
  if (v === null || v === undefined || v === "null") return null;
  try {
    var obj = JSON.parse(v);
    if (obj !== null && typeof obj === "object") return obj;
    log("WARNING: Script.storage[" + key + "] expected an object, got:", v);
    return null;
  } catch (e) {
    log("WARNING: Script.storage[" + key + "] failed to parse as JSON:", v);
    if (e && false) {}
    return null;
  }
}

// === KVS HELPERS ===
// key is a bare suffix; the full KVS key is CONFIG_KEY_PREFIX + key
// ("script/pool-pump/" + key, 17 chars). Real Shelly firmware rejects any
// KVS key of 42 chars or more (-103 "length should be less than 42!"),
// confirmed on hardware (#537) -- so key must stay <= 24 chars for the full
// name to stay <= 41. This is not checked at runtime (no spare heap for
// live validation on this script, see CLAUDE.md); see
// TestPoolPump_KVSKeyLengthsUnderFirmwareLimit in pool_pump_test.go for the
// build/test-time guard instead.
function storeValue(key, value) {
  var valueStr;
  if (typeof value === "undefined" || value === null) {
    valueStr = "null";
  } else if (typeof value === "number" || typeof value === "boolean") {
    valueStr = value.toString();
  } else if (typeof value === "object") {
    valueStr = JSON.stringify(value);
  } else {
    valueStr = String(value);
  }
  // Fire-and-forget to avoid callback depth issues
  Shelly.call("KVS.Set", {key: CONFIG_KEY_PREFIX + key, value: valueStr});
}

// Async KVS read (KVS.Get has no synchronous form — Shelly.call without a
// callback returns null on real firmware, see STORAGE_KEYS.scheduleMode
// comment below). cb receives (value) — a number if the stored string
// parses as one, else the raw string, or null if the key is missing/errored.
function loadValueAsync(key, cb) {
  Shelly.call("KVS.Get", {key: CONFIG_KEY_PREFIX + key}, function (result, error_code, error_message) {
    if (error_code !== 0 || !result || !("value" in result)) {
      if (error_message && false) {}
      cb(null);
      return;
    }
    var v = result.value;
    if (v === "null" || v === "undefined") {
      cb(null);
      return;
    }
    var num = Number(v);
    if (!isNaN(num)) {
      cb(num);
      return;
    }
    cb(v);
  });
}

// Fractional-day-free "YYYY-M-D" date string, used for day-rollover checks
// (forecast refresh, runtime/turnover day reset).
function todayDateString() {
  var now = new Date();
  return now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
}

// Comparable local-calendar-day number (e.g. 20260811) for a given epoch
// second. Used instead of a formatted/parsed date string (#469) so day
// comparisons never depend on round-tripping a string through
// Script.storage — only the number ts itself is persisted.
function localDayNumber(epochSec) {
  var d = new Date(epochSec * 1000);
  return d.getFullYear() * 10000 + (d.getMonth() + 1) * 100 + d.getDate();
}

// === WEATHER FORECAST FUNCTIONS (Memory-Optimized) ===
function setForecastURL(lat, lon) {
  log('setForecastURL', lat, lon);
  if (lat !== null && lon !== null) {
    var url = 'https://api.open-meteo.com/v1/forecast?latitude=' + lat + '&longitude=' + lon + '&hourly=temperature_2m&daily=sunrise,sunset&forecast_days=1&timezone=auto';
    STATE.forecastUrl = url;
    storeStorageValue(STORAGE_KEYS.forecastUrl, url);
    log('Forecast URL ready');
  }
}

function onForecast(result, error_code, error_message, cb) {
  if (error_code !== 0) {
    log('Forecast fetch error code:', error_code, 'message:', error_message);
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  if (!result || !result.body) {
    log('No forecast data in response');
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  var data = null;
  try {
    data = JSON.parse(result.body);
  } catch (e) {
    log('JSON parse error');
    if (e && false) {}
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  // Clear result to free memory immediately
  result = null;

  if (!data || !data.hourly || !data.hourly.temperature_2m || data.hourly.temperature_2m.length === 0) {
    log('Invalid forecast structure');
    data = null;
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  // Extract peak hour and max temp from hourly data; discard array immediately after
  var temps = data.hourly.temperature_2m;
  var maxTemp = null;
  var peakHour = 12;
  for (var i = 0; i < temps.length; i++) {
    var temp = temps[i];
    if (temp !== null && (maxTemp === null || temp > maxTemp)) {
      maxTemp = temp;
      peakHour = i;
    }
  }
  temps = null;
  STATE.maxForecastTemp = maxTemp;
  STATE.peakForecastHour = peakHour;

  // Parse sunrise/sunset from daily section (added to URL for schedule centering)
  if (data.daily && data.daily.sunrise && data.daily.sunrise.length > 0) {
    STATE.sunriseHour = parseHourFromISO(data.daily.sunrise[0]);
  }
  if (data.daily && data.daily.sunset && data.daily.sunset.length > 0) {
    STATE.sunsetHour = parseHourFromISO(data.daily.sunset[0]);
  }
  data = null;

  STATE.lastForecastFetchDate = todayDateString();
  log('Forecast cached, max temp:', maxTemp);

  if (typeof cb === 'function') {
    queueTask(function() { cb(); });
  }
}

function onDeviceLocation(result, error_code, error_message, cb) {
  if (error_code === 0 && result) {
    if (result.lat !== null && result.lon !== null) {
      log('Auto-detected location: lat=' + result.lat + ', lon=' + result.lon);
      setForecastURL(result.lat, result.lon);
      if (typeof cb === 'function') queueTask(function() { cb(); });
    } else {
      log('Location detection returned null coordinates');
      if (typeof cb === 'function') queueTask(function() { cb(); });
    }
  } else {
    log('Location detection error:', error_code, error_message);
    if (typeof cb === 'function') queueTask(function() { cb(); });
  }
}

// === COMPONENT NAMING ===
function configureComponentNames() {
  log("Configuring component names...");
  
  var names = COMPONENT_NAMES[STATE.deviceType];
  if (!names) {
    log("ERROR: No component names defined for device type:", STATE.deviceType);
    return;
  }
  
  // Build name mappings (synchronous)
  for (var i = 0; i < names.inputs.length; i++) {
    var input = names.inputs[i];
    if (STATE.inputs.indexOf(input.id) !== -1) {
      STATE.inputNames[input.id] = input.name;
    }
  }

  // Switch names derived from CONFIG speed mapping
  var switchNames = buildSwitchNames();
  for (var i = 0; i < switchNames.length; i++) {
    var sw = switchNames[i];
    if (STATE.outputs.indexOf(sw.id) !== -1) {
      STATE.switchNames[sw.id] = sw.name;
    }
  }
}

function applyComponentNames(callback) {
  log("Applying component names to device...");
  
  var names = COMPONENT_NAMES[STATE.deviceType];
  if (!names) {
    if (callback) callback();
    return;
  }
  
  // Build list of components to configure
  var componentsToConfig = [];
  
  for (var i = 0; i < names.inputs.length; i++) {
    var input = names.inputs[i];
    if (STATE.inputs.indexOf(input.id) !== -1) {
      componentsToConfig.push({
        type: "input", 
        id: input.id, 
        name: input.name,
        invert: input.invert
      });
    }
  }
  
  // Switch names derived from CONFIG speed mapping
  var switchNames = buildSwitchNames();
  for (var i = 0; i < switchNames.length; i++) {
    var sw = switchNames[i];
    if (STATE.outputs.indexOf(sw.id) !== -1) {
      componentsToConfig.push({type: "switch", id: sw.id, name: sw.name});
    }
  }
  
  if (componentsToConfig.length === 0) {
    log("No components to configure");
    if (callback) callback();
    return;
  }
  
  // Process components sequentially using task queue
  var index = 0;
  
  function processNext() {
    if (index >= componentsToConfig.length) {
      log("All component names applied");
      // Free static data — only needed during component setup
      COMPONENT_NAMES = null;
      if (callback) callback();
      return;
    }
    
    var comp = componentsToConfig[index];
    index++;
    
    if (comp.type === "input") {
      var config = {name: comp.name};
      if (typeof comp.invert === "boolean") {
        config.invert = comp.invert;
      }
      Shelly.call("Input.SetConfig", {id: comp.id, config: config}, function(res, err) {
        if (err && false) {}
        log("Applied input:" + comp.id + " config:", JSON.stringify(config));
        queueTask(processNext);
      });
    } else if (comp.type === "switch") {
      Shelly.call("Switch.SetConfig", {id: comp.id, config: {name: comp.name, in_mode: "detached"}}, function(res, err) {
        if (err && false) {}
        log("Applied switch:" + comp.id + " name:", comp.name, "in_mode: detached");
        queueTask(processNext);
      });
    }
  }
  
  processNext();
}

// === STATE PERSISTENCE ===
// loadState(onDone) restores STATE from persisted storage and calls onDone()
// when finished. It is synchronous in the common case (Script.storage has a
// valid {sec, ts} object) and calls onDone() immediately, before returning.
// It only goes truly asynchronous when Script.storage yields nothing usable
// at all, in which case it falls back to an async KVS read before calling
// onDone() (#469) — see loadRuntimeStateFromStorage/FromKVS below.
function loadState(onDone) {
  log("Loading persisted state...");

  // runtimeTodaySec/runtimeTs: restored before enforceOutputState() decides
  // whether to resume runtime accounting for a run already in progress.
  var fromStorage = loadRuntimeStateFromStorage();
  if (fromStorage !== null) {
    // #533: hoisted out of applyRuntimeState()'s argument list -- on an
    // AST-walking interpreter, evaluating reconcileRuntimeState() as an
    // argument expression runs it one frame deeper than a plain statement at
    // the same nominal call depth (see ensureRuntimeDay() below, which
    // already does this and has never crashed).
    var reconciledFromStorage = reconcileRuntimeState(fromStorage.sec, fromStorage.ts, "Script.storage");
    applyRuntimeState(reconciledFromStorage);
    finishLoadState(onDone);
    return;
  }

  log("WARNING: Script.storage has no valid runtime state, falling back to KVS (#469)");
  loadRuntimeStateFromKVS(function (kvsSec, kvsTs) {
    var reconciledFromKVS = reconcileRuntimeState(kvsSec, kvsTs, "KVS");
    applyRuntimeState(reconciledFromKVS);
    finishLoadState(onDone);
  });
}

// Commits a reconciled {sec, ts} result to STATE and re-persists it in the
// current (Script.storage) format immediately — this stabilizes a migrated
// legacy value or a KVS-recovered value on disk without waiting for the next
// checkpoint.
function applyRuntimeState(state) {
  STATE.runtimeTodaySec = state.sec;
  STATE.runtimeTs = state.ts;
  persistRuntimeState(STATE.runtimeTodaySec);
  log("Restored runtime today:", STATE.runtimeTodaySec, "s, ts:", STATE.runtimeTs);
}

// Remainder of loadState() that does not depend on the runtime-restore path
// taken above (sync vs. async KVS fallback).
function finishLoadState(onDone) {
  // activeOutput: KVS fire-and-forget write; read is skipped here because
  // enforceOutputState() reads the actual hardware switch state right after
  // this call — hardware truth overrides any stale KVS value.

  // scheduleMode: use Script.storage (synchronous getItem/setItem) so that
  // the correct mode survives a reboot without needing an async callback chain.
  var savedMode = loadStorageString(STORAGE_KEYS.scheduleMode);
  if (savedMode === "summer" || savedMode === "winter") {
    STATE.scheduleMode = savedMode;
    log("Restored schedule mode:", STATE.scheduleMode);
  } else {
    STATE.scheduleMode = "winter";
    log("No saved schedule mode, defaulting to winter");
  }

  if (typeof onDone === "function") onDone();
}

function saveState() {
  // Skip writes during initialization to avoid callback depth issues
  if (STATE.initializing) {
    return;
  }

  // activeOutput → KVS (fire-and-forget, read by CLI status command)
  queueTask(function() {
    storeValue(STATE_KEYS.activeOutput, STATE.activeOutput);
  });

  // scheduleMode → Script.storage (synchronous read in loadState on next boot)
  //              → KVS as well (CLI status command reads it there)
  if (STATE.scheduleMode !== null) {
    storeStorageValue(STORAGE_KEYS.scheduleMode, STATE.scheduleMode);
    queueTask(function() {
      storeValue("schedule-mode", STATE.scheduleMode);
    });
  }
}

// === RUNTIME/TURNOVER TRACKING (on-device, for ctl pool status — #402) ===
// pool-pump.js is the only place that knows the string→RPM mapping for the
// configured preferred speed (via computeFlowRate() below), so it computes
// and persists today's cumulative runtime and achieved turnover itself;
// the daemon (ctl pool status / pool.getstatus RPC) just reads the results
// from KVS instead of re-deriving flow rate. Hooked into applyDone() — the
// single actuator's completion, i.e. the one place where the relay has
// actually moved — so every path that starts or stops the pump (schedule,
// solar, water supply, button, web UI, the anti-cycling fuse) is covered
// with zero duplication.

// Restores {sec, ts} from Script.storage: prefers the current single-object
// format, falling back once to the pre-#469 loose-scalar pair for a device
// upgrading in place. Returns null if neither is present/valid, so the
// caller (loadState) can fall back further to KVS.
function loadRuntimeStateFromStorage() {
  var obj = loadStorageObject(STORAGE_KEYS.runtime);
  if (obj !== null) {
    if (typeof obj.sec === "number" && typeof obj.ts === "number") {
      return {sec: obj.sec, ts: obj.ts};
    }
    // #533: deferred like reconcileRuntimeState()'s own log() calls above --
    // this still runs inline on the same synchronous loadState() chain, at
    // the same depth as the frame that crashed in #530, with an extra nested
    // JSON.stringify() call in its argument list.
    queueTask(function() {
      log("WARNING: Script.storage[" + STORAGE_KEYS.runtime + "] is malformed:", JSON.stringify(obj));
    });
  }
  // (migrateLegacyRuntimeState() inlined -- single call site, and this was
  // already the tail statement of loadRuntimeStateFromStorage, so its early
  // "return null" below is equivalent to returning it from here directly.)
  // One-time migration for devices that only have the pre-#469 loose-scalar
  // pair (runtime-sec + runtime-date). The date string is parsed here, and
  // only here — this is the one legacy compatibility path, not the ongoing
  // day-rollover logic, which never stores or parses a date string (#469).
  // applyRuntimeState() re-persists the result in the new object format
  // immediately, so this path is not taken again on the next restart.
  var legacySec = loadStorageNumber(STORAGE_KEYS.runtimeSecLegacy);
  if (legacySec === null) {
    return null; // nothing to migrate; a genuinely fresh device
  }
  var legacyDateStr = loadStorageString(STORAGE_KEYS.runtimeDateLegacy);
  var legacyTs = epochSecondsForDateString(legacyDateStr);
  if (legacyTs === null) {
    // #533: deferred -- this used to run one frame deeper than
    // reconcileRuntimeState() (loadState -> loadRuntimeStateFromStorage ->
    // migrateLegacyRuntimeState -> log). measure/inline-single-callsite
    // inlined migrateLegacyRuntimeState() into loadRuntimeStateFromStorage
    // (single call site, tail position -- see the note above), which removes
    // that frame: this queueTask() now runs at the same depth as the
    // malformed-obj warning above, not deeper. Left deferred anyway --
    // shallower is a strict improvement over #533's measurement, not a
    // reason to make it synchronous again.
    queueTask(function() {
      log("WARNING: legacy runtime-date missing/unparseable during migration ('" + legacyDateStr + "'), assuming today");
    });
    legacyTs = Math.floor(Date.now() / 1000);
  }
  queueTask(function() {
    log("Migrating legacy runtime state (#469): sec=" + legacySec + " date=" + legacyDateStr);
  });
  return {sec: legacySec, ts: legacyTs};
}

// Parses a legacy "YYYY-M-D" date string into the epoch second of local noon
// on that day (noon avoids DST-transition edge cases at midnight). Returns
// null if s is missing or not that shape. Migration-only — see above.
function epochSecondsForDateString(s) {
  if (typeof s !== "string") return null;
  var parts = s.split('-');
  if (parts.length !== 3) return null;
  var y = Number(parts[0]);
  var m = Number(parts[1]);
  var d = Number(parts[2]);
  if (isNaN(y) || isNaN(m) || isNaN(d)) return null;
  var dt = new Date(y, m - 1, d, 12, 0, 0);
  return Math.floor(dt.getTime() / 1000);
}

// Recovery path (#469): only reached when Script.storage yields nothing
// usable for either format — e.g. a fresh Script.storage after a script
// reinstall. KVS survives reinstalls. cb(sec, ts) receives numbers, or null
// for whichever half is missing/invalid. KVS.Get is async-only (Shelly.call
// without a callback returns null on real firmware — see the
// STORAGE_KEYS.scheduleMode comment above), so this chains two sequential
// calls rather than reading synchronously.
function loadRuntimeStateFromKVS(cb) {
  loadValueAsync(STATE_KEYS.runtimeSec, function (secVal) {
    var sec = (typeof secVal === "number") ? secVal : null;
    loadValueAsync(STATE_KEYS.runtimeTs, function (tsVal) {
      var ts = (typeof tsVal === "number") ? tsVal : null;
      cb(sec, ts);
    });
  });
}

// Decides the day's starting {sec, ts} from a restored (sec, ts) pair.
// Zeroes the count ONLY when ts is valid and demonstrably belongs to an
// earlier calendar day than today (a genuine rollover). Anything else —
// missing state, a sec with no valid ts, a ts that fails to parse — carries
// the count forward with a loud warning instead of resetting it: an
// over-counted day wastes energy, but a zeroed day causes real
// over-filtration, which is the harm #469 was filed over.
//
// #530: every log() call below is deferred via queueTask() instead of called
// inline. loadState() reaches this function synchronously from the tail of
// the LAST KVS.Get callback inside loadConfig() (loadNextKey() resolves and
// calls callback(true) directly, with no queueTask boundary in between) —
// init -> loadConfig's last KVS.Get callback -> loadNextKey -> callback(true)
// -> continueInit -> configureComponentNames -> loadState ->
// reconcileRuntimeState. #474 and #476/#480 already found this exact chain
// has no interpreter stack headroom left for an inline log() call by the
// time it reaches DOWNSTREAM of loadState() (setupMQTT,
// startRuntimeAccounting) and fixed both by deferring via queueTask(); this
// is the same mechanism, one step further UP the same chain, inside
// loadState() itself. It stayed invisible until now because every other
// branch below is silent on an ordinary same-day boot (restoredDay === today
// returns with no log() call at all) — a runtime record from yesterday is
// the everyday case after any midnight, not a corrupted-state edge case, so
// this is the normal daily path, not a rare one.
//
// Only the string formatting and print() inside log() move off this stack;
// the {sec, ts} decision itself — including zeroing the counter and
// re-anchoring ts on a genuine rollover — stays fully synchronous, so
// ensureRuntimeDay()'s mid-run callers (already running on a fresh stack)
// and #502 (ts is never re-stamped except on a real rollover) are unaffected.
function reconcileRuntimeState(sec, ts, sourceLabel) {
  var nowSec = Math.floor(Date.now() / 1000);
  if (sec === null) {
    queueTask(function() {
      log("WARNING: no valid runtime state from " + sourceLabel + " - starting today's count at 0s rather than assuming a reset (#469)");
    });
    return {sec: 0, ts: nowSec};
  }
  if (ts === null) {
    queueTask(function() {
      log("WARNING: runtime state from " + sourceLabel + " has a count but no valid timestamp, cannot verify which day it belongs to - carrying forward", sec, "s as today's total");
    });
    return {sec: sec, ts: nowSec};
  }
  var restoredDay = localDayNumber(ts);
  var today = localDayNumber(nowSec);
  if (restoredDay < today) {
    // #533: the discarded sec figure is announced only by this deferred,
    // best-effort log() call -- if init aborts for a different reason within
    // the ~200ms before the task queue tick runs, or the script is stopped,
    // the message (and the number) is lost with nothing else recording it.
    // Mirroring it to KVS survives that: the {sec:0, ts:today} zeroing below
    // is already recoverable from Script.storage, but the discarded amount
    // itself was not, until now.
    queueTask(function() {
      log("Runtime day rollover: discarding", sec, "s from", restoredDay);
      storeValue("rollover-discarded", sec);
    });
    return {sec: 0, ts: nowSec};
  }
  if (restoredDay > today) {
    queueTask(function() {
      log("WARNING: runtime state from " + sourceLabel + " has a future timestamp (day " + restoredDay + " > today " + today + ") - carrying forward", sec, "s rather than trusting or discarding it");
    });
  }
  return {sec: sec, ts: ts};
}

// Re-derives STATE.runtimeTs's day against "now" mid-run (called from
// startRuntimeAccounting/flushRuntimeCheckpoint) and resets the counter on a
// genuine rollover. If a run is currently open (runStartTs set) across the
// rollover, its start marker is pulled forward to "now" so only
// post-midnight time accrues to the new day — otherwise the next
// flush/stop would re-credit the whole pre-midnight run (since
// Date.now() - runStartTs would still span back into yesterday).
function ensureRuntimeDay() {
  var reconciled = reconcileRuntimeState(STATE.runtimeTodaySec, STATE.runtimeTs, "in-memory state");
  if (reconciled.sec !== STATE.runtimeTodaySec || reconciled.ts !== STATE.runtimeTs) {
    STATE.runtimeTodaySec = reconciled.sec;
    STATE.runtimeTs = reconciled.ts;
    if (STATE.runStartTs !== null) {
      STATE.runStartTs = Date.now();
      storeStorageValue(STORAGE_KEYS.runStart, STATE.runStartTs);
    }
    persistRuntimeState(STATE.runtimeTodaySec);
  }
}

// #547: checkpoint cadence, in ms. Read by runtimeFlushTick() below.
var RUNTIME_FLUSH_INTERVAL_MS = 60000;

// Called (via noteRelayTransition) when the pump transitions OFF -> ON.
function startRuntimeAccounting() {
  ensureRuntimeDay();
  STATE.runStartTs = Date.now();
  // #550: mirrored to Script.storage (cheap, synchronous, same pattern as
  // scheduleMode) so a script outage that swallows the matching stop can be
  // reconciled against real hardware at the NEXT boot -- see
  // enforceOutputState()/reconcileMissedStop(). STATE.runStartTs itself is
  // never restored as belief (a restart always re-derives from hardware
  // truth); this mirror exists ONLY to recover the seconds an outage would
  // otherwise silently drop.
  storeStorageValue(STORAGE_KEYS.runStart, STATE.runStartTs);
  if (!STATE.runtimeFlushActive) {
    // #547: starts the checkpoint chain — see runtimeFlushTick() below. Only
    // exists while the pump is actually on (stopped in
    // stopRuntimeAccounting), so it never competes with the task-queue timer
    // during steady-state idle operation, and it adds no new Timer.set() —
    // it piggybacks entirely on the queue's existing 200ms drain.
    STATE.runtimeFlushActive = true;
    STATE.runtimeFlushDueTs = Date.now() + RUNTIME_FLUSH_INTERVAL_MS;
    queueTask(runtimeFlushTick);
  }
  log("Runtime accounting started, today so far:", STATE.runtimeTodaySec, "s");
}

// Called (via noteRelayTransition) when the pump transitions ON -> OFF.
function stopRuntimeAccounting() {
  // #502: without this, a stop landing exactly at a day boundary -- before
  // the next 60s flushRuntimeCheckpoint() tick has a chance to split it --
  // would blindly add the WHOLE pre+post-midnight elapsed span below to
  // "today's" total instead of only the post-midnight portion. This mirrors
  // the same call already present in startRuntimeAccounting() and
  // flushRuntimeCheckpoint(); stopRuntimeAccounting() was the one caller
  // missing it.
  ensureRuntimeDay();
  if (STATE.runStartTs !== null) {
    STATE.runtimeTodaySec += (Date.now() - STATE.runStartTs) / 1000;
    STATE.runStartTs = null;
    // #550: this stop was OBSERVED (this function only runs while the script
    // is up), so the outage-recovery marker is cleared cleanly.
    storeStorageValue(STORAGE_KEYS.runStart, null);
  }
  // #547: the chain in runtimeFlushTick() below checks this flag (and
  // runStartTs, now already null above) on its very next 200ms tick and lets
  // itself lapse — no Timer.clear()/handle bookkeeping needed.
  STATE.runtimeFlushActive = false;
  persistRuntimeState(STATE.runtimeTodaySec);
  log("Runtime accounting stopped, today total:", STATE.runtimeTodaySec, "s");
}

// Periodic checkpoint while the pump is running (driven by runtimeFlushTick()
// below): persists runtimeTodaySec plus the still-open interval's
// elapsed-so-far, without clearing runStartTs — the run is still in
// progress. This bounds crash/reboot data loss to at most one flush
// interval instead of the whole run.
//
// #524: also the periodic boundary check for a #524-extended window (or the
// hard ceiling, or the original stop) once solar is disabled and nothing
// else ticks reconcile() while running -- reconcile() is always safe to call
// (see "THE RECONCILER" above) and this chain already exists and already
// runs only while the pump is on, so no new timer is added.
function flushRuntimeCheckpoint() {
  ensureRuntimeDay();
  if (STATE.runStartTs === null) return;
  var elapsedSec = (Date.now() - STATE.runStartTs) / 1000;
  persistRuntimeState(STATE.runtimeTodaySec + elapsedSec);
  reconcile(null);
}

// #547: drives flushRuntimeCheckpoint() off the task queue's 200ms drain
// instead of a dedicated Timer.set(60000, true, ...). On a 7h32m production
// run (2026-08-24, filtration-hiver) that dedicated timer returned a live
// handle -- Timer.set did not fail to allocate -- yet neither
// STATE.runStartTs nor the persisted runtime total ever moved across two
// probes 95s apart mid-run; only stopRuntimeAccounting()'s single end-of-run
// credit was ever correct. The task queue's own 200ms timer is the one
// recurring construct proven reliable all day, every day -- every schedule
// dispatch, saveState()'s KVS mirrors and every applyDone() already ride it
// -- so the checkpoint now rides the same rail instead of a second,
// independently-scheduled long-period timer whose real-firmware "repeat"
// behaviour turned out not to be trustworthy. Self-requeues via queueTask()
// approximately every 200ms for as long as STATE.runtimeFlushActive and
// STATE.runStartTs both say the pump is running; either turning false ends
// the chain within one tick, with nothing left to clear.
function runtimeFlushTick() {
  if (!STATE.runtimeFlushActive || STATE.runStartTs === null) return;
  if (Date.now() >= STATE.runtimeFlushDueTs) {
    flushRuntimeCheckpoint();
    STATE.runtimeFlushDueTs = Date.now() + RUNTIME_FLUSH_INTERVAL_MS;
  }
  queueTask(runtimeFlushTick);
}

// Persists sec (defaults to STATE.runtimeTodaySec) plus the current day
// anchor (STATE.runtimeTs) to Script.storage as one object (sync, boot-safe
// — #469: a serialised {sec, ts} object cannot be silently misparsed as a
// number the way the old loose scalars could) and mirrors sec, ts, and
// today's computed turnover to KVS for Go-side visibility
// (myhome/daemon/pool_notices.go ComputeTurnover) and as a recovery path if
// Script.storage is ever lost (e.g. a script reinstall — see
// loadRuntimeStateFromKVS).
//
// #502: ts is STATE.runtimeTs, NEVER Date.now(). ts means "which calendar
// day does sec belong to", not "when was this record last written" — every
// earlier version of this function recomputed ts = Date.now() on every call
// (flushRuntimeCheckpoint every 60s while running, every stop, every load),
// so a total accumulated over several idle days always looked freshly
// written and reconcileRuntimeState() never saw a stale day to reset
// against — confirmed live on `mezzanine`: Script.storage["runtime"] read
// {"sec":42002.2,"ts":<the exact second of the last restart>}, days after
// that sec total was last genuinely accrued. The anchor is only ever
// allowed to move where reconcileRuntimeState() decides a genuine rollover
// (or "no valid data") occurred; every caller that wants to move it sets
// STATE.runtimeTs itself BEFORE calling this function — see
// applyRuntimeState() and ensureRuntimeDay().
//
// The Script.storage write is synchronous and always happens (cheap, no RPC).
// The KVS mirrors go through Shelly.call and are skipped during
// STATE.initializing — same guard as saveState() — and queued via
// queueTask() rather than fired back-to-back: un-queued calls here, repeated
// every 60s by flushRuntimeCheckpoint() while the pump runs and possibly
// overlapping saveState()'s own KVS writes, is exactly the "Too many calls
// in progress" crash PR #394 fixed (5-concurrent-RPC limit). A skipped
// mirror during init is harmless — the next checkpoint or stop re-persists it.
function persistRuntimeState(overrideSec) {
  var sec = (typeof overrideSec === "number") ? overrideSec : STATE.runtimeTodaySec;
  // Defensive fallback only: every real call site sets STATE.runtimeTs to a
  // number before reaching here (see #502 note above), so this should never
  // actually be exercised on a healthy device.
  var ts = (typeof STATE.runtimeTs === "number") ? STATE.runtimeTs : Math.floor(Date.now() / 1000);
  storeStorageValue(STORAGE_KEYS.runtime, {sec: sec, ts: ts});
  if (STATE.initializing) {
    return;
  }
  queueTask(function() {
    storeValue(STATE_KEYS.runtimeSec, Math.round(sec));
  });
  queueTask(function() {
    storeValue(STATE_KEYS.runtimeTs, ts);
  });
  queueTask(function() {
    // (computeTurnoverToday() inlined -- single call site. Hoisted to a
    // temporary before the storeValue() call rather than folded into its
    // argument list -- see #530/#537 on argument-position call nesting.)
    // Turnovers (pool volumes filtered) achieved today given sec seconds of
    // pump-on time at the currently configured preferred speed. Reuses
    // computeFlowRate() so the string->RPM mapping is only ever implemented
    // once.
    var turnoverToday = 0;
    var flowRate = computeFlowRate();
    if (flowRate && flowRate > 0 && CONFIG.poolVolume) {
      turnoverToday = Math.round((sec / 3600) * flowRate / CONFIG.poolVolume * 100) / 100;
    }
    storeValue("turnover-today", turnoverToday);
  });
}

// The speed this device will actually run at.
//
// A Pro1 is on/off: its single stage is 'max'. Whatever speed the KVS carries —
// a Pro3-era 'eco', say — the pump physically runs at max RPM, so the whole
// script must agree on that. Resolving it only in mapSpeedToSwitch() would fix
// the switching and leave the arithmetic lying: computeFlowRate() scales
// maxFlowRate by rpm/maxRpm, so 'eco' on a Pro1 would model 2000/2900 = 69% of
// the real flow, underestimate turnover per day by a third, and make
// computeRunHours() demand a longer window than the pool needs.
//
// Normalising rather than refusing also means a device still configured from
// the Pro3 era keeps filtering instead of stranding.
function effectiveSpeed(speed) {
  if (!speed || speed === 'off') return 'off';
  if (STATE.deviceType === 'pro1') return 'max';
  return speed;
}

function mapSpeedToSwitch(speed) {
  // Map a semantic speed to a physical switch ID.
  // speed: 'eco', 'day', 'max' (or 'off')
  // Returns switch ID, or -1 for off.

  var eff = effectiveSpeed(speed);
  if (eff === 'off') {
    return -1;
  }

  if (STATE.deviceType === 'pro3') {
    // Pro3 drives three speed stages.
    if (eff === 'eco') return CONFIG.ecoSpeed;
    if (eff === 'day') return CONFIG.daySpeed;
    if (eff === 'max') return CONFIG.maxSpeed;
  } else if (STATE.deviceType === 'pro1') {
    return 0;
  }

  log("WARNING: Unknown speed or device type, defaulting to off");
  return -1;
}

// === OUTPUT CONTROL ===
// Turns off outputs one at a time via the task queue. Dispatching Shelly.call from
// inside a for loop fires all iterations before any response arrives, exhausting the
// 5-concurrent-RPC budget (see AGENTS.md "Callback Depth Limits" — Cause B).
function turnOffAllSwitchesNext(ids, index, callback) {
  if (index >= ids.length) {
    if (callback) callback();
    return;
  }
  var id = ids[index];
  index++;
  Shelly.call("Switch.Set", {id: id, on: false}, function(res, err) {
    if (err && false) {}
    queueTask(function() { turnOffAllSwitchesNext(ids, index, callback); });
  });
}

function turnOffAllSwitches(callback) {
  turnOffAllSwitchesNext(STATE.outputs, 0, callback);
}

function setOutput(outputId, on, callback) {
  if (STATE.outputs.indexOf(outputId) === -1) {
    log("ERROR: Invalid output ID:", outputId);
    // Distinguish this from a real Switch.Set success: callers (applyDone)
    // key off a truthy error_code to tell a failure from an actual completion.
    // Calling back with no arguments at all used to make the two
    // indistinguishable.
    if (callback) callback(null, -1, "invalid output id");
    return;
  }

  log("Setting switch", outputId, "to", on);
  Shelly.call("Switch.Set", {id: outputId, on: on}, callback);
}

// Called when one of the break-before-make off-steps below fails. Belief
// must come from observation, never from intent (same rule applyDone()
// follows for the final on-step): the target output must NOT be turned on
// while another stage may still be energized, so this aborts the whole
// transition rather than pressing on to applyOutputOn(). STATE.activeOutput
// is left untouched and RC_BUSY is cleared so reconcile()'s "want ===
// STATE.activeOutput" check still disagrees with reality and retries on the
// next tick (#479 review finding 4).
// Turns off every output except exceptId, one at a time via the task queue
// (see turnOffAllSwitches above for why a for-loop of Shelly.call is unsafe here).
function turnOffOtherOutputsNext(ids, index, exceptId, callback) {
  if (index >= ids.length) {
    if (callback) callback();
    return;
  }
  var id = ids[index];
  index++;
  if (id === exceptId) {
    turnOffOtherOutputsNext(ids, index, exceptId, callback);
    return;
  }
  setOutput(id, false, function(result, error_code, error_message) {
    if (error_code) {
      // (turnOffOtherOutputsFailed() inlined -- single call site; tail
      // position of this branch.)
      log("break-before-make: output", id, "off FAILED, error", error_code, error_message);
      RC_BUSY = false;
      reconcile(null);
      return;
    }
    queueTask(function() { turnOffOtherOutputsNext(ids, index, exceptId, callback); });
  });
}

// (turnOffOtherOutputs() inlined at its one call site in applyOutput() --
// single call site, trivial one-statement wrapper.)

// === SOFTWARE FUSE (ANTI-CYCLING PROTECTION) ===
// Prevents rapid relay cycling that generates repeated motor inrush currents
// and trips circuit breakers. Tracks output state changes in a sliding window;
// if too many transitions occur, the fuse "trips": all switches are turned off
// and ON activations are refused for a cooldown period.
// OFF activations (-1) always pass — safety trumps the fuse.
var FUSE_WINDOW_MS = 120000;      // 2-minute sliding window
var FUSE_MAX_CHANGES = 4;         // max state changes per window
var FUSE_COOLDOWN_MS = 300000;    // 5-minute cooldown after trip
var FUSE_CHANGES = [];            // timestamps of recent state changes
var FUSE_TRIPPED = false;
var FUSE_TRIP_TIME = 0;

// (fuseRecord() inlined at its one call site in applyOutput() -- single
// call site, trivial one-statement wrapper.)

function fuseAllowOn() {
  var now = Date.now();

  // If tripped, check cooldown
  if (FUSE_TRIPPED) {
    if (now - FUSE_TRIP_TIME >= FUSE_COOLDOWN_MS) {
      log("FUSE: cooldown expired, resetting");
      FUSE_TRIPPED = false;
      FUSE_CHANGES = [];
      return true;
    }
    log("FUSE: tripped, refusing activation (cooldown remaining:",
        Math.round((FUSE_COOLDOWN_MS - (now - FUSE_TRIP_TIME)) / 1000), "s)");
    return false;
  }

  // Prune entries outside the window (no shift — manual loop per Shelly constraint)
  var recent = [];
  for (var i = 0; i < FUSE_CHANGES.length; i++) {
    if (now - FUSE_CHANGES[i] < FUSE_WINDOW_MS) {
      recent.push(FUSE_CHANGES[i]);
    }
  }
  FUSE_CHANGES = recent;

  // Check threshold
  if (FUSE_CHANGES.length >= FUSE_MAX_CHANGES) {
    log("FUSE: TRIPPED - " + FUSE_CHANGES.length + " state changes in " +
        (FUSE_WINDOW_MS / 1000) + "s window. Blocking ON activations for " +
        (FUSE_COOLDOWN_MS / 1000) + "s");
    FUSE_TRIPPED = true;
    FUSE_TRIP_TIME = now;
    // A trip is a FACT, not an action. This used to call
    // turnOffAllSwitches() here — from inside an in-flight actuation, which
    // then returned false and went on to issue its OWN off-chain, so two
    // overlapping Switch.Set sequences ran on the same outputs. Returning
    // false is enough: the caller forces the target to -1 and the single
    // actuator drives that one chain.
    //
    // #549: this event records that the fuse TRIPPED, not that the caller's
    // activation was REFUSED — the two used to be the same thing, but are
    // not anymore. A button-driven want can be the very call that flips
    // FUSE_TRIPPED to true (fuseAllowOn() still runs unconditionally for
    // every ON attempt, see reconcileNow()) and still proceed anyway,
    // because reconcileNow() ignores a false return for a button-driven
    // want. Do not read a pool.fuse_tripped event as "the pump stayed off" —
    // check the actual relay state/active-output for that.
    Shelly.emitEvent("pool.fuse_tripped", {
      changes: FUSE_CHANGES.length,
      window_s: FUSE_WINDOW_MS / 1000,
      cooldown_s: FUSE_COOLDOWN_MS / 1000
    });
    return false;
  }

  return true;
}

// === LAYER 1: FACTS (#476) ===
//
// One preallocated scalar per observed fact, exactly ONE writer each. No
// event objects, no event queues, no per-event strings: a fact is written in
// place and the reconciler is asked to converge. That is what makes an input
// flapping faster than a Switch.Set round trip free — there is no work to
// accumulate, only a value to overwrite.
var F_WATER      = false;  // input:0 state           — written only by setWater()
var F_SOLAR_WANT = false;  // solar hysteresis opinion — written only by checkSolarHysteresis()
var F_WIN_START  = -1;     // run-window start, minutes since midnight — written only by setWindow()
var F_WIN_STOP   = -1;     // run-window stop,  minutes since midnight — written only by setWindow()
var F_OVR_WANT   = -2;     // manual override: -2 none, -1 off, >=0 switch id — cycleOutputs()/handleSwitchEvent()/clearOverride()
var F_OVR_UNTIL  = 0;      // override expiry, ms epoch                      — same three writers
// #549: which of F_OVR_WANT's two setters produced the CURRENTLY ACTIVE
// override. true only while the override is a physical button press
// (cycleOutputs()); false for an out-of-band adoption (handleSwitchEvent(),
// e.g. web UI/cloud/another controller) and reset by clearOverride(). This is
// what lets reconcileNow() and applyOutput() tell "a human pressed the
// button" apart from "something else moved the relay" — the distinction the
// maintainer decided the fuse must respect (#549): a button press is never
// refused by the fuse, and never counted toward its window either; an
// out-of-band change keeps the fuse exactly as before.
var F_OVR_IS_BUTTON = false;

function setWater(active) {
  if (active === F_WATER) return;
  F_WATER = active;
  Shelly.emitEvent(active ? "pool.water_supply_protected" : "pool.water_supply_restored",
                   {output: STATE.activeOutput});
  // #524: on release, recover whatever this interruption cost before
  // reconciling -- extendWindowForShortfall() is a no-op if nothing is owed.
  if (!active) extendWindowForShortfall();
  reconcile(active ? "water supply on" : "water supply off");
}

function setWindow(startMin, stopMin) {
  if (startMin === F_WIN_START && stopMin === F_WIN_STOP) return;
  log("window:", F_WIN_START, "-", F_WIN_STOP, "->", startMin, "-", stopMin);
  F_WIN_START = startMin;
  F_WIN_STOP = stopMin;
  reconcile("window moved");
}

// #524: recovers time the pump lost to a non-filtering interval (typically
// the water-supply interlock) by pushing the window's stop bound later,
// through setWindow() -- the fact's one writer (#476) -- so achieved runtime
// converges on computeRunHours()'s intent for the day instead of losing
// whatever the interruption cost. Pure arithmetic over facts that already
// exist: STATE.runtimeTodaySec (only CLOSED intervals -- see its own
// declaration above, "completed intervals only" -- so a still-open run must
// be added in below) plus any still-running interval, and STATE.maxForecastTemp
// (this morning's forecast, unchanged since decideModeFromForecast() set it).
// No new tracking structure. Any solar-triggered running outside the window
// already folds into STATE.runtimeTodaySec once its interval closes --
// desiredOutput() checks F_SOLAR_WANT before the window -- so a solar-heavy
// morning already shrinks or erases the shortfall computed here.
//
// #524 review (silent-failure pass): the first version of this function used
// STATE.runtimeTodaySec alone. handleEveningStop() -- one of this function's
// two call sites -- fires with the pump still RUNNING (that open interval is
// exactly what handleEveningStop()'s own reconcile() is about to stop), so at
// that instant runtimeTodaySec excluded the entire in-progress run. On the
// issue's own measured day that turned a real 1099s shortfall into a computed
// 19052s one and extended to stopCeil regardless of the true gap -- and on a
// day with NO interruption at all it was worse, firing unconditionally with
// the whole day's intent as "owed". Adding the open interval here is what
// makes the two call sites agree: setWindow(false)'s call site already saw a
// closed interval (stopRuntimeAccounting() ran first), so it was never wrong;
// this makes handleEveningStop()'s call site correct the same way.
//
// Bounded like decideModeFromForecast()'s own window: stopCeil is the same
// sunset-minus-half-hour ceiling, so recovery can push the stop later but
// never past it -- the pump does not run into the night.
function extendWindowForShortfall() {
  if (STATE.scheduleMode !== 'summer') return;
  if (STATE.maxForecastTemp === null) return;
  if (F_WIN_START < 0 || F_WIN_STOP < 0) return;
  var achievedSec = STATE.runtimeTodaySec;
  if (STATE.runStartTs !== null) achievedSec += (Date.now() - STATE.runStartTs) / 1000;
  var shortfallSec = computeRunHours(STATE.maxForecastTemp) * 3600 - achievedSec;
  if (shortfallSec <= 0) return;
  var d = new Date();
  var nowMin = d.getHours() * 60 + d.getMinutes();
  var stopCeilMin = Math.floor(((STATE.sunsetHour !== null ? STATE.sunsetHour : 21) - 0.5) * 60);
  var targetStopMin = nowMin + Math.ceil(shortfallSec / 60);
  if (targetStopMin > stopCeilMin) {
    log("recovery clamped to stopCeil:", targetStopMin, "->", stopCeilMin);
    targetStopMin = stopCeilMin;
  }
  if (targetStopMin > F_WIN_STOP) setWindow(F_WIN_START, targetStopMin);
}

// A reconciler reverts an out-of-band relay change that contradicts the
// policy — a web-UI toggle or a button press would be undone within one
// 200ms task-queue tick. An override is therefore REQUIRED, not optional:
// it makes "a human moved this" a fact the policy can see, bounded by
// CONFIG.overrideMs and cleared at the next schedule edge, whichever is
// sooner.
function clearOverride() {
  if (F_OVR_WANT === -2) return;
  log("override cleared");
  F_OVR_WANT = -2;
  F_OVR_UNTIL = 0;
  F_OVR_IS_BUTTON = false;
}

// #549: true only while the CURRENTLY ACTIVE override is a physical button
// press, mirroring the exact two conditions desiredOutput() uses to decide
// whether F_OVR_WANT is in force right now -- so this can never disagree with
// what desiredOutput() is about to return. Read from reconcileNow(), once per
// pass, rather than re-derived inside applyOutput(): by the time applyOutput()
// runs, F_OVR_UNTIL may already have ticked past "now" (button/schedule
// events and applyOutput()'s own dispatch are not atomic), which would flip
// this function's answer between the two call sites for the very same
// transition.
function overrideIsButton() {
  return F_OVR_WANT !== -2 && Date.now() < F_OVR_UNTIL && F_OVR_IS_BUTTON;
}

// === LAYER 2: THE POLICY (#476) ===
//
// Pure function of the facts above. Returns an output id, -1 for off, or
// **-2 for "no opinion — leave the relay alone"**.
//
// The -2 is not decoration. A level-triggered controller with a two-valued
// desired state turns "I cannot tell" into "off", which would stop a running
// pump because a Schedule.List call failed. That is the lesson of #441/#436
// encoded in the return type.
//
// THE ORDER OF THESE LINES IS THE CONTROL POLICY and should be reviewed as
// such. Before this change the equivalent ordering was an accident of call
// order across seven functions.
//
// FUSE_TRIPPED is deliberately NOT consulted here. The fuse is applied by
// reconcileNow() instead, because fuseAllowOn() is also what clears the trip
// when the cooldown expires: short-circuiting to -1 here would mean
// fuseAllowOn() never runs again and the fuse would latch forever.
function desiredOutput() {
  if (F_WATER) return -1;                                        // safety first, always
  if (F_OVR_WANT !== -2 && Date.now() < F_OVR_UNTIL) return F_OVR_WANT;
  if (CONFIG.solarEnabled && solarHardCeilingReached()) return -1;
  if (F_SOLAR_WANT) return mapSpeedToSwitch(CONFIG.preferredSpeed);
  if (F_WIN_START < 0 || F_WIN_STOP < 0) return -2;              // window unknown: do not act
  var d = new Date();
  var n = d.getHours() * 60 + d.getMinutes();
  // A start > stop pair wraps past midnight (the fixed winter 23:15 -> 00:15 run).
  var inside = F_WIN_START <= F_WIN_STOP
    ? (n >= F_WIN_START && n < F_WIN_STOP)
    : (n >= F_WIN_START || n < F_WIN_STOP);
  if (inside) return mapSpeedToSwitch(CONFIG.preferredSpeed);
  return -1;
}

// === THE RUN WINDOW AS A FACT ===
//
// The window used to be re-read from Schedule.List on every decision
// (isWithinRunWindow), which is an RPC per decision and a race per rewrite
// (#441). It is now read once at init and thereafter owned by setWindow():
// updateScheduleMode() is the only thing that moves it, and (#509) derives
// the new window straight from the Schedule.List response it already has in
// hand, rather than issuing a second one after writing.
//
// Named callback, so no closure is allocated per call.
//
// Matches job code by CONTAINMENT, not exact equality (#480/#479-followup):
// createSchedules() wraps every job's code in
// (function(){try{...}catch(e){...}})() so a throw inside the handler can't
// kill the script, so job.calls[0].params.code is never exactly
// "handleMorningStart()" on a device that has been through that wrapping —
// only the unwrapped bare call would ever match "===". Without this, a and b
// never leave -1, this function returns on the "still symbolic" guard below,
// setWindow() never runs, and desiredOutput() returns -2 ("no opinion")
// forever: the pump silently never starts or stops on schedule again. This
// is the same fix that used to live in isWithinRunWindow() (removed as dead
// code by #524 — it lost its only caller, this reconciler never called it,
// and the source said so) — ported to the function this branch actually uses.
function onWindowJobs(result, err) {
  if (err && false) {}
  if (err || !result || !result.jobs) return;   // unresolvable: leave the facts alone (-2 path)
  var summer = STATE.scheduleMode === 'summer';
  var sc = summer ? 'handleMorningStart()' : 'handleNightStart()';
  var ec = summer ? 'handleEveningStop()' : 'handleNightStop()';
  var a = -1;
  var b = -1;
  for (var i = 0; i < result.jobs.length; i++) {
    var job = result.jobs[i];
    if (!job.enable || !job.calls || job.calls.length === 0) continue;
    var code = job.calls[0].params && job.calls[0].params.code;
    if (code && code.indexOf(sc) !== -1) {
      var ma = parseHM(job.timespec);
      if (ma !== null) a = ma;
    } else if (code && code.indexOf(ec) !== -1) {
      var mb = parseHM(job.timespec);
      if (mb !== null) b = mb;
    }
  }
  if (a < 0 || b < 0) return;                   // still symbolic (@sunrise): unknown, not "off"
  setWindow(a, b);
}

// === LAYER 3: THE RECONCILER (#476) ===
//
// Every entry point in this script is "write the fact, call reconcile(),
// return". reconcile() never acts inline: it sets a dirty bit and defers to
// the task queue, passing a NAMED function so no closure is allocated per
// event (#421 measured one per-call closure at ~1050 bytes of mem_peak).
//
// Coalescing is by construction. The queue is one boolean, not a list, so
// any number of events arriving before the next 200 ms tick collapse into a
// single pass over the current facts. An input flapping faster than a
// Switch.Set round trip therefore costs nothing and cannot accumulate work —
// which is the #450 crash removed at the root rather than guarded.
var RC_DIRTY = false;

function reconcile(reason) {
  if (reason) log("reconcile:", reason);
  if (RC_DIRTY) return;
  RC_DIRTY = true;
  queueTask(reconcileNow);
}

function reconcileNow() {
  RC_DIRTY = false;
  if (RC_BUSY) { reconcile(null); return; }

  var want = desiredOutput();
  if (want === -2) return;                       // unknown: never guess

  // #549: captured HERE, once per pass, and consumed by applyOutput() below
  // via the module-level flag -- see its declaration for why applyOutput()
  // must not re-derive this itself. Water supply is unaffected: F_WATER is
  // checked first, unconditionally, inside desiredOutput() above, so a
  // button override never even reaches this point while water protection is
  // active.
  RC_BUTTON_DRIVEN = overrideIsButton();

  // The fuse is applied here, not in desiredOutput(), because fuseAllowOn()
  // is also what clears the trip once the cooldown expires — see the note on
  // desiredOutput(). A refused ON degrades to "off" and is driven by the same
  // single actuation, so a trip can never produce two overlapping chains.
  //
  // #549: fuseAllowOn() still runs unconditionally for every ON attempt --
  // its cooldown-clearing side effect must keep firing on schedule regardless
  // of who wants the pump on, or the fuse would latch forever the next time a
  // non-button caller needs it. Only the REFUSAL is skipped for a
  // button-driven want: the maintainer's decision that a physical press must
  // never be refused by the fuse.
  if (want !== -1 && !fuseAllowOn() && !RC_BUTTON_DRIVEN) {
    log("FUSE: activation refused, forcing off");
    want = -1;
  }

  if (want === STATE.activeOutput) return;
  applyOutput(want);
}

// === THE SINGLE SERIALISED ACTUATOR (#476) ===
//
// applyOutput()/applyDone() are the ONLY code in this script permitted to
// drive the pump relay. Everything that wants the pump in a different state
// goes through applyOutput(); nothing else calls setOutput()/
// turnOffAllSwitches()/turnOffOtherOutputs() on the pump outputs.
//
// Why: before this, TWELVE paths reached the hardware (see #475), four of
// them assigning STATE.activeOutput and calling setOutput() directly. Two
// consequences, both observed in production:
//
//   - a second event arriving inside an in-flight Switch.Set started a
//     second chain nested in the first, and the nesting grew until the
//     interpreter stack overflowed (#450);
//   - button- and web-UI-driven runs accrued NO runtime and recorded NO
//     fuse change, because startRuntimeAccounting()/fuseRecord() were only
//     reachable from the old activateOutput() (#475 defect 1).
//
// RC_BUSY is true from the moment an actuation starts until its final
// Switch.Set callback has run. Nothing may start a second actuation while it
// is set — that is exactly the #450 nesting, where a second event's
// Shelly.call landed inside the native completion handler of the first and
// the nesting grew until the interpreter stack overflowed. applyDone() goes
// back through reconcile(), i.e. through queueTask, so any follow-up runs on
// a fresh stack.
var RC_BUSY = false;      // an actuation is in flight
var RC_TARGET = -1;       // the output the in-flight actuation drives towards
// #549: whether the in-flight (or about-to-be-dispatched) actuation is
// button-driven, as decided by reconcileNow() -- the one place that knows
// WHY applyOutput() is about to be called. applyOutput() reads this instead
// of calling overrideIsButton() itself so both call sites agree on a single
// decision made at a single instant, rather than risking F_OVR_UNTIL ticking
// past "now" between reconcileNow()'s decision and applyOutput()'s dispatch.
// (#550 removed the old RC_WAS capture that used to sit here -- accounting
// now rides on noteRelayTransition() comparing against the live
// STATE.activeOutput instead of a value captured at dispatch time; see its
// comment below.)
var RC_BUTTON_DRIVEN = false;

function applyOutputOn() {
  setOutput(RC_TARGET, true, applyDone);
}

// === RELAY-TRANSITION-DRIVEN ACCOUNTING (#550) ===
//
// #550: runtime accounting used to live entirely in applyDone(), keyed off
// this script's OWN belief (was it the one that just actuated?) rather than
// what the relay actually did. That made accounting a function of WHO
// actuated, not of WHAT happened -- a physical button press went through
// applyOutput()/applyDone() and counted; a web-UI change wrote
// STATE.activeOutput directly in handleSwitchEvent() and never reached
// applyOutput() at all (reconcileNow()'s "want === STATE.activeOutput" guard
// short-circuits once the fact already agrees), so it never counted. Confirmed
// on hardware 2026-08-25: a 75s web-driven run left runStartTs null and
// runtimeTodaySec at 0.
//
// noteRelayTransition() is now the ONLY place that calls
// startRuntimeAccounting()/stopRuntimeAccounting() or emits
// pool.pump_start/pool.pump_stop. Both applyDone() (this script's own
// actuation completing) and handleSwitchEvent() (the switch:N status-change
// notification firmware sends for EVERY relay transition, including this
// script's own -- the previous RC_WAS comment already relied on this fact to
// fix #479 finding 2) feed it the newly-observed output. It compares against
// STATE.activeOutput as it stands *right now* and only acts on a genuine
// -1<->non-1 edge:
//
//   - if the caller is the FIRST channel to report a given transition, the
//     edge is real (STATE.activeOutput still holds the pre-transition value)
//     and the hooks fire;
//   - if the caller is the SECOND channel to report the SAME transition
//     (e.g. the switch:N event that a self-driven Switch.Set also produces,
//     arriving after applyDone() already processed the RPC completion, or
//     vice versa if it arrives nested before), STATE.activeOutput already
//     equals what it is reporting and the call is a deliberate, silent no-op.
//
// This makes a transition counted EXACTLY ONCE regardless of delivery order,
// with no dependency on which of the two channels happens to observe it
// first -- and it is what closes #550: applyOutput()/applyDone() are never
// invoked at all for a web-UI (or any other out-of-band) change, so
// handleSwitchEvent() is the ONLY channel that ever reports one, and it now
// drives accounting exactly like it already drove STATE.activeOutput.
//
// Risk (see #550's PR description for the full writeup): applyDone() is
// deterministic for every path this script itself drives (schedule, solar,
// button, water-supply) -- it is a direct callback of the Shelly.call this
// script issued, not a notification that could be dropped in transit. So
// those paths keep exactly today's reliability even if the corresponding
// switch:N notification is ever lost on real firmware. The one channel that
// COULD go missing with no backstop is a genuinely out-of-band transition
// (web UI, cloud, another script) whose notification never arrives at all --
// there is no reconciliation elsewhere in this file that would notice
// STATE.runStartTs disagreeing with STATE.activeOutput and self-heal it. A
// lost START leaves that interval simply uncredited (matches today's
// behaviour for the untracked case, #550 was filed over); a lost STOP leaves
// runStartTs open, which is under-credited today (#547: the 60s checkpoint
// never fires, so nothing is credited until the next stop overwrites it) and
// would become over-credited once #547 is fixed (a live checkpoint crediting
// elapsed time while the pump is actually off) -- exactly the interaction
// #550 was filed to flag, not to resolve.
function noteRelayTransition(newOutput) {
  var was = STATE.activeOutput;
  STATE.activeOutput = newOutput;
  if (was === newOutput) return;   // already accounted for by the other channel
  if (was === -1 && newOutput !== -1) {
    startRuntimeAccounting();
    Shelly.emitEvent("pool.pump_start",
                     {speed: effectiveSpeed(CONFIG.preferredSpeed), switch_id: newOutput});
  } else if (was !== -1 && newOutput === -1) {
    stopRuntimeAccounting();
    Shelly.emitEvent("pool.pump_stop", {reason: F_WATER ? "water supply" : "policy"});
  }
}

// Invoked as a Shelly.call/setOutput completion: (result, error_code,
// error_message). A failed actuation must NOT be believed as if it
// succeeded -- STATE.activeOutput is left untouched (noteRelayTransition() is
// not called at all), so reconcileNow()'s "want === STATE.activeOutput" check
// still disagrees with reality and queues a retry on the next tick. Without
// this check, a failed ON left the reconciler believing the pump had started
// (no retry, pool silently stops filtering) and a failed OFF left it
// believing the pump had stopped while it kept running.
function applyDone(result, error_code, error_message) {
  if (error_code) {
    log("apply: FAILED, output", RC_TARGET, "error", error_code, error_message);
    RC_BUSY = false;
    // Converge: retry from the facts as they stand now. Belief was never
    // updated, so this is a genuine retry, not a no-op.
    reconcile(null);
    return;
  }

  // #550: STATE.activeOutput write and runtime/turnover accounting both now
  // happen inside noteRelayTransition() -- see its comment above -- so this
  // actuation's completion and an out-of-band observation of the very same
  // transition can never double-count. BEFORE saveState(): persistRuntimeState()
  // (inside start/stopRuntimeAccounting) enqueues its KVS mirror on the same
  // 200ms task queue, and callers (and tests) treat the active-output write as
  // the "transition finished" marker. Queueing the runtime mirrors first keeps
  // active-output last, so observing it means the runtime figures for that
  // interval are already persisted.
  noteRelayTransition(RC_TARGET);

  saveState();

  RC_BUSY = false;
  // Converge: the facts may have moved while we were busy.
  reconcile(null);
}

function applyOutput(target) {
  if (RC_BUSY) {
    // Never nest a second Switch.Set chain inside the first (#450). The
    // reconciler will re-evaluate from applyDone().
    log("apply: deferring", target, "- actuation in flight");
    reconcile(null);
    return;
  }

  RC_BUSY = true;
  RC_TARGET = target;
  // #549: a button-driven transition must not count toward the fuse window
  // either -- only refusing the ON and still recording it would let a
  // backwash's OWN presses eventually trip the fuse anyway, defeating the
  // "never refused" guarantee in reconcileNow() above. RC_BUTTON_DRIVEN was
  // decided there, once, for exactly this dispatch. Everything else
  // (schedule, solar, water-supply, out-of-band) keeps recording exactly as
  // before.
  if (target !== STATE.activeOutput && !RC_BUTTON_DRIVEN) FUSE_CHANGES.push(Date.now());
  log("apply:", STATE.activeOutput, "->", target);

  if (target === -1) {
    // A Pro1 has exactly one output: drive it directly rather than through
    // turnOffAllSwitches(), whose per-output queueTask hop would add a
    // needless 200ms to every stop on the device that actually runs the pump.
    if (STATE.deviceType !== "pro3") { setOutput(0, false, applyDone); return; }
    turnOffAllSwitches(applyDone);
    return;
  }
  if (STATE.deviceType === "pro3") {
    // Break-before-make: every other stage off first, then the target on.
    turnOffOtherOutputsNext(STATE.outputs, 0, target, applyOutputOn);
    return;
  }
  applyOutputOn();
}

// === BUTTON HANDLING ===
function cycleOutputs() {
  log("Button pressed, cycling outputs");

  // Check if water supply is active
  var input0 = Shelly.getComponentStatus("input:0");
  if (input0 && input0.state) {
    log("Water supply protection active, ignoring button press");
    return;
  }

  // Pro3 cycles all-off -> 0 -> 1 -> 2 -> all-off; a Pro1 simply toggles.
  var nextOutput = -1;
  if (STATE.deviceType !== "pro3") {
    if (STATE.activeOutput === -1) nextOutput = 0;
  } else if (STATE.activeOutput === -1) nextOutput = 0;
  else if (STATE.activeOutput === 0) nextOutput = 1;
  else if (STATE.activeOutput === 1) nextOutput = 2;
  log("Cycling from", STATE.activeOutput, "to", nextOutput);
  // #475 defect 1: this used to call setOutput()/turnOffAllSwitches()
  // directly and assign STATE.activeOutput itself, so a button-driven run
  // accrued no runtime and recorded no fuse change. It now writes a fact —
  // a manual override — and lets the reconciler drive the relay, so a button
  // run is accounted for exactly like a scheduled one.
  //
  // #549: F_OVR_IS_BUTTON marks this override as PHYSICAL-BUTTON-ORIGINATED,
  // which is what makes reconcileNow() never refuse it for the fuse and
  // applyOutput() never count its transition toward the fuse's window either
  // -- the maintainer's explicit decision: "manual button push does not
  // count against the fuse." Overnight evidence on filtration-hiver showed
  // the fuse and the override fighting each other (override wants ON, fuse
  // forces OFF, cooldown expires, override still wants ON, repeat) producing
  // the exact cycling the fuse exists to prevent. The one thing that still
  // overrides a button press is the water-supply interlock, unconditionally
  // ahead of the override branch in desiredOutput() -- untouched here.
  F_OVR_WANT = nextOutput;
  F_OVR_UNTIL = Date.now() + CONFIG.overrideMs;
  F_OVR_IS_BUTTON = true;
  reconcile("button");
}

// === EVENT HANDLERS ===
// Every handler is "write the fact, reconcile, return". None of them
// contains any decision about the relay: that lives in desiredOutput()
// alone, so there is nothing here for a second event to re-enter (#450).
function handleSwitchEvent(info) {
  log("Switch event:", info);

  // The relay moved. Believe the hardware — this is the only place
  // STATE.activeOutput is written outside applyDone(), and (#550) the only
  // place a genuinely out-of-band transition ever reaches
  // noteRelayTransition() at all, since applyOutput()/applyDone() are never
  // invoked for one.
  var observed = info.state ? info.id : -1;

  // An out-of-band change (web UI, daemon, physical toggle) that
  // contradicts the policy is adopted as a manual override, so the
  // reconciler does not undo it on the next 200ms tick. An event that
  // merely echoes our own actuation agrees with the policy by construction
  // and sets no override; neither does one arriving mid-actuation.
  if (!RC_BUSY) {
    var want = desiredOutput();
    if (want !== -2 && want !== observed) {
      log("override: adopting out-of-band", observed);
      F_OVR_WANT = observed;
      F_OVR_UNTIL = Date.now() + CONFIG.overrideMs;
      // #549: NOT a button press -- an out-of-band change (web UI, cloud,
      // another controller, a physical toggle wired separately from the
      // sys button) keeps the fuse exactly as before. Explicit, not merely
      // "leave F_OVR_IS_BUTTON alone", because this is the ONLY other writer
      // of F_OVR_WANT and a stale true here (left over from an earlier
      // button-driven override that a later out-of-band change replaces)
      // would silently exempt an out-of-band transition from the fuse.
      F_OVR_IS_BUTTON = false;
    }
  }

  // #550: was a direct "STATE.activeOutput = observed" write. Runtime
  // accounting now rides along on the same observation -- see
  // noteRelayTransition()'s comment above applyDone().
  noteRelayTransition(observed);
  saveState();
  reconcile("switch event");
}

// (handleInputEvent()/handleButtonEvent() inlined into the event dispatcher
// below -- each had exactly one call site, and both branches already carry
// the guard conditions that were checked again inside the old function
// bodies.)

// === SOLAR-DRIVEN HYSTERESIS (#405) ===
// Subscribes to the daemon's retained `myhome/energy/solar/available` topic
// (see #403) and layers a start/stop hysteresis on top of the existing
// forecast-driven schedule — never replacing it. Calls this script's own
// desiredOutput(), so the fuse and water-supply protection remain in force
// for solar-triggered runs exactly as for scheduled/manual runs.
//
// Staleness is judged from the payload's own `ts` field (unix-epoch-seconds,
// set by the daemon when it computed the value), NOT from local message
// receipt time. MQTT delivers retained messages to a new subscriber
// immediately regardless of how old they are — if the daemon published a
// value and then died, and this script reboots and re-subscribes hours or
// days later, it receives that old retained message immediately. Recording
// Date.now() as the freshness marker at *receipt* time would make a stale
// value look perfectly fresh, defeating the whole point of the staleness
// check. SOLAR.publishedTs (derived from data.ts) is what checkSolarHysteresis
// actually compares against Date.now(); SOLAR.lastMsgTs is kept only for
// debugging/visibility into when a message was last received at all.
var SOLAR = {
  availableW: 0,
  lastMsgTs: 0,       // Date.now() at last MQTT message receipt (debugging only)
  publishedTs: 0,     // data.ts converted to ms — the actual staleness clock
  aboveStartSince: 0,
  belowStopSince: 0,
  tickTimer: null
};

// #480 part 4: an uncaught throw inside an MQTT.subscribe callback kills the
// whole script (verified live on mezzanine with a real publish to a test
// topic). Wrapped in place -- same function, no new call frame, so dispatch
// isn't deeper than before.
function onSolarAvailable(topic, message) {
  try {
    var data = null;
    try {
      data = JSON.parse(message);
    } catch (e) {
      if (e && false) {}
      return;
    }
    if (!data || typeof data.available_w !== "number") return;

    SOLAR.availableW = data.available_w;
    SOLAR.lastMsgTs = Date.now();
    // ts is unix-epoch-seconds; convert to ms to compare against Date.now().
    // This is the field staleness decisions are based on — see comment above.
    if (typeof data.ts === "number") {
      SOLAR.publishedTs = data.ts * 1000;
    }

    checkSolarHysteresis();
  } catch (e) {
    log("mqtt handler error:", e);
  }
}

function subscribeSolarAvailable() {
  if (!CONFIG.solarEnabled) return;
  // Log BEFORE the subscribe, never after. Calling log() on the line
  // immediately following MQTT.subscribe() reliably kills the script with
  // "Too much recursion - the stack is about to overflow" on a Pro1, even
  // with ~23 KB of heap free (#474). Measured on `mezzanine` 2026-08-12:
  // this exact call after the subscribe crashes init every time; the same
  // call one line earlier runs fine. log() is only ever the innermost
  // frame, not the cause -- it is simply the first thing to touch the
  // stack on return from the subscribe.
  log("Subscribing to myhome/energy/solar/available");
  MQTT.subscribe("myhome/energy/solar/available", onSolarAvailable);
}

// Hard ceiling (pool volumes/day): pump always stops (and won't solar-start)
// once reached, regardless of solar availability.
//
// #502: ensureRuntimeDay() runs here, not only at load/init, because this is
// the read site a device with zero Schedule.List jobs (mezzanine) still
// reaches -- via desiredOutput() on every reconcile(), itself driven
// periodically by SOLAR.tickTimer even with no schedule at all. Even with a
// correct day anchor (see the #502 note on persistRuntimeState()), a device
// that never restarts and never runs the pump -- because the very ceiling
// it needs to reset is what's blocking it -- has no OTHER trigger that ever
// re-derives "today" against that anchor: loadState() only runs at boot,
// and startRuntimeAccounting/flushRuntimeCheckpoint only run while the pump
// is on. This is what actually breaks the catch-22 that left mezzanine
// stuck at 42002s; it is independent of, and still needed after, the
// anchor-persistence fix on persistRuntimeState() above.
function solarHardCeilingReached() {
  ensureRuntimeDay();
  var flowRate = computeFlowRate();
  if (!flowRate || flowRate <= 0 || !CONFIG.poolVolume) return false;
  var target = (CONFIG.poolVolume * CONFIG.solarMaxTurnover / flowRate) * 3600;
  return STATE.runtimeTodaySec >= target;
}

// Soft-stop target (pool volumes/day): solar keeps running past this while
// solar remains available, but stops once solar goes away.
// #502: same reasoning as solarHardCeilingReached() above.
function solarSoftTargetReached() {
  ensureRuntimeDay();
  var flowRate = computeFlowRate();
  if (!flowRate || flowRate <= 0 || !CONFIG.poolVolume) return false;
  var target = (CONFIG.poolVolume * CONFIG.solarMinTurnover / flowRate) * 3600;
  return STATE.runtimeTodaySec >= target;
}

// Solar start/stop hysteresis, re-evaluated on every solar MQTT message and
// on a periodic tick (see SOLAR.tickTimer in continueInit()) so staleness is
// still detected even if no further MQTT message ever arrives (e.g. the
// daemon dies while idle mid-hold-delay).
// #476: keeps its hold-delay logic but is now a FACT PRODUCER. It writes
// F_SOLAR_WANT (its own latched opinion, no longer the physical relay state)
// and asks the reconciler to converge; it never drives the relay itself.
//
// Reading the *physical* state here was subtly wrong: a window-driven run
// made `running` true, so solar skipped its start hysteresis entirely and
// went straight to its stop branch — meaning "solar went away" cut a
// scheduled run short. docs/pool-pump.md says solar layers on top of the
// forecast schedule and "never replac[es] it"; with a latched opinion that
// is finally true.
function solarWant(want) {
  if (want === F_SOLAR_WANT) {
    // The hard ceiling can be crossed without F_SOLAR_WANT moving.
    reconcile(null);
    return;
  }
  F_SOLAR_WANT = want;
  reconcile(want ? "solar available" : "solar gone");
}

function checkSolarHysteresis() {
  if (!CONFIG.solarEnabled) return;

  // Staleness is judged from the payload's own ts (SOLAR.publishedTs), not
  // from when we last received a message (SOLAR.lastMsgTs) — see the comment
  // on the SOLAR var above for why that distinction matters.
  var now = Date.now();
  if (SOLAR.publishedTs === 0 || (now - SOLAR.publishedTs) > CONFIG.solarStaleMs) {
    SOLAR.aboveStartSince = 0;
    SOLAR.belowStopSince = 0;
    solarWant(false);   // stale/absent: the forecast-driven window decides alone
    return;
  }

  if (!F_SOLAR_WANT) {
    if (SOLAR.availableW < CONFIG.solarStartThresholdW) {
      SOLAR.aboveStartSince = 0;
      solarWant(false);
      return;
    }
    if (SOLAR.aboveStartSince === 0) SOLAR.aboveStartSince = now;
    if (now - SOLAR.aboveStartSince < CONFIG.solarStartDelayMs) {
      solarWant(false);
      return;
    }
    SOLAR.aboveStartSince = 0;
    solarWant(true);
    return;
  }

  if (solarSoftTargetReached() && SOLAR.availableW < CONFIG.solarStartThresholdW) {
    SOLAR.belowStopSince = 0;
    solarWant(false);
    return;
  }
  if (SOLAR.availableW >= CONFIG.solarStopThresholdW) {
    SOLAR.belowStopSince = 0;
    solarWant(true);
    return;
  }
  if (SOLAR.belowStopSince === 0) SOLAR.belowStopSince = now;
  if (now - SOLAR.belowStopSince < CONFIG.solarStopDelayMs) {
    solarWant(true);
    return;
  }
  SOLAR.belowStopSince = 0;
  solarWant(false);
}

// === MQTT SETUP ===

function setupMQTT() {
  var mqttStatus = Shelly.getComponentStatus("mqtt");
  if (mqttStatus && mqttStatus.connected) {
    STATE.mqttConnected = true;
    log("MQTT connected");
  } else {
    log("MQTT not connected");
  }
  subscribeSolarAvailable();
}

// === SCHEDULE MANAGEMENT ===
//
// #524: this script no longer creates or clears its own Schedule.List jobs —
// createSchedules() and clearNonUpdateSchedules() were removed as dead code.
// They lost their only caller in 57b081f (2026-04-16, #504); schedule
// creation for a device now happens once, from Go, via
// PoolService.reconcileSchedules() (internal/myhome/shelly/script/pool.go),
// and this script only ever reads and rewrites existing jobs
// (updateScheduleMode(), onWindowJobs()).
//
// #480/#524: schedule jobs used to be registered from this script via
// createSchedules(), which wrapped every script.eval payload in
// try/catch(wrapScheduleCall()) so an uncaught throw inside a handler could
// never kill the whole script. That call site is gone (#524: createSchedules()
// removed as dead code; schedule creation now happens once, from Go, via
// PoolService.reconcileSchedules() — see the note above — and its
// buildJobSpec() writes the bare handler call, unwrapped). Code read back off
// a live Schedule.List (onWindowJobs, updateScheduleMode, and the "verify
// schedules" init step below -- verifySchedules() inlined there, single call
// site) still matches by substring CONTAINMENT rather than exact equality,
// which is a superset of exact matching and costs nothing extra — so this
// stays correct whether or not a future caller ever wraps the code again.

// === SCHEDULE MODE MANAGEMENT ===

// Parse fractional hour from Open-Meteo ISO timestamp ("2025-07-15T06:15")
function parseHourFromISO(isoStr) {
  var tIdx = isoStr.indexOf('T');
  if (tIdx < 0) return null;
  var timePart = isoStr.slice(tIdx + 1);
  var colonIdx = timePart.indexOf(':');
  if (colonIdx < 0) return Number(timePart);
  var h = Number(timePart.slice(0, colonIdx));
  var m = Number(timePart.slice(colonIdx + 1, colonIdx + 3));
  if (isNaN(h)) return null;
  if (isNaN(m)) m = 0;
  return h + m / 60;
}

function lpad2(n) {
  return n < 10 ? '0' + n : String(n);
}

// Flow rate (m3/h) at the speed this device actually runs at — see
// effectiveSpeed(): on a Pro1 that is always 'max', regardless of what the KVS
// says, because the pump has one stage.
function computeFlowRate() {
  var speedRpms = {eco: CONFIG.ecoRpm, day: CONFIG.dayRpm, max: CONFIG.maxRpm};
  var rpm = speedRpms[effectiveSpeed(CONFIG.preferredSpeed)];
  if (!rpm) rpm = CONFIG.maxRpm;
  return CONFIG.maxFlowRate * (rpm / CONFIG.maxRpm);
}

// Daily run hours proportional to today's max temperature
function computeRunHours(maxForecastTemp) {
  var flowRate = computeFlowRate();
  if (!flowRate || flowRate <= 0) {
    log('WARNING: invalid flow rate, defaulting run hours to 8');
    return 8;
  }
  var baseHours = (CONFIG.poolVolume * CONFIG.turnover) / flowRate;
  var minHours  = baseHours * 0.5;
  var maxHours  = baseHours * 1.5;
  var range = CONFIG.maxTemp - CONFIG.temperatureThreshold;
  var scale = range > 0 ? (maxForecastTemp - CONFIG.temperatureThreshold) / range : 1;
  if (scale < 0) scale = 0;
  if (scale > 1) scale = 1;
  var runHours = baseHours * scale;
  if (runHours < minHours) runHours = minHours;
  if (runHours > maxHours) runHours = maxHours;
  return runHours;
}

// Build a Shelly cron timespec string from a fractional hour (e.g. 9.625 → "0 37 9 * * SUN,...")
function makeTimespec(fractHours) {
  if (fractHours < 0) fractHours = 0;
  if (fractHours >= 24) fractHours = 23.99;
  var h = Math.floor(fractHours);
  var m = Math.round((fractHours - h) * 60);
  if (m >= 60) { h++; m = 0; }
  h = h % 24;
  return '0 ' + m + ' ' + h + ' * * SUN,MON,TUE,WED,THU,FRI,SAT';
}

// Inverse of makeTimespec(): pulls the "M H" fields out of a Shelly cron
// timespec ("0 M H * * DAYS") and returns minutes since midnight. Returns
// null for a spec that carries no concrete time yet — notably the symbolic
// "@sunrise"/"@sunset" forms createSchedules() lays down before
// updateScheduleMode() has ever rewritten them. Callers must treat null as
// "unknown", never as a time.
function parseHM(ts) {
  if (!ts || ts.indexOf('@') === 0) return null;
  var p = ts.split(' ');
  if (p.length < 3) return null;
  var m = Number(p[1]);
  var h = Number(p[2]);
  if (isNaN(h) || isNaN(m)) return null;
  return h * 60 + m;
}

// True when applying `upd` to the Schedule.List `job` it was built from would
// actually move the run window — the job gets enabled or disabled, or it gets
// a start/stop time different from the one already on the device.
function moves(job, upd) {
  return upd.enable !== job.enable || (!!upd.timespec && upd.timespec !== job.timespec);
}

function updateScheduleMode(newMode, morningStartHours, eveningStopHours) {
  var hasTimings = morningStartHours !== null && morningStartHours !== undefined;
  var modeChanged = STATE.scheduleMode !== newMode;

  if (!modeChanged && !hasTimings) {
    log('Mode already:', newMode, '- no changes needed');
    return;   // nothing written, so the window fact cannot have moved
  }

  if (modeChanged) {
    log('MODE CHANGE:', STATE.scheduleMode || 'unknown', '->', newMode);
    log(newMode === 'summer' ? '  Summer: enabling morning/evening schedules' : '  Winter: enabling night schedules only');
    STATE.scheduleMode = newMode;
    saveState();
  } else {
    log('Updating', newMode, 'schedule times');
  }

  // Enable/disable schedules based on mode; include new timespec for summer timings
  Shelly.call('Schedule.List', {}, function(result, err) {
    if (err) {
      log('ERROR: Failed to list schedules:', err);
      if (err && false) {}
      return;
    }

    if (!result || !result.jobs) {
      log('No schedules found');
      return;
    }

    var schedulesToUpdate = [];
    // #441: set when this call actually moves the active run window (a job is
    // enabled/disabled, or a start/stop time is rewritten to a different
    // value). A no-op re-run — the common case at sunrise when the forecast
    // yields the same window as yesterday — leaves it false so we don't
    // re-reconcile for nothing.
    var windowChanged = false;
    for (var i = 0; i < result.jobs.length; i++) {
      var job = result.jobs[i];
      if (job.calls && job.calls.length > 0) {
        var code = job.calls[0].params && job.calls[0].params.code;
        // #480: code is wrapScheduleCall()'s wrapped source, not the bare
        // handler call, so match by containment. `name` is set to the short
        // handler literal (not the matched `code`) so log lines stay
        // readable instead of printing the whole wrapper.
        if (code && code.indexOf('handleMorningStart()') !== -1) {
          var updM = {id: job.id, enable: newMode === 'summer', name: 'handleMorningStart()'};
          if (hasTimings && newMode === 'summer') {
            updM.timespec = makeTimespec(morningStartHours);
          }
          if (moves(job, updM)) windowChanged = true;
          // #509: mutate the job already in hand to the value we are about to
          // write, instead of allocating anything new. `result.jobs` then
          // already reflects the post-update state, which is what lets
          // updateNext()'s completion below derive the window without a
          // second Schedule.List.
          job.enable = updM.enable;
          if (updM.timespec) job.timespec = updM.timespec;
          schedulesToUpdate.push(updM);
        } else if (code && code.indexOf('handleEveningStop()') !== -1) {
          var updE = {id: job.id, enable: newMode === 'summer', name: 'handleEveningStop()'};
          if (hasTimings && newMode === 'summer') {
            updE.timespec = makeTimespec(eveningStopHours);
          }
          if (moves(job, updE)) windowChanged = true;
          job.enable = updE.enable;
          if (updE.timespec) job.timespec = updE.timespec;
          schedulesToUpdate.push(updE);
        } else if (code && (code.indexOf('handleNightStart()') !== -1 || code.indexOf('handleNightStop()') !== -1)) {
          var updN = {id: job.id, enable: newMode === 'winter', name: code.indexOf('handleNightStart()') !== -1 ? 'handleNightStart()' : 'handleNightStop()'};
          if (moves(job, updN)) windowChanged = true;
          job.enable = updN.enable;
          if (updN.timespec) job.timespec = updN.timespec;
          schedulesToUpdate.push(updN);
        }
      }
    }

    if (schedulesToUpdate.length === 0) {
      log('No schedules to update');
      return;
    }

    var updateIndex = 0;
    function updateNext() {
      if (updateIndex >= schedulesToUpdate.length) {
        log('All schedules updated for', newMode, 'mode');
        // #441: the window we just wrote may already contain (or no longer
        // contain) "now" — e.g. the sunrise re-forecast moves the morning
        // start into the past, or a restart re-runs this after the old
        // window's start instant has passed. Nothing else re-evaluates:
        // handleMorningStart()/handleEveningStop() only ever fire at their
        // scheduled instant, and a schedule that was rewritten past that
        // instant never fires at all — which is how the pool lost a whole
        // day of filtration on 2026-08-06. Reconcile here, once, in the one
        // place that knows the window moved.
        // #476: the window is a fact with one writer.
        //
        // #509: this used to re-read via queueTask(readWindow), a second
        // Schedule.List landing on top of whatever forecast parse just ran.
        // Measured ~670 bytes of mem_peak for that alone on `mezzanine`
        // 2026-08-12, and on `filtration-hiver` 2026-08-17 -- with only
        // ~1.2 KB of heap headroom left -- it reclaimed the whole script with
        // no trace, mid schedule-rewrite. The information onWindowJobs()
        // needs is already in hand: the loop above just mutated `job.enable`
        // / `job.timespec` on every job in `result.jobs` to the exact values
        // `schedulesToUpdate` wrote via Schedule.Update, so result.jobs
        // already IS the post-update state. Pass it straight to onWindowJobs
        // (named function reference, no closure) instead of re-fetching and
        // re-parsing it.
        //
        // #524 review (silent-failure pass): this call USED to be gated on
        // windowChanged -- "a no-op re-run must not buy any extra work at
        // all" -- which was sound when F_WIN_STOP had exactly one writer
        // (setWindow() via this same onWindowJobs() call). extendWindowForShortfall()
        // is now a second writer that can move F_WIN_STOP without touching any
        // schedule job, so fact and schedule can diverge. Gating this call on
        // windowChanged meant that on any morning whose recomputed window
        // matched the jobs already on the device -- the common case at
        // sunrise -- onWindowJobs() never ran, and a leftover extension from
        // the day before silently carried into today: since extension only
        // ever moves the stop forward, F_WIN_STOP would ratchet toward
        // stopCeil across days with nothing to pull it back (and sunset drifts
        // 1-2 min/day, so a leaked stop could even end up ABOVE today's
        // stopCeil, which is only enforced on a freshly computed value).
        // Calling onWindowJobs() unconditionally re-seeds F_WIN_STOP from the
        // schedule every single time this runs -- once/day via
        // handleDailyCheck() -- which costs one extra no-op reconcile() on the
        // common no-rewrite day and nothing else: no RPC, since result.jobs is
        // already in hand (the #509 fix above).
        onWindowJobs(result, null);
        return;
      }
      var sched = schedulesToUpdate[updateIndex];
      updateIndex++;
      var params = {id: sched.id, enable: sched.enable};
      if (sched.timespec) {
        params.timespec = sched.timespec;
      }
      Shelly.call('Schedule.Update', params, function(res, err) {
        if (err && false) {}
        var msg = sched.name + ' ' + (sched.enable ? 'ENABLED' : 'DISABLED');
        if (sched.timespec) msg = msg + ' (' + sched.timespec + ')';
        log('Schedule', msg);
        queueTask(updateNext);
      });
    }

    updateNext();
  });
}

function performDailyModeCheck() {
  log('Performing daily mode check...');

  // Ensure forecast URL is configured
  // (ensureForecastUrl() inlined -- single call site; was the tail
  // statement of performDailyModeCheck, so its early returns below are
  // equivalent to returning from performDailyModeCheck itself.)
  var ensureForecastUrlCb = function() {
    // Fetch or use cached forecast
    // (shouldRefreshForecast() inlined -- single call site.)
    if (STATE.lastForecastFetchDate === null || STATE.lastForecastFetchDate !== todayDateString()) {
      log('Fetching fresh forecast for mode check...');
      // (fetchAndCacheForecast() inlined -- single call site; tail
      // position within this branch.)
      var fetchAndCacheForecastCb = function() {
        decideModeFromForecast();
      };
      var url = STATE.forecastUrl || loadStorageString(STORAGE_KEYS.forecastUrl);
      if (!url) {
        log('Forecast URL not configured. Skipping.');
        if (typeof fetchAndCacheForecastCb === 'function') queueTask(function() { fetchAndCacheForecastCb(); });
        return;
      }
      log('Fetching forecast...');
      Shelly.call("HTTP.GET", {
        url: url,
        timeout: 10
      }, onForecast, fetchAndCacheForecastCb);
    } else {
      log('Using cached forecast for mode check');
      decideModeFromForecast();
    }
  };

  if (STATE.forecastUrl) {
    if (typeof ensureForecastUrlCb === 'function') queueTask(function() { ensureForecastUrlCb(); });
    return;
  }

  var storedUrl = loadStorageString(STORAGE_KEYS.forecastUrl);
  if (storedUrl && storedUrl.indexOf('daily=') !== -1) {
    STATE.forecastUrl = storedUrl;
    log('Loaded forecast URL from storage');
    if (typeof ensureForecastUrlCb === 'function') queueTask(function() { ensureForecastUrlCb(); });
    return;
  }

  log('Forecast URL not found, detecting location...');
  Shelly.call('Shelly.DetectLocation', {}, onDeviceLocation, ensureForecastUrlCb);
}

function decideModeFromForecast() {
  // (getMaxForecastTemp() inlined -- single call site.)
  var maxTemp = STATE.maxForecastTemp;

  if (maxTemp === null) {
    log('No forecast data available, keeping mode:', STATE.scheduleMode || 'winter');
    return;
  }

  log('Forecast max temp:', maxTemp + 'C', '(threshold:', CONFIG.temperatureThreshold + 'C)');
  var newMode = maxTemp > CONFIG.temperatureThreshold ? 'summer' : 'winter';
  log('Selected mode:', newMode, maxTemp > CONFIG.temperatureThreshold ? '(above threshold)' : '(below threshold)');

  if (newMode !== 'summer') {
    Shelly.emitEvent("pool.run_window", {mode: "winter", max_temp_c: maxTemp});
    updateScheduleMode(newMode, null, null);
    return;
  }

  var runHours   = computeRunHours(maxTemp);
  var peakHour   = STATE.peakForecastHour !== null ? STATE.peakForecastHour : 14;
  var startFloor = (STATE.sunriseHour !== null ? STATE.sunriseHour : 6) + 1;
  var stopCeil   = (STATE.sunsetHour  !== null ? STATE.sunsetHour  : 21) - 0.5;

  var startHour = peakHour - runHours / 2;
  var stopHour  = peakHour + runHours / 2;

  // Shift window forward if start is too early
  if (startHour < startFloor) {
    startHour = startFloor;
    stopHour  = startFloor + runHours;
  }
  // Shift window backward if stop is too late
  if (stopHour > stopCeil) {
    stopHour  = stopCeil;
    startHour = stopCeil - runHours;
  }
  // Hard floor after both shifts
  if (startHour < startFloor) startHour = startFloor;

  log('Run hours:', Math.round(runHours * 10) / 10,
      'Start:', Math.floor(startHour) + ':' + lpad2(Math.round((startHour % 1) * 60)),
      'Stop:',  Math.floor(stopHour)  + ':' + lpad2(Math.round((stopHour  % 1) * 60)));

  Shelly.emitEvent("pool.run_window", {
    mode: "summer",
    max_temp_c: maxTemp,
    run_hours: Math.round(runHours * 10) / 10,
    start_h: Math.round(startHour * 100) / 100,
    stop_h:  Math.round(stopHour  * 100) / 100
  });

  updateScheduleMode(newMode, startHour, stopHour);
}

// === SCHEDULE EVENT HANDLERS ===
function handleDailyCheck() {
  log('Daily check event');

  // #502: this @sunrise job runs every day in both summer and winter mode,
  // unlike handleNightStop()'s midnight reset (winter-only). It is the
  // reset site that actually fired on filtration-hiver while the counter
  // stayed stale -- because this function never called ensureRuntimeDay().
  ensureRuntimeDay();

  // #450: this used to skip performDailyModeCheck() entirely while
  // water-supply protection was active. That silently skipped the
  // summer/winter MODE decision too, not just pump actuation -- observed
  // live on 2026-08-11: the script crashed and restarted with protection
  // still active, the next daily check hit this guard and left the device
  // on 'winter' scheduling on an August evening, and it only self-corrected
  // on a LATER restart/daily-check once protection had cleared.
  //
  // performDailyModeCheck() -> updateScheduleMode() only reads the forecast
  // and rewrites Schedule.List jobs; it does not touch the physical output
  // directly. The one way it can move the pump is by moving the run-window
  // FACT (setWindow), and the policy checks F_WATER first, so a reconcile
  // during protection can only ever settle on "off". Running the daily
  // check during protection is therefore safe; skipping it just delayed a
  // correct decision for no safety benefit.
  performDailyModeCheck();
}

// A schedule edge is the natural expiry for a manual override: whatever the
// maintainer wanted by hand, they did not mean it to survive the next
// scheduled start or stop.
function handleMorningStart() {
  clearOverride();
  reconcile('morning start');
}

function handleEveningStop() {
  // #524: the window's natural end is exactly where a shortfall must be
  // recovered or lost outright -- extend before reconciling so today's
  // achieved runtime still has a chance to reach computeRunHours()'s intent.
  extendWindowForShortfall();
  clearOverride();
  reconcile('evening stop');
}

function handleNightStart() {
  clearOverride();
  reconcile('night start');
}

function handleNightStop() {
  // Belt-and-suspenders midnight reset, in addition to the lazy per-write
  // check in persistRuntimeState/ensureRuntimeDay (#402).
  ensureRuntimeDay();
  clearOverride();
  reconcile('night stop');
}


// === #550: RECONCILE A TRANSITION MISSED WHILE THE SCRIPT WAS DOWN ===
//
// noteRelayTransition() covers every transition observed WHILE the script is
// running -- that channel cannot be beaten by delivery order (see its
// comment above applyDone()). The one window it cannot see through is the
// script not running at all: if the script is up, its event handler
// receives every relay transition (that is the whole premise #550 relies on
// to close the web-UI gap); if it is down, it cannot do accounting at any
// point during the outage regardless of design. That whole class is closed
// here, not with a periodic re-check: enforceOutputState() already reads
// real hardware truth right after boot ("hardware truth overrides any stale
// KVS value" — see finishLoadState()'s comment), which is exactly the
// reconciliation point needed.
//
// MISSED_STOP_TS carries the one value reconcileMissedStop() needs across
// the queueTask() hop below without allocating a closure — same pattern as
// RC_TARGET for applyOutputOn().
var MISSED_STOP_TS = null;

function reconcileMissedStop() {
  var restoredStartTs = MISSED_STOP_TS;
  MISSED_STOP_TS = null;
  if (restoredStartTs === null) return;
  ensureRuntimeDay();
  // #550: the relay is OFF now but Script.storage still remembers an open
  // interval from before this boot -- the matching stop transition happened
  // while the script could not have observed it.
  //
  // This is NOT the mirror of the missed-START branch below, and crediting
  // "now - restoredStartTs" here (an earlier version of this function did
  // exactly that) is a bug that inverts the safety direction, not a harmless
  // symmetry:
  //
  //   - missed START: the relay is ON, so the interval is opened AT INIT
  //     TIME. The real start was earlier than that, so this credits LESS
  //     than reality -- under-credit, bounded, safe.
  //   - missed STOP: the relay is OFF now, but the real stop happened at
  //     some unknown point DURING the outage, strictly before "now". Crediting
  //     up to "now" credits MORE than reality, by an amount equal to however
  //     long the pump was actually off before this restart -- an outage of
  //     hours or days (#530 saw days) turns into hours or days of pure
  //     over-credit. #526's runtime recovery would then believe the day's
  //     filtration is satisfied when it is not, and SKIP filtration the pool
  //     actually needs -- silently, which is strictly worse than the
  //     under-credit direction (#526 just runs the pump a little more; a
  //     cost in electricity, not in water quality).
  //
  // Nothing anywhere records when the relay actually went off during the
  // outage -- persistRuntimeState()'s last-flushed {sec, ts} already
  // contains everything known up to the last checkpoint or stop, and that is
  // genuinely all there is. So this interval's duration is unrecoverable,
  // and the only safe choice is to credit ZERO: clear the marker, log the
  // gap, and let the day under-count rather than invent a number.
  storeStorageValue(STORAGE_KEYS.runStart, null);
  log("#550: reconciling a stop missed during a script outage (relay OFF, " +
      "open marker from", restoredStartTs, ") - duration unrecoverable, crediting 0s");
}

// === INITIALIZATION ===
function enforceOutputState() {
  log("Enforcing output state at startup...");
  
  if (STATE.deviceType === "pro3") {
    // Ensure only one output is on
    var onOutputs = [];
    for (var i = 0; i < STATE.outputs.length; i++) {
      var outputId = STATE.outputs[i];
      var status = Shelly.getComponentStatus("switch:" + outputId);
      if (status && status.output) {
        onOutputs.push(outputId);
      }
    }
    
    if (onOutputs.length > 1) {
      // Do not actuate from here — nothing outside the actuator may. Record
      // the state as "not a valid single output" and let the reconciler
      // repair it: whatever desiredOutput() returns, applyOutput() breaks
      // before it makes, so exactly one stage ends up on.
      log("Multiple outputs on:", onOutputs[0], "and others - reconciler will resolve");
      STATE.activeOutput = -1;
      saveState();
    } else if (onOutputs.length === 1) {
      STATE.activeOutput = onOutputs[0];
      saveState();
    } else {
      STATE.activeOutput = -1;
      saveState();
    }
  } else {
    // Pro1
    var status = Shelly.getComponentStatus("switch:0");
    if (status) {
      STATE.activeOutput = status.output ? 0 : -1;
      saveState();
    }
  }
  
  log("Current active output:", STATE.activeOutput);

  // If the pump was already running when the script (re)started, resume
  // runtime accounting so an in-progress run keeps accruing across restarts
  // (#402). If the policy then decides the pump should not be running, the
  // reconciler stops it and applyDone() closes the interval properly, so
  // the seconds between restart and that decision are still counted.
  //
  // #476: nothing is adopted as an override here. A restart re-derives the
  // relay from the policy, which is the whole point of #421/#441 — a
  // remembered value must never outrank the run window. The cost, recorded
  // deliberately: a crash-restart OUTSIDE the run window now stops a pump
  // that was running by hand, because no switch:N event survives a restart
  // to have set an override. Stopping is the safe direction.
  //
  // #474 class: startRuntimeAccounting() must NOT run inline here either.
  // enforceOutputState() is itself called synchronously at the top of
  // finishContinueInit(), i.e. still inside the fully synchronous
  // init -> loadConfig(cb) -> continueInit -> loadState -> finishContinueInit
  // chain that #474 already found the interpreter stack has no headroom
  // left for by the time it reaches this depth. Calling
  // startRuntimeAccounting() -> ensureRuntimeDay() -> reconcileRuntimeState()
  // -> log() inline crashed at init on `mezzanine` (Pro1) 2026-08-12 with
  // "Too much recursion" whenever the pump/light was already ON at script
  // restart (STATE.activeOutput !== -1) -- exactly the #474 mechanism, at a
  // second call site the reconciler refactor introduced. Deferred the same
  // way as setupMQTT(): a named function passed to queueTask allocates no
  // closure and runs on a fresh stack. reconcileMissedStop() below is
  // deferred for the identical reason -- same call site, same depth.
  //
  // #550: the second reconciliation case -- NOT a mirror of the branch
  // above, despite the superficial symmetry (see reconcileMissedStop()'s
  // comment for why crediting the two the same way is a bug, not a
  // simplification). STATE.activeOutput above is real hardware truth;
  // restoredRunStart is what Script.storage last remembered about an
  // interval that may still be open.
  //
  // Relay ON always wins the race to "start fresh at init time" regardless
  // of restoredRunStart: the exact pre-restart start instant is
  // unrecoverable either way, and starting fresh under-credits by
  // construction (safe -- see reconcileMissedStop()).
  //
  // Relay OFF with restoredRunStart still set means the matching stop
  // happened while the script could not observe it (see
  // reconcileMissedStop()'s comment for why that can only happen across an
  // outage of the script itself, never while it is running) -- and unlike
  // the ON case, there is no safe "credit to now" here: reconcileMissedStop()
  // credits zero rather than guess.
  var restoredRunStart = loadStorageNumber(STORAGE_KEYS.runStart);
  if (STATE.activeOutput !== -1) {
    queueTask(startRuntimeAccounting);
  } else if (restoredRunStart !== null) {
    MISSED_STOP_TS = restoredRunStart;
    queueTask(reconcileMissedStop);
  }
}

function init() {
  log("Script starting...");
  
  // Load configuration from KVS first (asynchronous)
  loadConfig(function(success) {
    if (!success) {
      log("FATAL: Configuration validation failed, script cannot start");
      return;
    }
    
    continueInit();
  });
}

function continueInit() {
  // Device type and ID are already detected in loadConfig
  // Just log them here for confirmation
  log('Device type:', STATE.deviceType);

  configureComponentNames();
  // loadState() is synchronous in the common case (Script.storage has valid
  // data) and calls finishContinueInit() immediately, before returning here
  // — so this is not a behavior change for a healthy device. It only goes
  // truly async when Script.storage yields nothing usable and it falls back
  // to an async KVS read (#469); the rest of init correctly waits for that.
  loadState(finishContinueInit);
}

function finishContinueInit() {
  enforceOutputState();
  // #474: setupMQTT() must NOT run inline here. init -> loadConfig(cb) ->
  // continueInit -> loadState -> finishLoadState -> finishContinueInit is a
  // fully synchronous chain that never returns to the event loop, and
  // subscribeSolarAvailable()'s log() call at the bottom of it overflows the
  // Espruino interpreter stack -- with 23044 bytes of heap still free.
  // Measured on `mezzanine` (Pro1) 2026-08-12: identical bytes crash with
  // solar-enabled=true and run with this one line changed to queueTask
  // (mem_peak 20636, mem_free 7392, same peak as the solar-off arm, so the
  // solar path costs no heap at all). With solar-enabled=false the crash
  // never showed because subscribeSolarAvailable() returns before reaching
  // log() -- which is the only reason the bug looked solar-specific.
  // queueTask takes a NAMED function, so this allocates no closure.
  queueTask(setupMQTT);

  // Solar hysteresis (#405): re-evaluate periodically so staleness is
  // detected even if the daemon dies mid-hold-delay and no further MQTT
  // message ever arrives. This is at most the 2nd concurrent Timer.set()
  // (alongside TASK_TIMER; #547 removed the periodic runtime-checkpoint
  // timer that used to also be in this budget) — see docs/pool-pump.md
  // "Timer Budget".
  if (CONFIG.solarEnabled) {
    SOLAR.tickTimer = Timer.set(30000, true, checkSolarHysteresis);
  }

  // Initialization complete - enable state persistence and flush initial state to KVS
  STATE.initializing = false;
  saveState();

  log("Script initialization complete");

  // Unified initialization for all devices
  var initSteps = [
    function(next) {
      log('Step 1/4: Disabling sys_btn_toggle...');
      Shelly.call('Sys.SetConfig', {config: {device: {sys_btn_toggle: false}}}, function(res, err) {
        if (err) {
          log('WARNING: Failed to disable sys_btn_toggle:', err);
          if (err && false) {}
        } else {
          log('sys_btn_toggle disabled (script handles button)');
        }
        next();
      });
    },
    function(next) {
      log('Step 2/4: Checking water supply status...');
      var input0 = Shelly.getComponentStatus('input:0');
      if (input0 && input0.state) setWater(true);
      next();
    },
    function(next) {
      log('Step 3/4: Configuring component names...');
      applyComponentNames(next);
    },
    function(next) {
      log('Step 4/4: Verifying schedules...');
      // Only Pro3 has schedules, but all devices verify
      // (verifySchedules() inlined -- single call site, and its whole body
      // was already one statement: this Shelly.call.)
      Shelly.call('Schedule.List', {}, function(result, err) {
        if (err) {
          log('WARNING: Cannot verify schedules:', err);
          if (typeof next === 'function') queueTask(function() { next(); });
          return;
        }

        var hasPoolSchedules = false;
        if (result && result.jobs) {
          for (var i = 0; i < result.jobs.length; i++) {
            var job = result.jobs[i];
            if (job.calls && job.calls.length > 0 && job.calls[0].method === 'script.eval') {
              var code = job.calls[0].params && job.calls[0].params.code;
              // #480: code carries wrapScheduleCall()'s wrapper boilerplate, not
              // just the bare handler call, so match by containment rather than
              // by prefix.
              if (code && (code.indexOf('handleNight') !== -1 || code.indexOf('handleDaily') !== -1 || code.indexOf('handleMorning') !== -1 || code.indexOf('handleEvening') !== -1)) {
                hasPoolSchedules = true;
                break;
              }
            }
          }
        }

        if (!hasPoolSchedules) {
          log('FATAL: No pool pump schedules found. Run: ctl pool setup');
          // Stop script - schedules are required for operation
          return;
        }

        log('Pool pump schedules verified');
        // #476: seed the run-window fact from the jobs we already have in hand.
        // A separate Schedule.List at init cost ~670 bytes of mem_peak on a Pro1
        // (measured on `mezzanine` 2026-08-12) purely to parse the same response
        // twice. From here on the window is owned by setWindow(), rewritten only
        // by updateScheduleMode().
        onWindowJobs(result, null);
        if (typeof next === 'function') queueTask(function() { next(); });
      });
    }
  ];

  var stepIndex = 0;

  function runNextStep() {
    if (stepIndex >= initSteps.length) {
      log('OK: All initialization steps complete - script is now running');
      // #421: assert the in-flight RPC tracking this script's crash-safety
      // depends on is actually live, and shout if it is not.
      checkTrack();
      log('Current mode:', STATE.scheduleMode || 'winter');
      queueTask(handleDailyCheck);
      return;
    }

    var step = initSteps[stepIndex];
    stepIndex++;

    step(function() {
      queueTask(runNextStep);
    });
  }

  queueTask(runNextStep);
}

// === EVENT SUBSCRIPTION ===
// #480 part 4: an uncaught throw inside this handler kills the whole script
// (verified live on mezzanine: a throwing addEventHandler callback, triggered
// by a real Switch.Set, crashed the script identically to the unwrapped
// queueTask/script.eval cases). Wrapped in place -- no extra function layer
// around the callback, so dispatch adds no new call frame; every event still
// goes through exactly the frames it did before, just with a try/catch
// around the existing body.
Shelly.addEventHandler(function(event) {
  try {
    if (!event || !event.info) return;

    var info = event.info;

    // Handle script stop event
    if (info.event === "script_stop") {
      log("Script stopping");
      return;
    }

    // Handle component events
    if (typeof info.component === "string") {
      if (info.component.indexOf("switch:") === 0 && typeof info.state === "boolean") {
        handleSwitchEvent(info);
      } else if (info.component.indexOf("input:") === 0 && typeof info.state === "boolean") {
        // (handleInputEvent() inlined -- single call site.)
        log("Input event:", info);
        // Handle input:0 (water-supply)
        if (info.id === 0) {
          setWater(info.state);
        }
        // Input:1 (high-water) and input:2 (max-speed-active) are just notifications
      } else if (info.component === "sys" && info.event === "sys_btn_push") {
        // (handleButtonEvent() inlined -- single call site.)
        log("Button event:", info);
        cycleOutputs();
      } else {
        log("Unhandled component event:", JSON.stringify(info));
      }
    } else {
      log("Unhandled event:", JSON.stringify(info));
    }
  } catch (e) {
    log("event handler error:", e);
  }
});

// Start the script
init();