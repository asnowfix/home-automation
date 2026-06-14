// garden.js — ET₀ water-balance garden sprinkler controller
// Target device: Shelly Pro3 (device named "arrosage", 192.168.1.83)
//
// Zone mapping:
//   switch:0  pelouse-maison   (lawn, house side)
//   switch:1  massifs          (flower beds)
//   switch:2  pelouse-barriere (lawn, fence side)
//
// Power supply supports ONE valve at a time — enforced in hardware and software.
// All logic runs on-device; no daemon dependency (resilience per CLAUDE.md).
// Forecast: Open-Meteo (free, no API key); 10 s timeout, fallback on offline.

var SCRIPT_NAME = "garden";
var CONFIG_KEY_PREFIX = "script/" + SCRIPT_NAME + "/";
var SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";
var NUM_ZONES = 3;
var PAST_DAYS = 3; // days of past data in query: daily[PAST_DAYS-1]=yesterday, daily[PAST_DAYS]=today

// === CONFIG SCHEMA (global settings; zone config lives in ZONE_DEFAULTS) ===
// KVS key lengths: prefix 14 chars + suffix ≤18 chars = ≤32 chars total
var CONFIG_SCHEMA = {
  enableLogging: {
    description: "Enable debug logging",
    key: "logging",
    default: true,
    type: "boolean"
  },
  mqttTopicPrefix: {
    description: "MQTT topic prefix (CLI metadata only, not used at runtime)",
    key: "mqtt-topic",
    default: "garden",
    type: "string",
    cliOnly: true
  },
  earliestStartHour: {
    description: "Earliest allowed watering start (0-23)",
    key: "earliest-start",
    default: 3,
    type: "number"
  },
  lunchStart: {
    description: "Midday quiet window start (fractional hour)",
    key: "lunch-start",
    default: 12.0,
    type: "number"
  },
  lunchEnd: {
    description: "Midday quiet window end (fractional hour)",
    key: "lunch-end",
    default: 14.0,
    type: "number"
  },
  eveningStart: {
    description: "Evening quiet window start (fractional hour)",
    key: "evening-start",
    default: 19.0,
    type: "number"
  },
  eveningEnd: {
    description: "Evening quiet window end (fractional hour)",
    key: "evening-end",
    default: 23.5,
    type: "number"
  },
  fallbackStartHour: {
    description: "Start hour when forecast unavailable",
    key: "fallback-start",
    default: 5,
    type: "number"
  },
  frostCutoffC: {
    description: "Skip watering if forecast min temp in window < this (C)",
    key: "frost-cutoff-c",
    default: 2,
    type: "number"
  },
  rainHoldoffMm: {
    description: "Skip watering if today's forecast rain >= this value (mm)",
    key: "rain-holdoff-mm",
    default: 8,
    type: "number"
  },
  maxDeficitMm: {
    description: "Maximum soil water deficit cap (mm)",
    key: "max-deficit-mm",
    default: 25,
    type: "number"
  }
};

// Per-zone defaults — calibrate appRateMmH via 'ctl garden calibrate'
// appRateMmH: water delivery rate (mm/h).
//   Grass zones (0, 2): 2 pop-up heads per zone, each measured at 96 mm/h (8 mm/5 min),
//   covering the same ground simultaneously → 192 mm/h.
//   Massifs zone (1): drip pipe — set after measuring or via KVS.
// kc: crop coefficient — ET0 multiplier. Lawn 0.8, ornamental beds 0.6.
// triggerMm: water when deficit reaches this depth (deep-infrequent irrigation).
//   Grass: 12 mm ≈ 2.5 days of peak-summer ETc (ET0=6 mm/d × kc=0.8).
//   At 192 mm/h the planner delivers 12 mm in ~3.75 min, 25 mm in ~7.8 min.
// fallbackMin: used when no forecast is available (internet outage).
//   Grass: 8 min delivers ~25.6 mm ≈ 5-day peak-summer deficit.
// maxMin: hard cap per session; 15 min @ 192 mm/h = 48 mm (more than maxDeficitMm=25).
var ZONE_DEFAULTS = [
  {id: 0, name: "pelouse-maison",   appRateMmH: 192.0, kc: 0.8, triggerMm: 12.0, maxMin: 15, fallbackMin: 8, enabled: true},
  {id: 1, name: "massifs",          appRateMmH:  18.0, kc: 0.6, triggerMm:  8.0, maxMin: 30, fallbackMin: 15, enabled: true},
  {id: 2, name: "pelouse-barriere", appRateMmH: 192.0, kc: 0.8, triggerMm: 12.0, maxMin: 15, fallbackMin: 8, enabled: true}
];

// Runtime config — populated from defaults then overridden by KVS at startup
var CONFIG = {};

function initConfig() {
  for (var k in CONFIG_SCHEMA) {
    CONFIG[k] = CONFIG_SCHEMA[k].default;
  }
}
initConfig();

// Runtime zone config — copy of ZONE_DEFAULTS, overridden by KVS in loadZones()
var ZONES = [];

// KVS key specs for a single zone (key suffix applied after "zoneN-")
var ZONE_KEY_SPECS = [
  {field: "name",        key: "name",         type: "string"},
  {field: "appRateMmH",  key: "app-rate",      type: "number"},
  {field: "kc",          key: "kc",            type: "number"},
  {field: "triggerMm",   key: "trigger-mm",    type: "number"},
  {field: "maxMin",      key: "max-min",       type: "number"},
  {field: "fallbackMin", key: "fallback-min",  type: "number"},
  {field: "enabled",     key: "enabled",       type: "boolean"}
];

function initZones() {
  for (var i = 0; i < ZONE_DEFAULTS.length; i++) {
    var d = ZONE_DEFAULTS[i];
    ZONES.push({
      id:          d.id,
      name:        d.name,
      appRateMmH:  d.appRateMmH,
      kc:          d.kc,
      triggerMm:   d.triggerMm,
      maxMin:      d.maxMin,
      fallbackMin: d.fallbackMin,
      enabled:     d.enabled
    });
  }
  ZONE_DEFAULTS = null; // free — not needed after copy
}
initZones();

// Script.storage keys (synchronous, survive reboot)
var STORAGE_KEYS = {
  forecastUrl:   "forecast-url",
  wateringQueue: "watering-queue",
  planStartH:    "plan-start-h"
};

// === TASK QUEUE (single recurring timer for all sequential async ops) ===
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
  task();
}

function queueTask(task) {
  TASK_QUEUE.push(task);
  if (!TASK_TIMER) {
    TASK_TIMER = Timer.set(200, true, processTaskQueue);
  }
}

// === RUNTIME STATE ===
var STATE = {
  forecastUrl:           null,
  lastForecastFetchDate: null,
  forecastEt0Yesterday:  null,  // ET0 for yesterday (mm) — drives deficit update
  forecastRainYesterday: null,  // actual precipitation yesterday (mm)
  forecastRainToday:     null,  // forecast precipitation today (mm) — rain holdoff check
  forecastWinds:         [],    // today's hourly wind_speed_10m [h] = km/h
  forecastTemps:         [],    // today's hourly temperature_2m [h] = C
  sunriseHour:           null,  // fractional hour of today's sunrise (informational)
  activeOutput:          -1,    // -1 = all off, 0/1/2 = that switch on
  initializing:          true
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
  print(SCRIPT_PREFIX + s);
}

// === SCRIPT.STORAGE HELPERS ===
function storeStorageValue(key, value) {
  var v;
  if (typeof value === "undefined" || value === null) {
    v = "null";
  } else if (typeof value === "number" || typeof value === "boolean") {
    v = value.toString();
  } else if (typeof value === "object") {
    v = JSON.stringify(value);
  } else {
    v = String(value);
  }
  Script.storage.setItem(key, v);
}

function loadStorageValue(key) {
  var v = Script.storage.getItem(key);
  if (v === null || v === undefined) return null;
  if (v === "null" || v === "undefined") return null;
  if (v === "true") return true;
  if (v === "false") return false;
  try {
    var num = Number(v);
    if (!isNaN(num) && v !== "") return num;
  } catch (e) {
    if (e && false) {}
  }
  try {
    return JSON.parse(v);
  } catch (e) {
    if (e && false) {}
    return v;
  }
}

// === KVS HELPERS ===
function storeKVSValue(key, value) {
  var v;
  if (typeof value === "undefined" || value === null) {
    v = "null";
  } else if (typeof value === "number" || typeof value === "boolean") {
    v = value.toString();
  } else if (typeof value === "object") {
    v = JSON.stringify(value);
  } else {
    v = String(value);
  }
  // Fire-and-forget — never block on this
  Shelly.call("KVS.Set", {key: CONFIG_KEY_PREFIX + key, value: v});
}

// === GLOBAL CONFIG LOADING ===
function loadConfig(callback) {
  log("Loading global config from KVS...");
  var keys = [];
  for (var k in CONFIG_SCHEMA) {
    if (!CONFIG_SCHEMA[k].cliOnly) keys.push(k);
  }
  var idx = 0;

  function loadNext() {
    if (idx >= keys.length) {
      CONFIG_SCHEMA = null; // free schema — not needed at runtime
      log("Global config loaded");
      if (callback) callback();
      return;
    }
    var k = keys[idx];
    var schema = CONFIG_SCHEMA[k];
    var kvsKey = CONFIG_KEY_PREFIX + schema.key;
    idx++;
    Shelly.call("KVS.Get", {key: kvsKey}, function(result, err) {
      if (!err && result && ("value" in result) && result.value !== null && result.value !== "") {
        var val = result.value;
        if (schema.type === "boolean") {
          CONFIG[k] = val === "true" || val === true;
        } else if (schema.type === "number") {
          var num = Number(val);
          if (!isNaN(num)) CONFIG[k] = num;
        } else {
          CONFIG[k] = val;
        }
      }
      if (err && false) {}
      queueTask(loadNext);
    });
  }

  loadNext();
}

// === ZONE CONFIG LOADING ===
function loadZones(callback) {
  log("Loading zone config from KVS...");
  var zIdx = 0;
  var kIdx = 0;

  function loadNextZoneKey() {
    if (zIdx >= ZONES.length) {
      ZONE_KEY_SPECS = null; // free — not needed after loading
      log("Zone config loaded");
      if (callback) callback();
      return;
    }
    if (kIdx >= ZONE_KEY_SPECS.length) {
      zIdx++;
      kIdx = 0;
      queueTask(loadNextZoneKey);
      return;
    }
    var zone = ZONES[zIdx];
    var spec = ZONE_KEY_SPECS[kIdx];
    var kvsKey = CONFIG_KEY_PREFIX + "zone" + zone.id + "-" + spec.key;
    kIdx++;
    Shelly.call("KVS.Get", {key: kvsKey}, function(result, err) {
      if (!err && result && ("value" in result) && result.value !== null && result.value !== "") {
        var val = result.value;
        if (spec.type === "boolean") {
          zone[spec.field] = val === "true" || val === true;
        } else if (spec.type === "number") {
          var num = Number(val);
          if (!isNaN(num)) zone[spec.field] = num;
        } else {
          zone[spec.field] = val;
        }
      }
      if (err && false) {}
      queueTask(loadNextZoneKey);
    });
  }

  loadNextZoneKey();
}

// === FORECAST UTILITIES ===
function parseHourFromISO(isoStr) {
  var tIdx = isoStr.indexOf("T");
  if (tIdx < 0) return null;
  var timePart = isoStr.slice(tIdx + 1);
  var colonIdx = timePart.indexOf(":");
  if (colonIdx < 0) return Number(timePart);
  var h = Number(timePart.slice(0, colonIdx));
  var m = Number(timePart.slice(colonIdx + 1, colonIdx + 3));
  if (isNaN(h)) return null;
  if (isNaN(m)) m = 0;
  return h + m / 60;
}

function lpad2(n) {
  return n < 10 ? "0" + n : String(n);
}

function makeTimespec(fractHours) {
  if (fractHours < 0) fractHours = 0;
  if (fractHours >= 24) fractHours = 23.99;
  var h = Math.floor(fractHours);
  var m = Math.round((fractHours - h) * 60);
  if (m >= 60) { h++; m = 0; }
  h = h % 24;
  return "0 " + m + " " + h + " * * SUN,MON,TUE,WED,THU,FRI,SAT";
}

// === OPEN-METEO FORECAST ===
function setForecastURL(lat, lon) {
  if (lat === null || lon === null) return;
  var url = "https://api.open-meteo.com/v1/forecast?latitude=" + lat +
    "&longitude=" + lon +
    "&hourly=temperature_2m,wind_speed_10m" +
    "&daily=sunrise,precipitation_sum,et0_fao_evapotranspiration" +
    "&past_days=" + PAST_DAYS + "&forecast_days=2&timezone=auto";
  STATE.forecastUrl = url;
  storeStorageValue(STORAGE_KEYS.forecastUrl, url);
  log("Forecast URL set");
}

function shouldRefreshForecast() {
  var now = new Date();
  var today = now.getFullYear() + "-" + (now.getMonth() + 1) + "-" + now.getDate();
  return STATE.lastForecastFetchDate === null || STATE.lastForecastFetchDate !== today;
}

function onForecast(result, error_code, error_message, cb) {
  if (error_code !== 0) {
    log("Forecast error:", error_code, error_message);
    if (typeof cb === "function") queueTask(function() { cb(); });
    return;
  }
  if (!result || !result.body) {
    log("No forecast body");
    if (typeof cb === "function") queueTask(function() { cb(); });
    return;
  }
  var data = null;
  try {
    data = JSON.parse(result.body);
  } catch (e) {
    log("Forecast JSON parse error");
    if (e && false) {}
    if (typeof cb === "function") queueTask(function() { cb(); });
    return;
  }
  result = null;

  if (!data || !data.daily || !data.hourly) {
    log("Invalid forecast structure");
    data = null;
    if (typeof cb === "function") queueTask(function() { cb(); });
    return;
  }

  // PAST_DAYS=3: daily[0]=3d ago, daily[1]=2d ago, daily[2]=yesterday, daily[3]=today
  var yIdx = PAST_DAYS - 1; // yesterday
  var tDay = PAST_DAYS;     // today

  if (data.daily.et0_fao_evapotranspiration && data.daily.et0_fao_evapotranspiration.length > yIdx) {
    STATE.forecastEt0Yesterday = data.daily.et0_fao_evapotranspiration[yIdx];
  }
  if (data.daily.precipitation_sum && data.daily.precipitation_sum.length > tDay) {
    STATE.forecastRainYesterday = data.daily.precipitation_sum[yIdx];
    STATE.forecastRainToday     = data.daily.precipitation_sum[tDay];
  }
  if (data.daily.sunrise && data.daily.sunrise.length > tDay) {
    STATE.sunriseHour = parseHourFromISO(data.daily.sunrise[tDay]);
  }

  // Extract today's hourly wind and temp for hours 0..lunchStart
  // hourly[PAST_DAYS*24 + h] = today's hour h
  var todayBase = PAST_DAYS * 24;
  var maxH = Math.ceil(CONFIG.lunchStart) + 1;
  if (maxH > 24) maxH = 24;
  var winds = [];
  var temps = [];
  var windArr = data.hourly.wind_speed_10m;
  var tempArr = data.hourly.temperature_2m;
  for (var h = 0; h < maxH; h++) {
    var hi = todayBase + h;
    var w = (windArr && hi < windArr.length && windArr[hi] !== null) ? windArr[hi] : 99;
    var t = (tempArr && hi < tempArr.length && tempArr[hi] !== null) ? tempArr[hi] : 99;
    winds.push(w);
    temps.push(t);
  }
  STATE.forecastWinds = winds;
  STATE.forecastTemps = temps;

  // Free large arrays immediately
  winds = null; temps = null; windArr = null; tempArr = null; data = null;

  var now2 = new Date();
  STATE.lastForecastFetchDate = now2.getFullYear() + "-" + (now2.getMonth() + 1) + "-" + now2.getDate();
  log("Forecast cached. ET0_yest=" + STATE.forecastEt0Yesterday +
      " rain_yest=" + STATE.forecastRainYesterday +
      " rain_today=" + STATE.forecastRainToday);

  if (typeof cb === "function") queueTask(function() { cb(); });
}

function fetchAndCacheForecast(cb) {
  var url = STATE.forecastUrl || loadStorageValue(STORAGE_KEYS.forecastUrl);
  if (!url) {
    log("No forecast URL, skipping fetch");
    if (typeof cb === "function") queueTask(function() { cb(); });
    return;
  }
  STATE.forecastUrl = url;
  log("Fetching forecast...");
  Shelly.call("HTTP.GET", {url: url, timeout: 10}, onForecast, cb);
}

function onDeviceLocation(result, error_code, error_message, cb) {
  if (error_code === 0 && result && result.lat !== null && result.lon !== null) {
    log("Location: lat=" + result.lat + " lon=" + result.lon);
    setForecastURL(result.lat, result.lon);
  } else {
    log("Location detection failed:", error_code, error_message);
  }
  if (typeof cb === "function") queueTask(function() { cb(); });
}

function ensureForecastUrl(cb) {
  if (STATE.forecastUrl) {
    if (typeof cb === "function") queueTask(function() { cb(); });
    return;
  }
  var stored = loadStorageValue(STORAGE_KEYS.forecastUrl);
  if (stored && stored.indexOf("daily=") !== -1) {
    STATE.forecastUrl = stored;
    log("Forecast URL loaded from storage");
    if (typeof cb === "function") queueTask(function() { cb(); });
    return;
  }
  log("Detecting device location...");
  Shelly.call("Shelly.DetectLocation", {}, onDeviceLocation, cb);
}

// === QUIET WINDOW HELPERS ===
function isInQuietWindow(h) {
  if (h >= CONFIG.lunchStart && h < CONFIG.lunchEnd) return true;
  if (h >= CONFIG.eveningStart && h < CONFIG.eveningEnd) return true;
  return false;
}

function willCrossQuietWindow(startH, durationH) {
  var endH = startH + durationH;
  if (startH < CONFIG.lunchEnd && endH > CONFIG.lunchStart) return true;
  if (startH < CONFIG.eveningEnd && endH > CONFIG.eveningStart) return true;
  return false;
}

function getCurrentHour() {
  var d = new Date();
  return d.getHours() + d.getMinutes() / 60.0;
}

// === PER-ZONE DEFICIT (Script.storage, survives reboot) ===
function deficitKey(zoneId) {
  return "deficit/" + zoneId;
}

function loadDeficit(zoneId) {
  var v = loadStorageValue(deficitKey(zoneId));
  if (v === null) return 0;
  var n = Number(v);
  return isNaN(n) ? 0 : n;
}

function saveDeficit(zoneId, value) {
  if (value < 0) value = 0;
  if (value > CONFIG.maxDeficitMm) value = CONFIG.maxDeficitMm;
  storeStorageValue(deficitKey(zoneId), value);
  storeKVSValue("zone" + zoneId + "-deficit", value); // mirror for CLI status
}

// === ET0 DEFICIT UPDATE ===
function updateDeficits() {
  var et0 = STATE.forecastEt0Yesterday;
  var rain = STATE.forecastRainYesterday;
  if (et0 === null || rain === null) {
    log("No ET0/rain data, skipping deficit update");
    return;
  }
  log("Updating deficits: ET0=" + et0 + " rain=" + rain);
  for (var i = 0; i < ZONES.length; i++) {
    var z = ZONES[i];
    if (!z.enabled) continue;
    var deficit = loadDeficit(z.id);
    deficit += et0 * z.kc - rain;
    if (deficit < 0) deficit = 0;
    if (deficit > CONFIG.maxDeficitMm) deficit = CONFIG.maxDeficitMm;
    saveDeficit(z.id, deficit);
    log("Zone " + z.id + " (" + z.name + "): deficit=" + Math.round(deficit * 10) / 10 + " mm");
  }
}

// === ZONE PLAN COMPUTATION ===
// Returns [{id, minutes}] for zones whose deficit >= trigger
function computeZonePlan() {
  var plan = [];
  for (var i = 0; i < ZONES.length; i++) {
    var z = ZONES[i];
    if (!z.enabled) continue;
    var deficit = loadDeficit(z.id);
    if (deficit < z.triggerMm) {
      log("Zone " + z.id + ": deficit " + Math.round(deficit * 10) / 10 + " mm < trigger " + z.triggerMm + " mm, skip");
      continue;
    }
    var depthMm = deficit < CONFIG.maxDeficitMm ? deficit : CONFIG.maxDeficitMm;
    var minutes = depthMm / z.appRateMmH * 60;
    if (minutes > z.maxMin) minutes = z.maxMin;
    minutes = Math.round(minutes);
    if (minutes < 1) continue;
    plan.push({id: z.id, minutes: minutes});
    log("Zone " + z.id + ": water " + minutes + " min (deficit=" + Math.round(deficit * 10) / 10 + " mm)");
  }
  return plan;
}

// === CALM MORNING WINDOW SELECTION ===
// Score each candidate start hour by avg wind (dominant) + 0.1 * avg temp;
// lower is better. Returns best integer start hour.
function findCalmWindow(totalMinutes) {
  var totalH = totalMinutes / 60.0;
  var maxStart = Math.floor(CONFIG.lunchStart - totalH);
  var best = CONFIG.fallbackStartHour;
  var bestScore = -1;
  var found = false;

  for (var startH = CONFIG.earliestStartHour; startH <= maxStart; startH++) {
    if (isInQuietWindow(startH)) continue;
    if (willCrossQuietWindow(startH, totalH)) continue;

    var sumW = 0;
    var sumT = 0;
    var count = 0;
    var hEnd = Math.ceil(startH + totalH);
    for (var h = startH; h < hEnd && h < STATE.forecastWinds.length; h++) {
      sumW += STATE.forecastWinds[h];
      sumT += STATE.forecastTemps[h];
      count++;
    }
    if (count === 0) continue;
    var score = sumW / count + 0.1 * sumT / count;
    if (!found || score < bestScore) {
      bestScore = score;
      best = startH;
      found = true;
    }
  }

  log("Calm window: startH=" + best + (found ? " (scored)" : " (fallback)"));
  return best;
}

// === RUN COUNTERS ===
// Per-zone run count in Script.storage ("runs/<id>"); mirrored to KVS for CLI reads.
function loadRunCount(zoneId) {
  var v = loadStorageValue("runs/" + zoneId);
  return (typeof v === "number") ? v : 0;
}

function incrementRunCount(zoneId) {
  var n = loadRunCount(zoneId) + 1;
  storeStorageValue("runs/" + zoneId, n);
  Shelly.call("KVS.Set", {key: CONFIG_KEY_PREFIX + "zone" + zoneId + "-runs", value: String(n)},
    function(r, e) { if (e && false) {} });
}

// === WATERING QUEUE PERSISTENCE ===
function storeWateringQueue(queue) {
  storeStorageValue(STORAGE_KEYS.wateringQueue, queue);
}

function loadWateringQueue() {
  var v = loadStorageValue(STORAGE_KEYS.wateringQueue);
  if (!v || typeof v !== "object") return [];
  if (!v.length) return [];
  return v;
}

// === SCHEDULE MANAGEMENT ===
function updatePlanSchedule(startH) {
  var ts = makeTimespec(startH);
  log("Updating watering schedule to " + ts);
  Shelly.call("Schedule.List", {}, function(result, err) {
    if (err) {
      log("Schedule.List error:", err);
      if (err && false) {}
      return;
    }
    if (!result || !result.jobs) return;
    var jobId = -1;
    for (var i = 0; i < result.jobs.length; i++) {
      var job = result.jobs[i];
      if (job.calls && job.calls.length > 0) {
        var code = job.calls[0].params && job.calls[0].params.code;
        if (code === "handleWateringStart()") {
          jobId = job.id;
          break;
        }
      }
    }
    if (jobId < 0) {
      log("WARNING: handleWateringStart() schedule not found");
      return;
    }
    Shelly.call("Schedule.Update", {id: jobId, enable: true, timespec: ts}, function(r, e) {
      if (e && false) {}
      log("Watering schedule updated to " + ts);
    });
  });
}

function clearNonUpdateSchedules(callback) {
  log("Clearing non-firmware-update schedules...");
  Shelly.call("Schedule.List", {}, function(result, err) {
    if (err) {
      log("Schedule.List error:", err);
      if (err && false) {}
      if (callback) callback();
      return;
    }
    if (!result || !result.jobs) {
      if (callback) callback();
      return;
    }
    var toDelete = [];
    for (var i = 0; i < result.jobs.length; i++) {
      var job = result.jobs[i];
      var isUpdate = false;
      if (job.calls) {
        for (var j = 0; j < job.calls.length; j++) {
          if (job.calls[j].method === "Shelly.Update") {
            isUpdate = true;
            break;
          }
        }
      }
      if (!isUpdate) toDelete.push(job.id);
    }
    if (toDelete.length === 0) {
      if (callback) callback();
      return;
    }
    var dIdx = 0;
    function deleteNext() {
      if (dIdx >= toDelete.length) {
        if (callback) callback();
        return;
      }
      var jid = toDelete[dIdx];
      dIdx++;
      Shelly.call("Schedule.Delete", {id: jid}, function(r, e) {
        if (e && false) {}
        queueTask(deleteNext);
      });
    }
    deleteNext();
  });
}

function createSchedules(callback) {
  log("Creating garden schedules...");
  var scriptId = Shelly.getCurrentScriptId();
  var fallbackTs = makeTimespec(CONFIG.fallbackStartHour);
  var schedules = [
    {
      enable: true,
      timespec: "0 30 0 * * SUN,MON,TUE,WED,THU,FRI,SAT",
      calls: [{method: "script.eval", params: {id: scriptId, code: "handlePlan()"}}]
    },
    {
      enable: true,
      timespec: fallbackTs,
      calls: [{method: "script.eval", params: {id: scriptId, code: "handleWateringStart()"}}]
    }
  ];
  var sIdx = 0;
  function createNext() {
    if (sIdx >= schedules.length) {
      log("All garden schedules created");
      if (callback) callback();
      return;
    }
    var s = schedules[sIdx];
    sIdx++;
    Shelly.call("Schedule.Create", s, function(r, e) {
      if (e) log("WARNING: Schedule.Create error:", e);
      if (e && false) {}
      queueTask(createNext);
    });
  }
  createNext();
}

function verifySchedules(cb) {
  Shelly.call("Schedule.List", {}, function(result, err) {
    if (err) {
      log("WARNING: Cannot verify schedules:", err);
      if (err && false) {}
      if (typeof cb === "function") queueTask(function() { cb(); });
      return;
    }
    var hasPlan = false;
    var hasWater = false;
    if (result && result.jobs) {
      for (var i = 0; i < result.jobs.length; i++) {
        var job = result.jobs[i];
        if (job.calls && job.calls.length > 0) {
          var code = job.calls[0].params && job.calls[0].params.code;
          if (code === "handlePlan()") hasPlan = true;
          if (code === "handleWateringStart()") hasWater = true;
        }
      }
    }
    if (!hasPlan || !hasWater) {
      log("FATAL: Garden schedules missing. Run: ctl garden setup <device>");
      if (typeof cb === "function") queueTask(function() { cb(); });
      return;
    }
    log("Garden schedules verified");
    if (typeof cb === "function") queueTask(function() { cb(); });
  });
}

// === SOFTWARE FUSE (anti-cycling protection) ===
var FUSE_WINDOW_MS  = 120000; // 2 min sliding window
var FUSE_MAX_ON     = 5;      // max switch-on events per window
var FUSE_COOLDOWN_MS = 300000; // 5 min cooldown after trip
var FUSE_TIMES      = [];
var FUSE_TRIPPED    = false;
var FUSE_TRIP_TIME  = 0;

function fuseRecord() {
  FUSE_TIMES.push(Date.now());
}

function fuseAllowOn() {
  var now = Date.now();
  if (FUSE_TRIPPED) {
    if (now - FUSE_TRIP_TIME >= FUSE_COOLDOWN_MS) {
      log("FUSE: cooldown expired, reset");
      FUSE_TRIPPED = false;
      FUSE_TIMES = [];
      return true;
    }
    log("FUSE: tripped, refusing activation");
    return false;
  }
  var recent = [];
  for (var i = 0; i < FUSE_TIMES.length; i++) {
    if (now - FUSE_TIMES[i] < FUSE_WINDOW_MS) recent.push(FUSE_TIMES[i]);
  }
  FUSE_TIMES = recent;
  if (FUSE_TIMES.length >= FUSE_MAX_ON) {
    log("FUSE: TRIPPED — too many activations");
    FUSE_TRIPPED = true;
    FUSE_TRIP_TIME = now;
    turnOffAll();
    Shelly.emitEvent("garden.fuse_tripped", {count: FUSE_TIMES.length});
    return false;
  }
  return true;
}

// === OUTPUT CONTROL (one-at-a-time) ===
function turnOffAll() {
  for (var i = 0; i < NUM_ZONES; i++) {
    Shelly.call("Switch.Set", {id: i, on: false}, function(r, e) { if (e && false) {} });
  }
}

function turnOnZone(zoneId, callback) {
  if (!fuseAllowOn()) {
    log("Fuse blocked zone", zoneId);
    if (callback) queueTask(callback);
    return;
  }
  fuseRecord();
  // Turn off all other switches synchronously, then turn on target
  for (var i = 0; i < NUM_ZONES; i++) {
    if (i !== zoneId) {
      Shelly.call("Switch.Set", {id: i, on: false}, function(r, e) { if (e && false) {} });
    }
  }
  queueTask(function() {
    Shelly.call("Switch.Set", {id: zoneId, on: true}, function(r, e) {
      if (e && false) {}
      STATE.activeOutput = zoneId;
      if (callback) callback();
    });
  });
}

// === WATERING STATE MACHINE ===
var WATERING_QUEUE      = [];
var WATERING_ZONE_INDEX = 0;
var WATERING_START_MS   = 0;
var WATERING_TICK_TIMER = null;
var WATERING_TICK_MS    = 20000; // 20 second tick

function stopWatering(reason) {
  if (WATERING_TICK_TIMER !== null) {
    Timer.clear(WATERING_TICK_TIMER);
    WATERING_TICK_TIMER = null;
  }
  turnOffAll();
  STATE.activeOutput = -1;
  storeWateringQueue([]);
  Shelly.emitEvent("garden.cycle_done", {
    reason: reason || "complete",
    zones_completed: WATERING_ZONE_INDEX
  });
  log("Watering cycle done:", reason || "complete");
  WATERING_QUEUE      = [];
  WATERING_ZONE_INDEX = 0;
  WATERING_START_MS   = 0;
}

function tickWatering() {
  if (WATERING_ZONE_INDEX >= WATERING_QUEUE.length) {
    stopWatering("complete");
    return;
  }
  var entry = WATERING_QUEUE[WATERING_ZONE_INDEX];
  var zone = null;
  for (var i = 0; i < ZONES.length; i++) {
    if (ZONES[i].id === entry.id) { zone = ZONES[i]; break; }
  }
  if (!zone) {
    log("Unknown zone id:", entry.id, "- skipping");
    WATERING_ZONE_INDEX++;
    WATERING_START_MS = 0;
    return;
  }

  if (WATERING_START_MS === 0) {
    // Starting a new zone
    var curH = getCurrentHour();
    if (isInQuietWindow(curH)) {
      log("Quiet window at " + Math.floor(curH) + ":" + lpad2(Math.round((curH % 1) * 60)) + " — aborting");
      Shelly.emitEvent("garden.aborted_quiet", {hour: curH, zone: entry.id});
      stopWatering("quiet");
      return;
    }
    WATERING_START_MS = Date.now();
    log("Zone " + entry.id + " start (" + entry.minutes + " min)");
    Shelly.emitEvent("garden.zone_start", {zone: entry.id, name: zone.name, minutes: entry.minutes});
    turnOnZone(entry.id, null);
    return;
  }

  // Zone is running — check elapsed time
  var elapsedMs = Date.now() - WATERING_START_MS;
  var targetMs  = entry.minutes * 60000;
  if (elapsedMs >= targetMs) {
    // Zone complete — apply deficit credit, advance to next zone
    var appliedMm = zone.appRateMmH / 60.0 * (elapsedMs / 60000.0);
    var deficit = loadDeficit(zone.id);
    deficit -= appliedMm;
    if (deficit < 0) deficit = 0;
    saveDeficit(zone.id, deficit);
    Shelly.emitEvent("garden.zone_stop", {
      zone:        entry.id,
      name:        zone.name,
      elapsed_min: Math.round(elapsedMs / 60000 * 10) / 10,
      applied_mm:  Math.round(appliedMm * 10) / 10,
      deficit_mm:  Math.round(deficit * 10) / 10
    });
    incrementRunCount(entry.id);
    log("Zone " + entry.id + " done. Applied " + Math.round(appliedMm * 10) / 10 +
        " mm. Deficit now " + Math.round(deficit * 10) / 10 + " mm");
    Shelly.call("Switch.Set", {id: entry.id, on: false}, function(r, e) { if (e && false) {} });
    WATERING_ZONE_INDEX++;
    WATERING_START_MS = 0;
  }
}

function handleWateringStart() {
  if (WATERING_TICK_TIMER !== null) {
    log("Watering already in progress");
    return;
  }
  var curH = getCurrentHour();
  if (isInQuietWindow(curH)) {
    log("Quiet window active at " + curH + ", not starting");
    Shelly.emitEvent("garden.aborted_quiet", {hour: curH, zone: -1});
    return;
  }
  WATERING_QUEUE = loadWateringQueue();
  if (WATERING_QUEUE.length === 0) {
    log("No zones planned — nothing to water today");
    return;
  }
  WATERING_ZONE_INDEX = 0;
  WATERING_START_MS   = 0;
  log("Starting watering cycle: " + WATERING_QUEUE.length + " zones");
  Shelly.emitEvent("garden.cycle_start", {zones: WATERING_QUEUE.length});
  WATERING_TICK_TIMER = Timer.set(WATERING_TICK_MS, true, tickWatering);
}

// === CALIBRATION ===
// Run one zone for a measured duration; operator measures applied depth with
// catch-cups and sets zoneN-app-rate KVS key to real mm/h value.
function handleCalibrate(zoneId, minutes) {
  if (zoneId < 0 || zoneId >= NUM_ZONES) {
    log("Invalid zone for calibrate:", zoneId);
    return;
  }
  if (WATERING_TICK_TIMER !== null) {
    log("Stopping active watering cycle for calibration");
    stopWatering("calibrate");
  }
  var curH = getCurrentHour();
  if (isInQuietWindow(curH)) {
    log("Quiet window — refusing calibrate for zone " + zoneId);
    Shelly.emitEvent("garden.calibrate_refused", {zone: zoneId, reason: "quiet_window"});
    return;
  }
  log("Calibrate: zone " + zoneId + " for " + minutes + " min");
  turnOffAll();
  queueTask(function() {
    var secs = minutes * 60;
    // toggle_after turns the valve off automatically — no timer slot needed for that
    Shelly.call("Switch.Set", {id: zoneId, on: true, toggle_after: secs}, function(r, e) {
      if (e && false) {}
    });
    Shelly.emitEvent("garden.calibrate_start", {zone: zoneId, minutes: minutes});
    // Separate one-shot timer just to emit calibrate_stop event
    Timer.set(secs * 1000, false, function() {
      Shelly.emitEvent("garden.calibrate_stop", {zone: zoneId, minutes: minutes});
      log("Calibrate done: zone " + zoneId);
    });
  });
}

// === DAILY PLANNER ===
function runFallbackPlanner() {
  log("Using fallback plan (no forecast data)");
  var queue = [];
  for (var i = 0; i < ZONES.length; i++) {
    if (ZONES[i].enabled) {
      queue.push({id: ZONES[i].id, minutes: ZONES[i].fallbackMin});
    }
  }
  storeWateringQueue(queue);
  storeKVSValue("last-plan-start", CONFIG.fallbackStartHour);
  storeKVSValue("last-plan-zones", queue);
  updatePlanSchedule(CONFIG.fallbackStartHour);
  Shelly.emitEvent("garden.plan_fallback", {start_h: CONFIG.fallbackStartHour, zones: queue.length});
  log("Fallback: start " + CONFIG.fallbackStartHour + ":00, " + queue.length + " zones");
}

function runPlanner() {
  log("Running planner...");

  if (STATE.forecastEt0Yesterday === null) {
    runFallbackPlanner();
    return;
  }

  // Step 1: accumulate yesterday's ET0 and rain into per-zone deficits
  updateDeficits();

  // Step 2: which zones need water today?
  var plan = computeZonePlan();
  if (plan.length === 0) {
    log("No zones need water today");
    storeWateringQueue([]);
    storeKVSValue("last-plan-zones", []);
    Shelly.emitEvent("garden.skip_none_due", {});
    return;
  }

  // Step 3: rain holdoff
  var rainToday = STATE.forecastRainToday;
  if (rainToday !== null && rainToday >= CONFIG.rainHoldoffMm) {
    log("Rain holdoff: forecast " + rainToday + " mm >= " + CONFIG.rainHoldoffMm + " mm threshold");
    storeWateringQueue([]);
    storeKVSValue("last-plan-zones", []);
    Shelly.emitEvent("garden.skip_rain", {rain_mm: rainToday, threshold_mm: CONFIG.rainHoldoffMm});
    return;
  }

  // Step 4: total span + calm window selection
  var totalMinutes = 0;
  for (var i = 0; i < plan.length; i++) totalMinutes += plan[i].minutes;
  totalMinutes += (plan.length - 1) * 2; // 2-min inter-zone gaps

  var startH = findCalmWindow(totalMinutes);
  var endH   = startH + totalMinutes / 60.0;

  // Step 5: frost guard — check min temp in the planned window
  var minTemp = 99;
  for (var h = Math.floor(startH); h <= Math.ceil(endH) && h < STATE.forecastTemps.length; h++) {
    if (STATE.forecastTemps[h] < minTemp) minTemp = STATE.forecastTemps[h];
  }
  if (minTemp < CONFIG.frostCutoffC) {
    log("Frost guard: " + minTemp + " C < " + CONFIG.frostCutoffC + " C threshold");
    storeWateringQueue([]);
    storeKVSValue("last-plan-zones", []);
    Shelly.emitEvent("garden.skip_frost", {min_temp_c: minTemp, threshold_c: CONFIG.frostCutoffC});
    return;
  }

  // Step 6: commit plan
  storeWateringQueue(plan);
  storeKVSValue("last-plan-start", startH);
  storeKVSValue("last-plan-zones", plan);
  updatePlanSchedule(startH);
  Shelly.emitEvent("garden.plan", {
    start_h:      startH,
    zones:        plan.length,
    total_min:    totalMinutes,
    rain_today_mm: rainToday,
    min_temp_c:   minTemp
  });
  log("Plan: start " + startH + ":00, " + plan.length + " zone(s), " + totalMinutes + " min total");
}

function onPlanForecastReady() {
  if (shouldRefreshForecast()) {
    fetchAndCacheForecast(runPlanner);
  } else {
    log("Using cached forecast for plan");
    queueTask(runPlanner);
  }
}

function handlePlan() {
  log("handlePlan triggered");
  ensureForecastUrl(onPlanForecastReady);
}

// === BUTTON (manual zone cycling) ===
function cycleOutputs() {
  log("Button: cycling zones");
  var curH = getCurrentHour();
  if (isInQuietWindow(curH)) {
    log("Button: quiet window, refusing");
    Shelly.emitEvent("garden.button_blocked", {reason: "quiet_window", hour: curH});
    return;
  }
  if (WATERING_TICK_TIMER !== null) {
    log("Button: stopping active automated cycle");
    stopWatering("manual_button");
    return;
  }
  // Cycle: all off → 0 → 1 → 2 → all off
  var next = (STATE.activeOutput === -1) ? 0 :
             (STATE.activeOutput === 0) ? 1 :
             (STATE.activeOutput === 1) ? 2 : -1;
  if (next === -1) {
    turnOffAll();
    STATE.activeOutput = -1;
    Shelly.emitEvent("garden.manual", {action: "off"});
  } else {
    turnOnZone(next, null);
    Shelly.emitEvent("garden.manual", {action: "on", zone: next});
  }
}

// === SWITCH EVENT HANDLER (one-at-a-time enforcement) ===
function handleSwitchEvent(info) {
  if (STATE.initializing) return;
  if (info.state === true) {
    var activated = info.id;
    // Force off all other switches
    for (var i = 0; i < NUM_ZONES; i++) {
      if (i !== activated) {
        Shelly.call("Switch.Set", {id: i, on: false}, function(r, e) { if (e && false) {} });
      }
    }
    STATE.activeOutput = activated;
  } else {
    // Check if any switch is still on
    var anyOn = false;
    for (var j = 0; j < NUM_ZONES; j++) {
      var st = Shelly.getComponentStatus("switch:" + j);
      if (st && st.output) { anyOn = true; break; }
    }
    if (!anyOn) STATE.activeOutput = -1;
  }
}

// === INITIALIZATION ===
function enforceOutputState() {
  var onSwitches = [];
  for (var i = 0; i < NUM_ZONES; i++) {
    var st = Shelly.getComponentStatus("switch:" + i);
    if (st && st.output) onSwitches.push(i);
  }
  if (onSwitches.length > 0) {
    // Sprinkler(s) were on when the device rebooted — mid-cycle reboot.
    // We cannot know elapsed time, so turn everything off and start clean.
    // The planner will re-schedule a fresh cycle at its next run (00:30).
    log("Reboot recovery: " + onSwitches.length + " switch(es) were on — turning off and clearing queue");
    for (var i = 0; i < onSwitches.length; i++) {
      Shelly.call("Switch.Set", {id: onSwitches[i], on: false}, function(r, e) { if (e && false) {} });
    }
    storeWateringQueue([]); // discard interrupted cycle; planner rebuilds tonight
    Shelly.emitEvent("garden.reboot_recovery", {zones_on: onSwitches});
  }
  STATE.activeOutput = -1;
  log("Active output at startup:", STATE.activeOutput);
}

function continueInit() {
  enforceOutputState();
  STATE.initializing = false;
  log("Script initialization complete");

  // Disable sys_btn_toggle so button events reach our handler
  Shelly.call("Sys.SetConfig", {config: {device: {sys_btn_toggle: false}}}, function(res, err) {
    if (err && false) {}
  });

  // Verify schedules exist, then run an initial plan
  queueTask(function() {
    verifySchedules(function() {
      queueTask(handlePlan);
    });
  });
}

function init() {
  log("Garden sprinkler script starting...");
  loadConfig(function() {
    loadZones(function() {
      continueInit();
    });
  });
}

// === EVENT SUBSCRIPTION ===
Shelly.addEventHandler(function(event) {
  if (!event || !event.info) return;
  var info = event.info;
  if (info.event === "script_stop") {
    log("Script stopping");
    return;
  }
  if (typeof info.component === "string") {
    if (info.component.indexOf("switch:") === 0 && typeof info.state === "boolean") {
      handleSwitchEvent(info);
    } else if (info.component === "sys" && info.event === "sys_btn_push") {
      cycleOutputs();
    }
  }
});

// Start
init();
