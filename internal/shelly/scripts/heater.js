// Kalman filter heater controller for Shelly Plus 1 (ES5 style, no Node.js, for Shelly scripting engine)
// Uses Open-Meteo API for weather forecasts (no API key required)

// === STATIC CONSTANTS ===
const SCRIPT_NAME = "heater";
const CONFIG_KEY_PREFIX = 'script/' + SCRIPT_NAME + '/';
const SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";
const DEFAULT_COOLING_RATE = 1.0;

// Configuration schema with type information
var CONFIG_SCHEMA = {
  enableLogging: {
    description: "Enable logging when true",
    key: "enable-logging",
    default: true,
    type: "boolean"
  },
  setpoint: {
    description: "Target temperature",
    key: "set-point",
    default: 19.0,
    type: "number"
  },
  minInternalTemp: {
    description: "Minimum internal temperature threshold",
    key: "min-internal-temp",
    default: 15.0,
    type: "number"
  },
  cheapStartHour: {
    description: "Start hour of cheap electricity window",
    key: "cheap-start-hour",
    default: 23,
    type: "number"
  },
  cheapEndHour: {
    description: "End hour of cheap electricity window",
    key: "cheap-end-hour",
    default: 7,
    type: "number"
  },
  pollIntervalMs: {
    description: "Polling interval in milliseconds",
    key: "poll-interval-ms",
    default: 5 * 60 * 1000,
    type: "number"
  },
  preheatHours: {
    description: "Hours before cheap window end to start preheating",
    key: "preheat-hours",
    default: 2,
    type: "number"
  },
  normallyClosed: {
    description: "Whether the switch is normally closed",
    key: "normally-closed",
    default: true,
    type: "boolean",
    unprefixed: true
  },
  internalTemperatureTopic: {
    description: "MQTT topic for internal temperature sensor",
    key: "internal-temperature-topic",
    default: null,
    type: "string"
  },
  externalTemperatureTopic: {
    description: "MQTT topic for external temperature sensor",
    key: "external-temperature-topic",
    default: null,
    type: "string"
  }
};

// Runtime configuration values (initialized from defaults)
var CONFIG = {};

// Initialize CONFIG with default values
function initConfig() {
  for (var key in CONFIG_SCHEMA) {
    CONFIG[key] = CONFIG_SCHEMA[key].default;
  }
}

initConfig();

// State Script.storage keys for continuously evolving values, automatically saved per script
var STORAGE_KEYS = {
  coolingRate: "cooling-rate",
  forecastUrl: "forecast-url",
  lastCheapEnd: "last-cheap-end"
};

function getCoolingRate() {
  var v = loadValue(STORAGE_KEYS.coolingRate);
  return (typeof v === "number") ? v : DEFAULT_COOLING_RATE;
}

function setCoolingRate(v) {
  if (typeof v === "number") {
    storeValue(STORAGE_KEYS.coolingRate, v);
  }
}

/**
 * Script.storage key: "cooling-rate"
 * Stores the continuously learned cooling coefficient as a number.
 */

/**
 * Script.storage key: "forecast-url"
 * Stores the Open-Meteo forecast URL string built from detected device location.
 */

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
      // Ensure 'e' is referenced so the minifier doesn't drop it and produce `catch {}`
      if (e && false) {}
    }
    if (i + 1 < arguments.length) s += " ";
  }
  print(SCRIPT_PREFIX, s);
}

// === POLYFILLS FOR MISSING ARRAY METHODS ===
// Shelly's JavaScript engine (modified Espruino) doesn't support concat()
if (!Array.prototype.concat) {
  Array.prototype.concat = function() {
    var result = [];
    var i, j, k, arg;
    
    // Add elements from this array
    for (i = 0; i < this.length; i++) {
      result[result.length] = this[i];
    }
    
    // Add elements from each argument
    for (j = 0; j < arguments.length; j++) {
      arg = arguments[j];
      if (Array.isArray(arg)) {
        for (k = 0; k < arg.length; k++) {
          result[result.length] = arg[k];
        }
      } else {
        result[result.length] = arg;
      }
    }
    
    return result;
  };
}

// === TIMER LIST SYSTEM ===
// Custom timer management to stay within Shelly's 5-timer limit
// Uses a single 5-second polling timer to manage multiple virtual timers
// stored in a sorted array by fire date.
//
// API:
//   addTimer(delayMs, recurring, callback) -> id
//   removeTimer(id) -> boolean
//
// This allows the script to have many logical timers while only using
// 1 actual Shelly Timer.set() for the poller.
var TIMER_LIST = [];
var TIMER_LIST_POLLER = null;
var TIMER_LIST_POLL_INTERVAL = 5000; // 5 seconds

// Generate random ID for timer
function generateTimerId() {
  var chars = 'abcdefghijklmnopqrstuvwxyz0123456789';
  var id = '';
  for (var i = 0; i < 8; i++) {
    id += chars[Math.floor(Math.random() * chars.length)];
  }
  return id;
}

// Add timer to sorted list
function addTimer(delayMs, recurring, callback) {
  var id = generateTimerId();
  var fireDate = new Date(Date.now() + delayMs);
  var period = recurring ? delayMs : 0;
  
  var timer = {
    date: fireDate,
    id: id,
    recurring: recurring,
    period: period,
    cb: callback
  };
  
  // Insert in sorted position
  var inserted = false;
  for (var i = 0; i < TIMER_LIST.length; i++) {
    if (timer.date.getTime() < TIMER_LIST[i].date.getTime()) {
      // Insert before this element
      var before = TIMER_LIST.slice(0, i);
      var after = TIMER_LIST.slice(i);
      TIMER_LIST = before.concat([timer]).concat(after);
      inserted = true;
      break;
    }
  }
  
  if (!inserted) {
    TIMER_LIST.push(timer);
  }
  
  // Start poller if not running
  if (!TIMER_LIST_POLLER && TIMER_LIST.length > 0) {
    startTimerPoller();
  }
  
  return id;
}

// Remove timer by ID
function removeTimer(id) {
  var newList = [];
  var found = false;
  for (var i = 0; i < TIMER_LIST.length; i++) {
    if (TIMER_LIST[i].id === id) {
      found = true;
    } else {
      newList.push(TIMER_LIST[i]);
    }
  }
  TIMER_LIST = newList;
  
  // Stop poller if list is empty
  if (TIMER_LIST.length === 0 && TIMER_LIST_POLLER) {
    Timer.clear(TIMER_LIST_POLLER);
    TIMER_LIST_POLLER = null;
  }
  
  return found;
}

// Process timers (called every 5 seconds)
function processTimers() {
  var now = Date.now();
  var processed = 0;
  
  // Process all timers at head of list that are due
  while (TIMER_LIST.length > 0 && TIMER_LIST[0].date.getTime() <= now) {
    var timer = TIMER_LIST[0];
    TIMER_LIST = TIMER_LIST.slice(1); // Remove from head
    
    // Execute callback
    try {
      timer.cb();
    } catch (e) {
      log('Timer error:', e);
      if (e && false) {}
    }
    
    // Re-add if recurring
    if (timer.recurring && timer.period > 0) {
      timer.date = new Date(now + timer.period);
      
      // Re-insert in sorted position
      var inserted = false;
      for (var i = 0; i < TIMER_LIST.length; i++) {
        if (timer.date.getTime() < TIMER_LIST[i].date.getTime()) {
          var before = TIMER_LIST.slice(0, i);
          var after = TIMER_LIST.slice(i);
          TIMER_LIST = before.concat([timer]).concat(after);
          inserted = true;
          break;
        }
      }
      if (!inserted) {
        TIMER_LIST.push(timer);
      }
    }
    
    processed++;
  }
  
  // Stop poller if no more timers
  if (TIMER_LIST.length === 0 && TIMER_LIST_POLLER) {
    Timer.clear(TIMER_LIST_POLLER);
    TIMER_LIST_POLLER = null;
  }
}

// Start the timer poller
function startTimerPoller() {
  if (!TIMER_LIST_POLLER) {
    TIMER_LIST_POLLER = Timer.set(TIMER_LIST_POLL_INTERVAL, true, processTimers);
  }
}

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // Occupancy URL (built from MQTT server IP + /status)
  occupancyUrl: null,
  
  // Forecast cache
  forecastUrl: null,
  cachedForecast: null,
  cachedForecastTimes: null,
  lastForecastFetchDate: null,
  
  // Heater state
  lastHeaterState: false,
  normallyClosed: true,
  
  // Readiness tracking
  temperatureReady: {
    internal: false,
    external: false
  },
  
  // Temperature values (in-memory cache from MQTT)
  temperature: {
    internal: null,
    external: null
  },
  
  // Track subscribed MQTT topics for cleanup
  subscribedTemperatureTopic: {
    internal: null,
    external: null
  },
  
  // Timer IDs for cleanup
  learningTimerIds: [],
  controlLoopTimerId: null,
  mqttRepeatTimerId: null,
  checkTimerId: null
};

function onDeviceLocation(result, error_code, error_message, cb) {
  log('onDeviceLocation')
  if (error_code === 0 && result) {
    if (result.lat !== null && result.lon !== null) {
      log('Auto-detected location: lat=' + result.lat + ', lon=' + result.lon + ', tz=' + result.tz);
      setForecastURL(result.lat, result.lon);
      if (typeof cb === 'function') cb();
    } else {
      log('Location detection returned null coordinates');
    }
  } else {
    log('Failed to detect location (error ' + error_code + '): ' + error_message);
  }
}

function onForecastUrlReady(cb) {
  log('onForecastUrlReady')
  fetchAndCacheForecast(loadConfig.bind(null, cb));
}

function fetchForecast(cb) {
  log('fetchForecast')

  if (!STATE.forecastUrl) {
    STATE.forecastUrl = loadValue(STORAGE_KEYS.forecastUrl);
  }

  if (!STATE.forecastUrl) {
    log('Forecast URL not loaded/cached, detecting location...');
    Shelly.call('Shelly.DetectLocation', {}, onDeviceLocation, onForecastUrlReady.bind(null, cb));
  } else {
    onForecastUrlReady(cb);
  }
}

function onDeviceLocation(result, error_code, error_message, cb) {
  log('onDeviceLocation')
  if (error_code === 0 && result) {
    if (result.lat !== null && result.lon !== null) {
      log('Auto-detected location: lat=' + result.lat + ', lon=' + result.lon + ', tz=' + result.tz);
      setForecastURL(result.lat, result.lon);
      if (typeof cb === 'function') cb();
    } else {
      log('result: ' + JSON.stringify(result));
    }
  } else {
    log('error_code: ' + error_code + ', error_message: ' + error_message);
    onForecastUrlReady(cb);
  }
}

// Parse a value from KVS based on its expected type
function parseValueWithType(valueStr, type) {
  // Handle null/undefined
  if (valueStr === null || valueStr === undefined || valueStr === "null" || valueStr === "undefined") {
    return null;
  }
  
  // Parse based on type
  if (type === "boolean") {
    if (valueStr === "true" || valueStr === true) return true;
    if (valueStr === "false" || valueStr === false) return false;
    return null;
  }
  
  if (type === "number") {
    var num = parseFloat(valueStr);
    if (!isNaN(num)) return num;
    return null;
  }
  
  if (type === "string") {
    if (typeof valueStr === "string") return valueStr;
    return String(valueStr);
  }
  
  if (type === "object") {
    // Try JSON parse for objects/arrays
    try {
      return JSON.parse(valueStr);
    } catch (e) {
      if (e && false) {}  // Prevent minifier from removing parameter
      return null;
    }
  }
  
  // Unknown type - return as-is
  return valueStr;
}

function storeValue(key, value) {
  var valueStr;
  // Use "null" as a sentinel for JS null/undefined so we can round-trip
  // through Script.storage, which only stores strings.
  if (typeof value === 'undefined' || value === null) {
    valueStr = "null";
  } else if (typeof value === 'number' || typeof value === 'boolean') {
    valueStr = value.toString();
  } else if (typeof value === 'string') {
    valueStr = value;
  } else if (value instanceof Date) {
    valueStr = value.toISOString();
  } else if (typeof value === 'object') {
    valueStr = JSON.stringify(value);
  } else {
    valueStr = String(value);
  }
  Script.storage.setItem(key, valueStr);
}

// Simple parse for Script.storage values (we control what we store)
function parseStorageValue(valueStr) {
  if (valueStr === "null" || valueStr === null || valueStr === undefined) {
    return null;
  }
  if (valueStr === "true") return true;
  if (valueStr === "false") return false;
  
  // Try as number
  var num = parseFloat(valueStr);
  if (!isNaN(num) && valueStr === num.toString()) {
    return num;
  }
  
  // Try as JSON (for objects/arrays)
  if (valueStr.charAt(0) === '{' || valueStr.charAt(0) === '[') {
    try {
      return JSON.parse(valueStr);
    } catch (e) {
      if (e && false) {}  // Prevent minifier from removing parameter
    }
  }
  
  // Return as string
  return valueStr;
}

function loadValue(key) {
  var v = Script.storage.getItem(key);
  // Missing key or explicit "null" sentinel both map to JS null
  if (v === null || typeof v === 'undefined' || v === "null") {
    return null;
  }
  return parseStorageValue(v);
}

function setForecastURL(lat, lon) {
  log('setForecastURL', lat, lon);
  if (lat !== null && lon !== null) {
    var url = 'https://api.open-meteo.com/v1/forecast?latitude=' + lat + '&longitude=' + lon + '&hourly=temperature_2m&forecast_days=1&timezone=auto';
    STATE.forecastUrl = url;
    storeValue(STORAGE_KEYS.forecastUrl, url);
    log('Forecast URL ready');
  }
}

function onKvsLoaded(result, error_code, error_message, cb) {
  log('onKvsLoaded');
  var updated = [];
  if (error_code === 0 && result && result.items) {
    log('KVS config loaded, processing', result.items.length, 'items');
    try {
      // Loop through all KVS items
      for (var i = 0; i < result.items.length; i++) {
        var item = result.items[i];
        var itemKey = item.key;
        
        // Check if this key matches any of our config schema
        for (var configName in CONFIG_SCHEMA) {
          var schema = CONFIG_SCHEMA[configName];
          var fullKey = schema.unprefixed ? schema.key : (CONFIG_KEY_PREFIX + schema.key);
          
          if (itemKey === fullKey) {
            var value = parseValueWithType(item.value, schema.type);
            if (value !== null) {
              if (CONFIG[configName] !== value) {
                CONFIG[configName] = value;
                log('Loaded config', configName, '=', value, 'from key', itemKey);
                updated.push(configName);
              }
            }
            break;
          }
        }
      }
    } catch (e) {
      log('Error loading KVS config:', e);
    }
  } else {
    log('Failed to load KVS config (error ' + error_code + '): ' + error_message);
  }
  if (typeof cb === 'function') {
    cb(updated);
  } else {
    log('BUG: No callback provided for onKvsLoaded', JSON.stringify(cb));
  }
}

function loadConfig(cb) {
  log('loadConfig');
  Shelly.call('KVS.GetMany', { match: CONFIG_KEY_PREFIX + "*" }, onKvsLoaded, cb);
}

// === TIME WINDOW FOR HEATING ===
function isCheapHour() {
  var now = new Date();
  var hour = now.getHours();
  return (hour >= CONFIG.cheapStartHour || hour < CONFIG.cheapEndHour);
}

function getFilteredTemp() {
  return kalman.lastMeasurement ? kalman.lastMeasurement() : null;
}

function onCheapWindowEnd() {
  var temp = getFilteredTemp();
  if (temp !== null) {
    var now = (new Date()).getTime();
    var data = { temp: temp, time: now };
    storeValue(STORAGE_KEYS.lastCheapEnd, data);
    log("Stored end-of-cheap-window temp:", temp);
  }
}

function onCheapWindowStart() {
  var data = loadValue(STORAGE_KEYS.lastCheapEnd);
  if (!data) {
    log("No previous cheap window end data available for learning");
    return;
  }
  // loadValue already returns a parsed object when we stored one with storeValue.
  // Be defensive in case of older/corrupted data.
  if (typeof data !== 'object') {
    log("Invalid last cheap end data (not an object)");
    return;
  }
  if (!("temp" in data) || !("time" in data)) {
    log("Invalid last cheap end data (missing fields)");
    return;
  }
  
  var prevTemp = data.temp;
  var prevTime = data.time;
  var now = (new Date()).getTime();
  var hours = (now - prevTime) / (3600 * 1000);
  var currTemp = getFilteredTemp();
  if (currTemp !== null && hours > 0) {
    var rate = (prevTemp - currTemp) / hours;
    // Update moving average
    var oldRate = getCoolingRate();
    var newRate = 0.7 * oldRate + 0.3 * rate; // EMA
    setCoolingRate(newRate);
    log("Updated cooling rate:", newRate, "from", oldRate);
  }
}

// Schedule learning events at CHEAP_START_HOUR and CHEAP_END_HOUR
function scheduleLearningTimers() {
  // Clear old timers if any
  for (var i = 0; i < STATE.learningTimerIds.length; i++) {
    removeTimer(STATE.learningTimerIds[i]);
  }
  STATE.learningTimerIds = [];
  
  var now = new Date();
  var hour = now.getHours();
  
  var scheduleAt = function(targetHour, cb) {
    var delay = (targetHour - hour) * 3600000;
    if (delay < 0) delay += 24 * 3600000;
    
    // Schedule initial one-shot timer
    var id = addTimer(delay, false, function() {
      cb();
      // After first fire, schedule recurring daily timer
      var recurringId = addTimer(24 * 3600000, true, cb);
      STATE.learningTimerIds.push(recurringId);
    });
    STATE.learningTimerIds.push(id);
  };
  
  scheduleAt(CONFIG.cheapEndHour, onCheapWindowEnd);
  scheduleAt(CONFIG.cheapStartHour, onCheapWindowStart);
}

function initUrls() {
  log('initUrls');
  // Try to get MQTT status synchronously
  var cfg = Shelly.getComponentConfig('mqtt');
  if (cfg && typeof cfg === 'object') {
    if ("client_id" in cfg && typeof cfg.client_id === 'string') {
      if (cfg.client_id.length > 0) {
        log("client_id:", cfg.client_id);
        STATE.clientId = cfg.client_id;
      } else {
        var info = Shelly.getDeviceInfo();
        log("client_id(device_id):", info.id);
        STATE.clientId = info.id;
      }
    }
    if ("server" in cfg && typeof cfg.server === 'string') {
      // server = "192.168.1.2:1883"
      var host = cfg.server;
      var i = host.indexOf(':');
      if (i >= 0) host = host.substring(0, i);
      STATE.occupancyUrl = 'http://' + host + ':8889/status';
      log('Occupancy URL set to', STATE.occupancyUrl);
    }
  }
}

// === PRE-HEATING LOGIC ===
function minutesUntilCheapEnd() {
  var now = new Date();
  var hour = now.getHours();
  var minute = now.getMinutes();
  var end = CONFIG.cheapEndHour;
  var minutesNow = hour * 60 + minute;
  var minutesEnd = end * 60;
  if (end <= CONFIG.cheapStartHour) minutesEnd += 24 * 60; // handle overnight windows
  if (minutesNow > minutesEnd) minutesEnd += 24 * 60; // handle wrap-around
  return minutesEnd - minutesNow;
}

// === ADVANCED COOLING MODEL: LOSS DEPENDS ON TEMP DIFFERENCE ===
// We now use: predictedDrop = COOLING_COEFF * (filteredTemp - externalTemp) * hoursToEnd
// COOLING_COEFF is learned as before (from data)

function shouldPreheat(filteredTemp, forecastTemp, mfTemp, cb) {
  k = getCoolingRate(); // k is now a cooling coefficient (per hour)
  var minutesToEnd = minutesUntilCheapEnd();
  var hoursToEnd = minutesToEnd / 60.0;
  // Use the lowest forecast for the next N hours for external temp
  var futureExternal = null;
  if (forecastTemp !== null && mfTemp !== null) futureExternal = Math.min(forecastTemp, mfTemp);
  else if (forecastTemp !== null) futureExternal = forecastTemp;
  else if (mfTemp !== null) futureExternal = mfTemp;
  // Fallback to current external temp if no forecast
  if (futureExternal === null && typeof lastExternalTemp !== 'undefined') {
    futureExternal = lastExternalTemp;
  }
  // If still null, fallback to 0
  if (futureExternal === null) futureExternal = 0;
  // Predict indoor temp at end of cheap window using exponential model
  // T_end = T_start - k * (T_start - T_ext) * hours
  var predictedDrop = k * (filteredTemp - futureExternal) * hoursToEnd;
  var predictedTemp = filteredTemp - predictedDrop;
  cb((hoursToEnd <= CONFIG.preheatHours) && (predictedTemp < CONFIG.setpoint));
}

// Store last measured external temp for fallback in shouldPreheat
var lastExternalTemp = null;

// === PARALLEL DATA FETCH HELPERS (reduce callback nesting) ===
// Note: Must be defined BEFORE being patched below (no hoisting in Shelly JS)
function fetchAllControlInputs(cb) {
  // Check if we need to refresh the forecast (once per day)
  if (shouldRefreshForecast()) {
    log('Fetching fresh forecast from Open-Meteo...');
    fetchAndCacheForecast(fetchControlInputsWithCachedForecast.bind(null, cb));
  } else {
    // Use cached forecast
    log('Using cached forecast');
    fetchControlInputsWithCachedForecast(cb);
  }
}

function fetchControlInputsWithCachedForecast(cb) {
  log('fetchControlInputsWithCachedForecast')
  var results = {
    internal: STATE.temperature['internal'],
    external: STATE.temperature['external'],
    forecast: getCurrentForecastTemp(),
    occupied: STATE.occupied
  };
  cb(results);
}

// Patch fetchAllControlInputs to store last external temp
var origFetchAll = fetchAllControlInputs;

fetchAllControlInputs = function(cb) {
  origFetchAll(function(results) {
    if (results.external !== null) lastExternalTemp = results.external;
    log('Fetched all control inputs:', results);
    cb(results);
  });
};

function getOccupancy(cb) {
  log('getOccupancy')
  if (!STATE.occupancyUrl) {
    log('Occupancy URL not configured, assuming not occupied');
    cb(false);
    return;
  }
  
  Shelly.call("HTTP.GET", {
    url: STATE.occupancyUrl,
    timeout: 5
  }, function(result, error_code, error_message) {
    if (error_code === 0 && result && result.body) {
      var data = null;
      try { data = JSON.parse(result.body); } catch (e) { if (e && false) {} }
      cb(data && data.occupied === true);
    } else {
      log('Error fetching occupancy status:', error_message);
      cb(false); // Default: not occupied
    }
  });
}

// === KALMAN FILTER IMPLEMENTATION (ES5) ===
function KalmanFilter(R, Q, A, B, C) {
  this.R = typeof R !== 'undefined' ? R : 0.01;
  this.Q = typeof Q !== 'undefined' ? Q : 1;
  this.A = typeof A !== 'undefined' ? A : 1;
  this.B = typeof B !== 'undefined' ? B : 0;
  this.C = typeof C !== 'undefined' ? C : 1;
  this.cov = NaN;
  this.x = NaN;
}
KalmanFilter.prototype.filter = function(z, u) {
  if (typeof u === 'undefined') u = 0;
  if (isNaN(this.x)) {
    this.x = (1 / this.C) * z;
    this.cov = (1 / this.C) * this.Q * (1 / this.C);
  } else {
    var predX = this.A * this.x + this.B * u;
    var predCov = this.A * this.cov * this.A + this.R;
    var K = predCov * this.C / (this.C * predCov * this.C + this.Q);
    this.x = predX + K * (z - this.C * predX);
    this.cov = predCov - K * this.C * predCov;
  }
  return this.x;
};
KalmanFilter.prototype.lastMeasurement = function() {
  return this.x;
};

// Initialize Kalman filter instance
var kalman = new KalmanFilter();

// === MQTT TEMPERATURE HANDLING ===
// Detect if topic is Gen1 or Gen2 format and extract temperature
function parseTemperatureFromMqtt(topic, message) {
  var temp = null;
  log("parseTemperatureFromMqtt", topic, message);
  try {
    // Gen1 format: shellies/<id>/sensor/temperature with plain number payload
    if (topic.indexOf('shellies/') === 0 && topic.indexOf('/sensor/temperature') > 0) {
      temp = parseFloat(message);
      if (!isNaN(temp)) {
        log('Parsed Gen1 temperature:', temp, 'from topic:', topic);
        return temp;
      }
    }
    // Gen2 format: <id>/events/rpc with JSON payload
    else if (topic.indexOf('/events/rpc') > 0) {
      var data = JSON.parse(message);
      // Look for temperature in NotifyStatus params
      if (data.method === 'NotifyStatus' && data.params) {
        // Check various temperature component formats
        if (data.params['temperature:0'] && typeof data.params['temperature:0'].tC !== 'undefined') {
          temp = data.params['temperature:0'].tC;
        } else if (data.params['temperature:1'] && typeof data.params['temperature:1'].tC !== 'undefined') {
          temp = data.params['temperature:1'].tC;
        } else if (data.params['temperature:2'] && typeof data.params['temperature:2'].tC !== 'undefined') {
          temp = data.params['temperature:2'].tC;
        }
        if (temp !== null) {
          log('Parsed Gen2 temperature:', temp, 'from topic:', topic);
          return temp;
        }
      }
    }
    else {
      log('Unknown topic format:', topic);
    }
  } catch (e) {
    log('Error parsing temperature from MQTT:', e);
  }
  return null;
}

function onTemperature(topic, message, location) {
  var temp = parseTemperatureFromMqtt(topic, message);
  if (temp !== null) {
    // Store in STATE (in-memory cache)
    STATE.temperature[location] = temp;
    STATE.temperatureReady[location] = true;
    log('Temperature', temp, 'location:', location, 'is ready:', STATE.temperatureReady[location]);
    
    // Schedule check if not already scheduled
    scheduleControlLoopCheck();
  }
}

// Extract device name from MQTT topic (e.g., "shellies/device-name/sensor/temperature" -> "device-name")
function extractDeviceNameFromTopic(topic) {
  if (!topic) return null;
  var parts = topic.split('/');
  if (parts.length >= 2) {
    return parts[1];
  }
  return null;
}

// Generate a simple random request ID
function generateRequestId() {
  var chars = 'abcdefghijklmnopqrstuvwxyz0123456789';
  var id = '';
  for (var i = 0; i < 16; i++) {
    id += chars[Math.floor(Math.random() * chars.length)];
  }
  return id;
}

/**
 * Request `myhome` to republish its last cached value for the given topic.
 * @param {Request} topic 
 */
function requestMqttRepeat(topic) {
  var request = JSON.stringify({
    id: generateRequestId(),
    src: STATE.clientId,
    dst: 'myhome',
    method: 'mqtt.repeat',
    params: topic
  });

  log('Publishing request to myhome/rpc:', request);
  
  MQTT.publish('myhome/rpc', request, 0, false);
}

function subscribeMqttTemperature(location, topic) {
  log('Subscribing to MQTT topic for', location, 'temperature...');
  
  var oldTopic = STATE.subscribedTemperatureTopic[location];
  var newTopic = topic;
  
  // Skip if already subscribed to this topic
  if (oldTopic === newTopic && newTopic) {
    log('Already subscribed to', location, 'topic:', newTopic);
    return;
  }
  
  // Unsubscribe from old topic if it changed
  if (oldTopic && oldTopic !== newTopic) {
    log('Unsubscribing from old', location, 'topic:', oldTopic);
    MQTT.unsubscribe(oldTopic);
    STATE.subscribedTemperatureTopic[location] = null;
  }
  
  // Subscribe to new topic & request last value
  if (newTopic) {
    log('Subscribing to', location, 'temperature topic:', newTopic);
    MQTT.subscribe(newTopic, onTemperature, location);
    STATE.subscribedTemperatureTopic[location] = newTopic;
    
    // Clear old MQTT repeat timer if any
    if (STATE.mqttRepeatTimerId) {
      removeTimer(STATE.mqttRepeatTimerId);
    }
    
    // Delay the repeat request to ensure subscription is active
    // MQTT.subscribe() may not be synchronous on the device
    STATE.mqttRepeatTimerId = addTimer(100, false, function() {
      requestMqttRepeat(newTopic);
      STATE.mqttRepeatTimerId = null;
    });
  }
}

// === DATA FETCHING FUNCTIONS ===
// Read temperature from STATE (in-memory cache)
function getShellyTemperature(location, cb) {
  log('getShellyTemperature', location);
  var temp = STATE.temperature[location];
  
  if (temp !== null && temp !== undefined) {
    log('Read', location, 'temperature:', temp);
    cb(temp);
  } else {
    log('No', location, 'temperature available yet');
    cb(null);
  }
}

function shouldRefreshForecast() {
  var now = new Date();
  var today = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
  
  // Refresh if never fetched or if it's a new day
  if (STATE.lastForecastFetchDate === null || STATE.lastForecastFetchDate !== today) {
    return true;
  }
  return false;
}

function onForecast(result, error_code, error_message) {
  if (error_code !== 0) {
    log('Forecast fetch error:', error_code);
    return;
  }
  
  if (!result || !result.body) {
    log('No forecast data in response');
    return;
  }
  
  var data = null;
  try { 
    data = JSON.parse(result.body);
  } catch (e) { 
    log('JSON parse error');
    if (e && false) {}
    return;
  }
  
  if (!data || !data.hourly || !data.hourly.temperature_2m || data.hourly.temperature_2m.length === 0) {
    log('Invalid forecast structure');
    return;
  }
  
  // Cache only the arrays we need, let GC clean up the rest
  STATE.cachedForecast = data.hourly.temperature_2m;
  STATE.cachedForecastTimes = data.hourly.time;
  data = null; // Help GC
  
  var now = new Date();
  STATE.lastForecastFetchDate = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
  log('Forecast cached:', STATE.cachedForecast.length, 'values');
  
  // Schedule check if not already scheduled
  scheduleControlLoopCheck();
}

function fetchAndCacheForecast(cb) {
  var url = STATE.forecastUrl || loadValue(STORAGE_KEYS.forecastUrl);
  if (!url) {
    log('Open-Meteo forecast URL not configured. Skipping forecast.');
    return;
  }
  
  log('Fetching fresh forecast from Open-Meteo...');
  Shelly.call("HTTP.GET", {
    url: url,
    timeout: 10
  }, onForecast, cb);
}

function getCurrentForecastTemp() {
  if (!STATE.cachedForecast || STATE.cachedForecast.length === 0) {
    return null;
  }
  
  var now = new Date();
  var currentHour = now.getHours();
  
  // Use current hour as index (Open-Meteo returns data starting from 00:00 today)
  var idx = currentHour < STATE.cachedForecast.length ? currentHour : 0;
  var temp = STATE.cachedForecast[idx];
  
  return temp;
}

// Get minimum forecast temperature for the next N hours
function getMinForecastTemp(hours) {
  if (!STATE.cachedForecast || STATE.cachedForecast.length === 0) {
    return null;
  }
  
  var now = new Date();
  var currentHour = now.getHours();
  
  // Calculate how many hours to look ahead (capped by available forecast data)
  var hoursToCheck = Math.min(Math.ceil(hours), STATE.cachedForecast.length - currentHour);
  if (hoursToCheck <= 0) {
    return getCurrentForecastTemp();
  }
  
  // Find minimum temperature in the next N hours
  var minTemp = null;
  for (var i = 0; i < hoursToCheck; i++) {
    var idx = currentHour + i;
    if (idx < STATE.cachedForecast.length) {
      var temp = STATE.cachedForecast[idx];
      if (temp !== null && temp !== undefined) {
        if (minTemp === null || temp < minTemp) {
          minTemp = temp;
        }
      }
    }
  }
  
  return minTemp;
}

function controlHeaterWithInputs(results) {
  var internalTemp = results.internal;
  var externalTemp = results.external;
  var forecastTemp = results.forecast;
  var isOccupied = results.occupied;
  log('Internal:', internalTemp, 'External:', externalTemp, 'Forecast:', forecastTemp, 'Occupied:', isOccupied);
  if (internalTemp === null) {
    log('Skipping control cycle due to missing internal temperature');
    return;
  }
  var controlInput = 0;
  var count = 0;
  if (externalTemp !== null) { controlInput += externalTemp; count++; }
  if (forecastTemp !== null) { controlInput += forecastTemp; count++; }
  if (count > 0) controlInput = controlInput / count;
  var filteredTemp = kalman.filter(internalTemp, controlInput);
  log('Filtered temperature:', filteredTemp);
  var heaterShouldBeOn = filteredTemp < CONFIG.setpoint;
  // SAFETY: If filtered temperature is below minInternalTemp, always heat IF occupied
  if (isOccupied && filteredTemp < CONFIG.minInternalTemp) {
    log('Safety override: internal temp', filteredTemp, 'below minInternalTemp', CONFIG.minInternalTemp, '=> HEAT');
    setHeaterState(true);
    return;
  }
  // Calculate minimum forecast temperature for preheat window
  var mfTemp = getMinForecastTemp(CONFIG.preheatHours);
  log('Minimum forecast temp for next', CONFIG.preheatHours, 'hours:', mfTemp);
  shouldPreheat(filteredTemp, forecastTemp, mfTemp, function(preheat) {
    if ((heaterShouldBeOn && isCheapHour()) || preheat) {
      log('Heater ON (normal or preheat mode)', 'preheat:', preheat);
      setHeaterState(true);
    } else {
      log('Outside cheap window => no heating');
      setHeaterState(false);
    }
  });
}

// === MAIN CONTROL LOOP (flattened) ===
function pollAndControl() {
  // Check if we need to refresh the forecast (once per day)
  if (shouldRefreshForecast()) {
    log('Daily forecast refresh triggered from poll');
    fetchAndCacheForecast();
  }
  
  // Only run control if we have all necessary inputs
  if (!STATE.forecastUrl || !STATE.cachedForecast || !STATE.temperatureReady.internal || !STATE.temperatureReady.external) {
    log('Skipping control cycle - waiting for initialization (url:', !!STATE.forecastUrl, 'forecast:', !!STATE.cachedForecast, 'internal:', STATE.temperatureReady.internal, 'external:', STATE.temperatureReady.external, ')');
    return;
  }
  
  fetchAllControlInputs(controlHeaterWithInputs);
}

// Schedule a deferred check (breaks call stack without creating multiple timers)
function scheduleControlLoopCheck() {
  if (!STATE.checkTimerId) {
    STATE.checkTimerId = addTimer(0, false, function() {
      STATE.checkTimerId = null;
      checkAndStartControlLoop();
    });
  }
}

// Check if ready and start control loop timer
function checkAndStartControlLoop() {
  log('Checking whether we can start control loop')
  log('  - Forecast URL ready:' + !!STATE.forecastUrl);
  log('  - Forecast data ready:' + !!STATE.cachedForecast);
  log('  - Internal temp ready:' + STATE.temperatureReady.internal);
  log('  - External temp ready:' + STATE.temperatureReady.external);
  if (STATE.forecastUrl && STATE.cachedForecast && STATE.temperatureReady.internal && STATE.temperatureReady.external) {
    if (!STATE.controlLoopTimerId) {
      log('All inputs ready - starting control loop timer');
      // Start the control loop timer now that all inputs are ready
      STATE.controlLoopTimerId = addTimer(CONFIG.pollIntervalMs, true, pollAndControl);
      // Run first cycle immediately
      pollAndControl();
    }
  }
}

// === HEATER CONTROL (LOCAL SHELLY CALL, SUPPORTS normally-closed VALUE) ===
function setHeaterState(on, cb) {
  STATE.lastHeaterState = on;
  var newState = on !== CONFIG.normallyClosed
  Shelly.call("Switch.Set", { id: 0, on: newState }, function(result, error_code, error_msg, userdata) {
    if (error_code) {
      log('Error setting heater switch state:', error_msg);
    } else {
      log('Heater switch set to', on, "(result:", result, ")");
      if (typeof userdata === 'function') userdata();
    }
  }, cb);
}

function onConfigLoaded(updated) {
  log('onConfigLoaded', JSON.stringify(updated));

  if (updated.indexOf('externalTemperatureTopic') !== -1) {
    subscribeMqttTemperature("external", CONFIG.externalTemperatureTopic);
  }
  if (updated.indexOf('internalTemperatureTopic') !== -1) {
    subscribeMqttTemperature("internal", CONFIG.internalTemperatureTopic);
  }
}

// === SCHEDULED EXECUTION ===
log("Script starting...");

// Initialize URLs (occupancy service)
initUrls();

scheduleLearningTimers();
loadConfig(onConfigLoaded);
fetchForecast();

Shelly.addStatusHandler(function(status) {
  // Detect KVS updates and reload configuration
  if (status && status.component === "sys" && status.delta && ("kvs_rev" in status.delta)) {
    log('KVS updated (rev ' + status.delta.kvs_rev + '), reloading configuration and re-fetching temperatures');
    loadConfig(onConfigLoaded);
  } else {
    log('Script status:', JSON.stringify(status));
  }
});

log("Script started");

