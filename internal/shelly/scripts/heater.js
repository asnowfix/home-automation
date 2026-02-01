// Kalman filter heater controller for Shelly Plus 1 (ES5 style, no Node.js, for Shelly scripting engine)
// Uses Open-Meteo API for weather forecasts (no API key required)
//
// === TIME FORMAT REFERENCE ===
// The temperature service returns time ranges as minutes since midnight (0-1439)
// This allows fast integer comparisons without string parsing.
//
// Common time conversions:
//   00:00 = 0     (0×60 + 0)
//   06:00 = 360   (6×60 + 0)
//   08:00 = 480   (8×60 + 0)
//   12:00 = 720   (12×60 + 0)
//   18:00 = 1080  (18×60 + 0)
//   23:00 = 1380  (23×60 + 0)
//   23:59 = 1439  (23×60 + 59)
//
// To get current time in minutes: var now = new Date(); var mins = now.getHours() * 60 + now.getMinutes();
// To check if in range: if (mins >= range.start && mins < range.end) { ... }
// Handle midnight crossing: if (range.end < range.start) { if (mins >= range.start || mins < range.end) { ... } }

// === STATIC CONSTANTS ===
const SCRIPT_NAME = "heater";
const CONFIG_KEY_PREFIX = 'script/' + SCRIPT_NAME + '/';
const SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";
const DEFAULT_COOLING_RATE = 1.0;

// Configuration schema with type information
// IMPORTANT: This schema is synchronized with heaterKVSKeys in myhome/ctl/heater/main.go
// Any changes here must be reflected in the Go code and validated by TestHeaterKVSKeysMatchJSSchema
var CONFIG_SCHEMA = {
  enableLogging: {
    description: "Enable logging when true",
    key: "enable-logging",
    default: true,
    type: "boolean"
  },
  roomId: {
    description: "Room identifier for temperature API",
    key: "room-id",
    default: null,
    type: "string",
    unprefixed: true
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
    default: 5 * 60 * 1000, // 5 minutes
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
 * @params
 */

/**
 * Logs a message if logging is enabled.
 * @returns void
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
      if (e && false) { }
    }
    if (i + 1 < arguments.length) s += " ";
  }
  print(SCRIPT_PREFIX, s);
}

// Log memory usage for diagnostics
function logMemory(label) {
  if (!CONFIG.enableLogging) return;
  var status = Shelly.getComponentStatus('sys');
  if (status && ("ram_free" in status)) {
    print(SCRIPT_PREFIX, 'MEMORY [' + label + '] ram_free:', status.ram_free);
  }
}

// Generate a simple random ID
function randomId(n) {
  var chars = 'abcdefghijklmnopqrstuvwxyz0123456789';
  var id = '';
  for (var i = 0; i < n; i++) {
    id += chars[Math.floor(Math.random() * chars.length)];
  }
  return id;
}

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // Device info (cached at startup)
  deviceInfo: null,
  deviceId: null,
  deviceName: null,

  // Forecast cache
  forecastUrl: null,
  cachedForecast: null,
  cachedForecastTimes: null,
  lastForecastFetchDate: null,

  // Heater state
  lastHeaterState: false,
  normallyClosed: true,

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

  // Temperature ranges subscription topic
  subscribedRangesTopic: null,

  // Occupancy subscription flag
  subscribedOccupancy: false,

  // {
  //   "room_id": "bureau",
  //   "date": "2025-11-30",
  //   "day_type": "day-off",
  //   "levels": {
  //     "comfort": 21,
  //     "eco": 17
  //   },
  //   "ranges": [
  //     {
  //       "start": 600,
  //       "end": 720
  //     }
  //   ]
  // }
  cachedTemperatureRanges: null,

  // Temperature ranges subscription topics (for dynamic config change)
  temperatureRangesTopic: null,
  subscribedTemperatureRangesTopic: null,

  // Control loop timer handle
  controlLoopTimerId: null,

  // Pending control loop check timer handle (for deferred checks)
  controlLoopCheckTimerId: null,

  // Last successful temperature ranges from RPC
  lastSuccessfulRanges: null
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
  if (!valueStr || valueStr === "null" || valueStr === "undefined") {
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
      if (e && false) { }  // Prevent minifier from removing parameter
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
  if (valueStr === "null" || !valueStr) {
    return null;
  }

  // Convert to string if it's not already a string
  if (typeof valueStr !== "string") {
    valueStr = String(valueStr);
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
      if (e && false) { }  // Prevent minifier from removing parameter
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

function onKvsLoaded(result, error_code, error_message, userdata) {
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
  if (typeof userdata === 'function') {
    userdata(updated);
  } else {
    log('BUG: onKvsLoaded: type:', typeof userdata, 'value:', JSON.stringify(userdata));
  }
}

function loadConfig(cb) {
  log('loadConfig');
  // Load every KVS, filter-out later
  Shelly.call('KVS.GetMany', { match: "*" }, onKvsLoaded, cb);
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
// Uses a single hourly timer to check and fire events, conserving timer slots
var learningTimerId = null;

function checkLearningEvents() {
  var now = new Date();
  var hour = now.getHours();

  if (hour === CONFIG.cheapEndHour) {
    log('Cheap window ended - triggering learning event');
    onCheapWindowEnd();
  } else if (hour === CONFIG.cheapStartHour) {
    log('Cheap window started - triggering learning event');
    onCheapWindowStart();
  }
}

function scheduleLearningTimers() {
  // Use a single recurring timer that checks hourly instead of 2 separate timers
  // This conserves timer slots (Shelly limit: 5 timers per script)
  if (learningTimerId !== null) {
    log('Learning timer already scheduled');
    return;
  }

  // Check immediately at startup
  checkLearningEvents();

  // Then check every hour
  learningTimerId = Timer.set(3600000, true, checkLearningEvents);
  log('Learning timer scheduled (hourly check), id:', learningTimerId);
}

function initDeviceInfo() {
  log('initDeviceInfo');

  // Cache device info at startup (call getDeviceInfo only once)
  STATE.deviceInfo = Shelly.getDeviceInfo();

  // Get MQTT status synchronously
  var cfg = Shelly.getComponentConfig('mqtt');
  if (cfg && typeof cfg === 'object') {
    if ("client_id" in cfg && typeof cfg.client_id === 'string') {
      if (cfg.client_id.length > 0) {
        log("client_id:", cfg.client_id);
        STATE.clientId = cfg.client_id;
      } else {
        log("client_id(device_id):", STATE.deviceInfo.id);
        STATE.clientId = STATE.deviceInfo.id;
      }
    }
    if ("server" in cfg && typeof cfg.server === 'string') {
      // server = "192.168.1.2:1883"
      var host = cfg.server;
      var i = host.indexOf(':');
      if (i >= 0) host = host.substring(0, i);

      log('MQTT server host:', host);
    }
  }

  STATE.deviceName = STATE.deviceInfo.name || STATE.deviceInfo.id || 'unknown';
  STATE.deviceId = STATE.deviceInfo.id || 'unknown';

  log('Device ID:', STATE.deviceId, 'Device Name:', STATE.deviceName);
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

function shouldPreheat(filteredTemp, forecastTemp, mfTemp, targetTemp) {
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
  var result = (hoursToEnd <= CONFIG.preheatHours) && (predictedTemp < targetTemp);

  return result
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

// Guard against recursive calls
var _fetchingControlInputs = false;

function fetchControlInputsWithCachedForecast(cb) {
  // Prevent recursion
  if (_fetchingControlInputs) {
    log('WARNING: fetchControlInputsWithCachedForecast called recursively, skipping');
    return;
  }
  _fetchingControlInputs = true;

  log('fetchControlInputsWithCachedForecast')
  var results = {
    internal: STATE.temperature['internal'],
    external: STATE.temperature['external'],
    forecast: getCurrentForecastTemp(),
    isComfortTime: isComfortTime(),
    occupied: STATE.occupied
  };
  // Store last external temp for fallback in shouldPreheat
  if (results.external !== null) lastExternalTemp = results.external;
  log('Fetched all control inputs:', JSON.stringify(results));

  // Clear guard before calling callback
  _fetchingControlInputs = false;

  // Validate callback is the expected function
  if (typeof cb !== 'function') {
    log('ERROR: cb is not a function:', typeof cb);
    return;
  }

  // Call callback - defer to break call stack and prevent recursion
  Timer.set(0, false, function() {
    cb(results);
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
KalmanFilter.prototype.filter = function (z, u) {
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
KalmanFilter.prototype.lastMeasurement = function () {
  return this.x;
};

// Initialize Kalman filter instance
var kalman = new KalmanFilter();

/**
 * Parse temperature from MQTT message
 * @param {string} topic - MQTT topic
 * @param {string} message - MQTT message payload
 * @returns {number|null} Parsed temperature or null if not found
 */
function parseTemperatureFromMqtt(topic, message) {
  var temp = null;
  log("parseTemperatureFromMqtt", topic, message);
  try {
    // H&T Gen1 format, via gen1 HTTP-to-MQTT proxy
    // topic: shellies/<id>/sensor/temperature
    // message: plain number payload
    if (topic.indexOf('shellies/') === 0 && topic.indexOf('/sensor/temperature') > 0) {
      temp = parseFloat(message);
      if (!isNaN(temp)) {
        log('Parsed Gen1 temperature:', temp, 'from topic:', topic);
        return temp;
      }
    }
    // H&T BLU Gen3 format, via `blu-publisher.js` script
    // topic: shelly-blu/events/7c:c6:b6:7f:48:ed
    // message: {"encryption":false,"BTHome_version":2,"pid":149,"battery":100,"humidity":52,"temperature":17,"rssi":-92,"address":"7c:c6:b6:7f:48:ed"}
    if (topic.indexOf('shelly-blu/events/') === 0) {
      var data = JSON.parse(message);
      if (data.temperature) {
        log('Parsed BLU Gen3 temperature:', data.temperature, 'from topic:', topic);
        return data.temperature;
      }
    }
    // Gen2 format: <id>/events/rpc with JSON payload
    // topic: <id>/events/rpc
    // message: {"method":"NotifyStatus","params":{"temperature:0":{"tC":22.5}}}
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

function handleTemperatureUpdate(location, temp) {
  if (temp !== null) {
    // Store in STATE (in-memory cache)
    STATE.temperature[location] = temp;
    log('Temperature', temp, 'location:', location);
    logMemory('onTemperature:' + location);

    // Schedule check if not already scheduled
    log('About to call scheduleControlLoopCheck');
    scheduleControlLoopCheck();
    log('Returned from scheduleControlLoopCheck');
  }
}

function onInternalTemperature(topic, message) {
  var temp = parseTemperatureFromMqtt(topic, message);
  handleTemperatureUpdate('internal', temp);
}

function onExternalTemperature(topic, message) {
  var temp = parseTemperatureFromMqtt(topic, message);
  handleTemperatureUpdate('external', temp);
}

// Handle temperature ranges update from MQTT
function onTemperatureRanges(topic, message) {
  log('Received temperature ranges update from MQTT:', topic);
  try {
    log('Received temperature ranges update from MQTT:', message);
    var data = JSON.parse(message);
    if (data && data.ranges) {
      STATE.cachedTemperatureRanges = data
      log('Updated temperature ranges:', JSON.stringify(STATE.cachedTemperatureRanges));

      // Schedule control loop check
      scheduleControlLoopCheck();
    }
  } catch (e) {
    if (e && false) { } // Minifier-safe catch
    log('Error parsing temperature ranges from MQTT:', e);
  }
}

// Subscribe to temperature ranges topic for this room
function subscribeToTemperatureRanges() {
  // Unsubscribe from old topic if exists
  if (STATE.subscribedRangesTopic) {
    log('Unsubscribing from old ranges topic:', STATE.subscribedRangesTopic);
    MQTT.unsubscribe(STATE.subscribedRangesTopic);
  }

  // Subscribe to new topic
  log('Subscribing to temperature ranges topic:', STATE.temperatureRangesTopic);
  MQTT.subscribe(STATE.temperatureRangesTopic, onTemperatureRanges);
  STATE.subscribedRangesTopic = STATE.temperatureRangesTopic;
}

function getTemperatureLevels() {
  if (!STATE.cachedTemperatureRanges) {
    log('No cached temperature ranges found: using defaults');
    return {
      comfort: 21,
      eco: 17,
      away: 12
    };
  }
  return STATE.cachedTemperatureRanges.levels;
}

function getTemperatureRanges() {
  if (!STATE.cachedTemperatureRanges) {
    log('No cached temperature ranges found: using defaults');
    return [];
  }
  return STATE.cachedTemperatureRanges.ranges;
}

function isComfortTime() {
  // Get current time in minutes since midnight
  var now = new Date();
  var currentMinutes = now.getHours() * 60 + now.getMinutes();

  ranges = getTemperatureRanges();

  for (var i = 0; i < ranges.length; i++) {
    var r = ranges[i];

    // Handle midnight crossing
    if (r.end < r.start) {
      if (currentMinutes >= r.start || currentMinutes < r.end) {
        return true;
      }
    } else {
      // Normal range
      if (currentMinutes >= r.start && currentMinutes < r.end) {
        return true;
      }
    }
  }
  return false;
}

function subscribeToOccupancy() {
  // Skip if already subscribed
  if (STATE.subscribedOccupancy) {
    return;
  }
  STATE.subscribedOccupancy = true;

  MQTT.subscribe("myhome/occupancy", function (topic, message) {
    log('Received occupancy message:', message);

    var response = null;
    try {
      response = JSON.parse(message);
      log('Occupancy: ', JSON.stringify(response));
      STATE.occupied = response.occupied;
    } catch (e) {
      if (e && false) { }
      log('Failed to JSON-parse occupancy message:', message);
    }
  });
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

function subscribeMqttTemperature(location, topic) {
  log('Subscribing to MQTT topic for', location, 'temperature...');

  var oldTopic = STATE.subscribedTemperatureTopic[location];
  var newTopic = topic;

  // Skip if already subscribed to this topic
  if (oldTopic === newTopic && newTopic) {
    log('Already subscribed to', location, ' (or invalid) topic:', newTopic);
    return;
  }

  // Unsubscribe from old topic if it changed
  if (oldTopic && oldTopic !== newTopic) {
    log('Unsubscribing from old', location, 'topic:', oldTopic);
    MQTT.unsubscribe(oldTopic);
    STATE.subscribedTemperatureTopic[location] = null;
  }

  // Subscribe to new topic with location-specific callback
  // Using separate named functions avoids userdata parameter issues
  if (newTopic) {
    log('Subscribing to', location, 'temperature topic:', newTopic);
    if (location === 'internal') {
      MQTT.subscribe(newTopic, onInternalTemperature);
    } else if (location === 'external') {
      MQTT.subscribe(newTopic, onExternalTemperature);
    }
    STATE.subscribedTemperatureTopic[location] = newTopic;
  }
}

// === DATA FETCHING FUNCTIONS ===
// Read temperature from STATE (in-memory cache)
function getShellyTemperature(location, cb) {
  log('getShellyTemperature', location);
  var temp = STATE.temperature[location];

  if (!temp) {
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

function scheduleForecastRetry() {
  // Retry forecast fetch after 10 seconds if not already cached
  if (!STATE.cachedForecast) {
    log('Scheduling forecast retry in 10 seconds...');
    Timer.set(10000, false, function () {
      if (!STATE.cachedForecast) {
        log('Retrying forecast fetch...');
        fetchAndCacheForecast();
      }
    });
  }
}

function onForecast(result, error_code, error_message, cb) {
  if (error_code !== 0) {
    log('Forecast fetch error code:', error_code, 'message:', error_message);
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }

  if (!result || !result.body) {
    log('No forecast data in response');
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }

  var data = null;
  try {
    data = JSON.parse(result.body);
  } catch (e) {
    log('JSON parse error:', result.body);
    if (e && false) { }
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }

  if (!data || !data.hourly || !data.hourly.temperature_2m || data.hourly.temperature_2m.length === 0) {
    log('Invalid forecast structure data:', data);
    scheduleForecastRetry();
    if (typeof cb === 'function') cb();
    return;
  }

  // Cache only the arrays we need, let GC clean up the rest
  STATE.cachedForecast = data.hourly.temperature_2m;
  STATE.cachedForecastTimes = data.hourly.time;
  data = null; // Help GC

  var now = new Date();
  STATE.lastForecastFetchDate = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
  log('Forecast cached:', STATE.cachedForecast.length, 'values');

  // Call the callback if provided, but defer it to break the call stack
  // This prevents "Too much recursion" errors from deep callback nesting
  if (typeof cb === 'function') {
    Timer.set(0, false, cb);
  }
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
      if (!!temp) {
        if (minTemp === null || temp < minTemp) {
          minTemp = temp;
        }
      }
    }
  }

  return minTemp;
}

function controlHeaterWithInputs(results) {
  logMemory('controlHeaterWithInputs:start');
  var internalTemp = results.internal;
  var externalTemp = results.external;
  var forecastTemp = results.forecast;
  var isComfortTime = results.isComfortTime;
  var isOccupied = results.occupied;

  log('Internal:', internalTemp, 'External:', externalTemp, 'Forecast:', forecastTemp, 'isComfortTime:', isComfortTime, 'Occupied:', isOccupied);

  if (internalTemp === null) {
    log('Skipping control cycle due to missing internal temperature');
    return;
  }

  levels = getTemperatureLevels()
  log('Levels:', levels)

  if (!isOccupied) {
    log('Not occupied, using away level')
    targetTemp = levels.away
  } else if (isComfortTime) {
    log('Comfort time, using comfort level')
    targetTemp = levels.comfort
  } else {
    log('Defaulting to eco level')
    targetTemp = levels.eco
  }

  var controlInput = 0;
  var count = 0;
  if (externalTemp !== null) { controlInput += externalTemp; count++; }
  if (forecastTemp !== null) { controlInput += forecastTemp; count++; }
  if (count > 0) controlInput = controlInput / count;

  var filteredTemp = kalman.filter(internalTemp, controlInput);
  log('Filtered temperature:', filteredTemp, 'Target:', targetTemp);

  var heaterShouldBeOn = filteredTemp < targetTemp;

  // SAFETY: Always heat if below away setpoint (frost protection)
  if (filteredTemp < levels.away) {
    log('Safety: internal temp', filteredTemp, 'below away setpoint', levels.away, '=> HEAT');
    setHeaterState(true);
    return;
  }

  // ECO FLOOR: If occupied and below eco level, always heat regardless of cheap time
  // This ensures minimum comfort when inhabitants are home
  if (isOccupied && filteredTemp < levels.eco) {
    log('Eco floor: internal temp', filteredTemp, 'below eco setpoint', levels.eco, 'and occupied => HEAT');
    setHeaterState(true);
    return;
  }

  // Calculate minimum forecast temperature for preheat window
  var mfTemp = getMinForecastTemp(CONFIG.preheatHours);
  log('Minimum forecast temp for next', CONFIG.preheatHours, 'hours:', mfTemp);

  var preheat = shouldPreheat(filteredTemp, forecastTemp, mfTemp, targetTemp)
  log('Preheat:', preheat)

  // Normal heating: only during cheap hours or preheat mode
  if ((heaterShouldBeOn && isCheapHour()) || preheat) {
    log('Heater ON (cheap hour or preheat mode)', 'preheat:', preheat);
    setHeaterState(true);
  } else {
    log('Outside cheap window and above eco floor => no heating');
    setHeaterState(false);
  }
}

// === MAIN CONTROL LOOP (flattened) ===
function pollAndControl() {
  // Check if we need to refresh the forecast (once per day)
  if (shouldRefreshForecast()) {
    log('Daily forecast refresh triggered from poll');
    fetchAndCacheForecast();
  }

  // Only run control if we have all necessary inputs
  if (!STATE.forecastUrl || !STATE.cachedForecast || !STATE.temperature.internal || !STATE.temperature.external) {
    log('Skipping control cycle - waiting for initialization (url:', !!STATE.forecastUrl, 'forecast:', !!STATE.cachedForecast, 'internal:', !!STATE.temperature.internal, 'external:', !!STATE.temperature.external, ')');
    return;
  }

  fetchAllControlInputs(controlHeaterWithInputs);
}

// Schedule a deferred check (breaks call stack to prevent recursion)
function scheduleControlLoopCheck() {
  log('scheduleControlLoopCheck called, existing timer:', STATE.controlLoopCheckTimerId);
  // Prevent duplicate timers - only schedule if no check is already pending
  if (STATE.controlLoopCheckTimerId === null) {
    log('Setting 100ms timer for control loop check');
    STATE.controlLoopCheckTimerId = Timer.set(100, false, function () {
      // Clear the timer ID when the timer fires
      STATE.controlLoopCheckTimerId = null;
      checkAndStartControlLoop();
    });
    log('Timer set, id:', STATE.controlLoopCheckTimerId);
  } else {
    log('Timer already pending, skipping');
  }
}

// Check if ready and start control loop timer
function checkAndStartControlLoop() {
  logMemory('checkAndStartControlLoop:start');
  log('Checking whether we can start control loop')
  log('  - Room ID configured:', !!CONFIG.roomId, "room-id:", CONFIG.roomId);
  log('  - Forecast URL ready:', !!STATE.forecastUrl);
  log('  - Forecast data ready:', !!STATE.cachedForecast);
  log('  - Temperature ranges ready:', !!STATE.cachedTemperatureRanges, "topic:", STATE.temperatureRangesTopic);
  log('  - Internal temperature ready:', !!STATE.temperature.internal, "topic:", CONFIG.internalTemperatureTopic);
  log('  - External temperature ready:', !!STATE.temperature.external, "topic:", CONFIG.externalTemperatureTopic);

  // Fail if roomId is not configured - required for temperature ranges
  if (!CONFIG.roomId) {
    log('ERROR: roomId not configured - cannot start control loop');
    return;
  }

  if (STATE.forecastUrl && STATE.cachedForecast && STATE.temperature.internal && STATE.temperature.external) {
    if (!STATE.controlLoopTimerId) {
      log('All inputs ready - starting control loop timer');
      logMemory('checkAndStartControlLoop:allReady');
      // Start the control loop timer now that all inputs are ready
      STATE.controlLoopTimerId = Timer.set(CONFIG.pollIntervalMs, true, pollAndControl);
      // Run first cycle immediately
      pollAndControl();
    }
  }
}

// === HEATER CONTROL (LOCAL SHELLY CALL, SUPPORTS normally-closed VALUE) ===
function setHeaterState(on, cb) {
  STATE.lastHeaterState = on;
  var newState = on !== CONFIG.normallyClosed
  Shelly.call("Switch.Set", { id: 0, on: newState }, function (result, error_code, error_msg, userdata) {
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
  if (updated.indexOf('roomId') !== -1) {
    STATE.temperatureRangesTopic = "myhome/rooms/" + CONFIG.roomId + "/temperature/ranges";
    subscribeToTemperatureRanges();
  }

  subscribeToOccupancy();

  // Re-evaluate control loop when configuration changes
  scheduleControlLoopCheck();
}

// === SCHEDULED EXECUTION ===
// Only run initialization code when running on a Shelly device
// This allows the script to be parsed in other contexts (e.g., tests) without executing
if (typeof Shelly !== "undefined") {
  log("Script starting...");
  logMemory('startup');

  // Initialize device info (cache device info at startup)
  initDeviceInfo();

  scheduleLearningTimers();
  loadConfig(onConfigLoaded);
  fetchForecast();

  Shelly.addStatusHandler(function (status) {
    // Detect KVS updates and reload configuration
    if (status && status.component === "sys" && status.delta && ("kvs_rev" in status.delta)) {
      log('KVS updated (rev ' + status.delta.kvs_rev + '), reloading configuration and re-fetching temperatures');
      loadConfig(onConfigLoaded);
    } else {
      log('Script status:', JSON.stringify(status));
    }
  });

  // Subscribe to all heater commands using wildcard
  MQTT.subscribe('myhome/heater/#', function (topic, message) {
    log('Received message on topic:', topic);

    // Parse the message
    var request = null;
    try {
      request = JSON.parse(message);
    } catch (e) {
      log('Failed to parse message:', message);
      if (e && false) { }
      return;
    }

    // Check for replyTo property - required for all commands
    if (!request || !("replyTo" in request) || typeof request.replyTo !== 'string') {
      log('Message missing replyTo property, dropping request');
      return;
    }

    var replyToTopic = request.replyTo;
    var deviceId = STATE.deviceId || 'unknown';
    var deviceName = STATE.deviceName || 'unknown';

    // Handle different command topics
    if (topic === 'myhome/heater/list' || topic === 'myhome/heater/show/' + deviceId) {
      log('Handling show/list request, will reply to:', replyToTopic);

      // Build response with current configuration and state
      var response = {
        device_id: deviceId,
        device_name: deviceName,
        script_name: SCRIPT_NAME,
        config: {
          enableLogging: CONFIG.enableLogging,
          setpoint: CONFIG.setpoint,
          cheapStartHour: CONFIG.cheapStartHour,
          cheapEndHour: CONFIG.cheapEndHour,
          pollIntervalMs: CONFIG.pollIntervalMs,
          preheatHours: CONFIG.preheatHours,
          normallyClosed: CONFIG.normallyClosed,
          internalTemperatureTopic: CONFIG.internalTemperatureTopic,
          externalTemperatureTopic: CONFIG.externalTemperatureTopic,
          roomId: CONFIG.roomId
        },
        state: {
          heaterOn: STATE.heaterOn,
          internalTemp: STATE.internalTemp,
          externalTemp: STATE.externalTemp,
          temperatureRanges: STATE.cachedTemperatureRanges,
          currentSetpoint: STATE.currentSetpoint,
          coolingRate: getCoolingRate(),
          lastUpdate: new Date().getTime()
        },
        timestamp: new Date().getTime() / 1000
      };

      // Publish response to the topic specified in replyTo
      MQTT.publish(replyToTopic, JSON.stringify(response), 0, false);
      log('Sent response to', replyToTopic);
    } else {
      // Log other heater commands but don't respond
      log('Received unhandled heater command on topic:', topic, 'request:', JSON.stringify(request));
    }
  });

  log("Script started");
}
