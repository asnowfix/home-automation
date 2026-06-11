// heater-myhome.js — daemon-side heater workflow (JS port of the Go
// HeaterService in internal/myhome/shelly/script/heater.go).
//
// Listed in daemon.scripts.run it takes over the heater.getconfig and
// heater.setconfig RPC verbs (MyHome.registerVerb replaces the Go handlers),
// and serves a get_forecast handler so device heater scripts can fetch the
// weather forecast through the daemon instead of calling the internet
// themselves (distributed device<->daemon execution via script.invoke).
//
// Handlers complete asynchronously: they receive (params, respond) and call
// respond(result) / respond(null, "error") after the device RPC chain ends.
//
// KVS key schema on the heater device (same as the Go service and heater.js):
//   script/heater/enable-logging, script/heater/cheap-start-hour,
//   script/heater/cheap-end-hour, script/heater/poll-interval-ms,
//   script/heater/preheat-hours, script/heater/internal-temperature-topic,
//   script/heater/external-temperature-topic, room-id, normally-closed

var KVS_KEYS = {
  enable_logging: "script/heater/enable-logging",
  cheap_start_hour: "script/heater/cheap-start-hour",
  cheap_end_hour: "script/heater/cheap-end-hour",
  poll_interval_ms: "script/heater/poll-interval-ms",
  preheat_hours: "script/heater/preheat-hours",
  internal_temperature_topic: "script/heater/internal-temperature-topic",
  external_temperature_topic: "script/heater/external-temperature-topic",
  room_id: "room-id",
  normally_closed: "normally-closed"
};

// ------------------------------------------------------------- helpers

function kvsItemValue(items, key) {
  if (!items || !(key in items)) return null;
  var v = items[key];
  if (v !== null && typeof v === "object" && ("value" in v)) return v.value;
  return v;
}

function toInt(v) {
  var n = Number(v);
  if (isNaN(n)) return null;
  return Math.floor(n);
}

// ------------------------------------------------------------- heater.getconfig

// Per-request context objects (req) travel through the callback chain via
// Function.prototype.bind — no shared mutable state between requests.

function handleGetConfig(params, respond) {
  if (!params || !params.identifier) {
    respond(null, "missing identifier");
    return;
  }
  var req = {
    identifier: params.identifier,
    respond: respond,
    result: { device_id: params.identifier, device_name: "", has_script: false }
  };
  MyHome.call("device.lookup", params.identifier, onGetConfigLookup.bind(null, req));
}

function onGetConfigLookup(req, result, error_code, error_message) {
  if (error_code === 0 && result && result.length > 0) {
    if (result[0].id) req.result.device_id = result[0].id;
    if (result[0].name) req.result.device_name = result[0].name;
  }
  MyHome.deviceCall(req.identifier, "Script.List", null, onGetConfigScriptList.bind(null, req));
}

function onGetConfigScriptList(req, result, error_code, error_message) {
  if (error_code !== 0) {
    req.respond(null, "Script.List failed: " + error_message);
    return;
  }
  var scripts = (result && result.scripts) ? result.scripts : [];
  for (var i = 0; i < scripts.length; i++) {
    if (scripts[i].name === "heater.js" || scripts[i].name === "heater") {
      req.result.has_script = true;
      break;
    }
  }
  if (!req.result.has_script) {
    req.respond(req.result);
    return;
  }
  MyHome.deviceCall(req.identifier, "KVS.GetMany", { match: "script/heater/*" }, onGetConfigKvsMany.bind(null, req));
}

function onGetConfigKvsMany(req, result, error_code, error_message) {
  var items = (error_code === 0 && result) ? result.items : null;
  var config = {
    enable_logging: kvsItemValue(items, KVS_KEYS.enable_logging) === "true",
    room_id: "",
    cheap_start_hour: 0,
    cheap_end_hour: 0,
    poll_interval_ms: 0,
    preheat_hours: 0,
    normally_closed: false,
    internal_temperature_topic: "",
    external_temperature_topic: ""
  };
  var v;
  v = toInt(kvsItemValue(items, KVS_KEYS.cheap_start_hour));
  if (v !== null) config.cheap_start_hour = v;
  v = toInt(kvsItemValue(items, KVS_KEYS.cheap_end_hour));
  if (v !== null) config.cheap_end_hour = v;
  v = toInt(kvsItemValue(items, KVS_KEYS.poll_interval_ms));
  if (v !== null) config.poll_interval_ms = v;
  v = toInt(kvsItemValue(items, KVS_KEYS.preheat_hours));
  if (v !== null) config.preheat_hours = v;
  v = kvsItemValue(items, KVS_KEYS.internal_temperature_topic);
  if (v) config.internal_temperature_topic = String(v);
  v = kvsItemValue(items, KVS_KEYS.external_temperature_topic);
  if (v) config.external_temperature_topic = String(v);

  req.result.config = config;
  MyHome.deviceCall(req.identifier, "KVS.Get", { key: KVS_KEYS.room_id }, onGetConfigRoomId.bind(null, req));
}

function onGetConfigRoomId(req, result, error_code, error_message) {
  if (error_code === 0 && result && ("value" in result) && result.value !== null) {
    req.result.config.room_id = String(result.value);
  }
  MyHome.deviceCall(req.identifier, "KVS.Get", { key: KVS_KEYS.normally_closed }, onGetConfigNormallyClosed.bind(null, req));
}

function onGetConfigNormallyClosed(req, result, error_code, error_message) {
  if (error_code === 0 && result && ("value" in result)) {
    req.result.config.normally_closed = String(result.value) === "true";
  }
  req.respond(req.result);
}

// ------------------------------------------------------------- heater.setconfig

function handleSetConfig(params, respond) {
  if (!params || !params.identifier) {
    respond(null, "missing identifier");
    return;
  }
  var tasks = [];
  var fields = ["enable_logging", "room_id", "cheap_start_hour", "cheap_end_hour",
    "poll_interval_ms", "preheat_hours", "normally_closed",
    "internal_temperature_topic", "external_temperature_topic"];
  for (var i = 0; i < fields.length; i++) {
    var f = fields[i];
    if ((f in params) && params[f] !== null && typeof params[f] !== "undefined") {
      tasks.push({ field: f, key: KVS_KEYS[f], value: String(params[f]) });
    }
  }
  var req = { identifier: params.identifier, tasks: tasks, respond: respond };
  setConfigNext(req);
}

// setConfigNext applies KVS sets one at a time (sequential task queue: avoids
// flooding the device with parallel RPCs), then uploads heater.js.
function setConfigNext(req) {
  if (req.tasks.length === 0) {
    MyHome.uploadScript(req.identifier, "heater.js", onSetConfigUploaded.bind(null, req));
    return;
  }
  var task = req.tasks[0];
  req.tasks = req.tasks.slice(1);
  MyHome.deviceCall(req.identifier, "KVS.Set", { key: task.key, value: task.value },
    onSetConfigKvsSet.bind(null, req, task));
}

function onSetConfigKvsSet(req, task, result, error_code, error_message) {
  if (error_code !== 0) {
    req.respond({ success: false, message: "failed to set " + task.field + ": " + error_message });
    return;
  }
  setConfigNext(req);
}

function onSetConfigUploaded(req, result, error_code, error_message) {
  if (error_code !== 0) {
    req.respond({ success: false, message: "failed to upload/start heater.js: " + error_message });
    return;
  }
  MyHome.log("Heater script uploaded and started on " + req.identifier);
  req.respond({ success: true });
}

// ------------------------------------------------------------- get_forecast

// Device heater scripts invoke this through script.invoke instead of calling
// open-meteo themselves: the daemon fetches and caches the forecast.

var FORECAST = {
  cacheMs: 60 * 60 * 1000,
  cache: {} // "lat,lon" -> { at: ms, data: {...} }
};

function forecastUrl(lat, lon) {
  return "https://api.open-meteo.com/v1/forecast?latitude=" + lat + "&longitude=" + lon +
    "&hourly=temperature_2m&forecast_days=1&timezone=auto";
}

function handleGetForecast(params, respond) {
  if (!params || typeof params.lat === "undefined" || typeof params.lon === "undefined") {
    respond(null, "missing lat/lon");
    return;
  }
  var key = String(params.lat) + "," + String(params.lon);
  var cached = (key in FORECAST.cache) ? FORECAST.cache[key] : null;
  if (cached && (Date.now() - cached.at) < FORECAST.cacheMs) {
    respond({ cached: true, fetched_at: cached.at, forecast: cached.data });
    return;
  }
  var req = { key: key, respond: respond };
  Shelly.call("HTTP.GET", { url: forecastUrl(params.lat, params.lon), timeout: 15 },
    onForecastFetched.bind(null, req));
}

function onForecastFetched(req, result, error_code, error_message) {
  if (error_code !== 0 || !result || !result.body) {
    req.respond(null, "forecast fetch failed: " + error_message);
    return;
  }
  var data = null;
  try {
    data = JSON.parse(result.body);
  } catch (e) {
    req.respond(null, "forecast parse failed: " + String(e));
    return;
  }
  FORECAST.cache[req.key] = { at: Date.now(), data: data };
  req.respond({ cached: false, fetched_at: Date.now(), forecast: data });
}

// ------------------------------------------------------------- main

function main() {
  if (typeof MyHome === "undefined") {
    print("heater-myhome.js is a daemon workflow; it does nothing on a device");
    return;
  }
  MyHome.registerVerb("heater.getconfig", handleGetConfig);
  MyHome.registerVerb("heater.setconfig", handleSetConfig);
  MyHome.on("get_forecast", handleGetForecast);
  MyHome.log("heater workflow ready on instance " + MyHome.instance());
}

main();
